package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type Mode string

const (
	ModeAuto        Mode = "" // decide from the clock (what the scheduled trigger uses)
	ModeLeaderboard Mode = "leaderboard"
	ModeStars       Mode = "stars"
)

type Target string

const (
	TargetBoth  Target = ""
	TargetSlack Target = "slack"
	TargetVesta Target = "vesta"
)

const (
	slackStarsWindow = 15 * time.Minute // Slack: new stars, checked every 15 minutes
	vestaSlot        = 30 * time.Minute // Vestaboard: one update per half hour
	pageSize         = 10
)

// Config holds everything the job needs.
type Config struct {
	Year int
	Loc  *time.Location

	// Days of December on which the scheduled run does anything (AoC 2025: 1-12).
	FirstDay, LastDay int

	// Slack: full leaderboard once a day in the first 15 minutes of this hour;
	// new stars every 15 minutes between the two hours (local time, to exclusive).
	SlackLeaderboardHour         int
	SlackStarsFrom, SlackStarsTo int

	// Vestaboard: update on the half hour between these hours (to exclusive).
	VestaFrom, VestaTo int
	VestaWeekdaysOnly  bool

	StarsLookback time.Duration // default window for a manual mode=stars
	AlwaysActive  bool          // ignore the year/event-day check (testing)
}

type Job struct {
	Cfg   Config
	AoC   *AoCClient
	Vesta *Vestaboard // nil = disabled
	Slack *Slack      // nil = disabled
	Now   func() time.Time
}

type Options struct {
	Mode     Mode
	Target   Target
	Dry      bool
	Lookback time.Duration // manual stars only; 0 = Cfg.StarsLookback
}

type Result struct {
	Actions []string // what was (or, for a dry run, would be) posted
	Reason  string   // why nothing was done
}

type window struct{ from, to time.Time }

// plan says what to do for one call. It is derived from the clock only, so the
// service needs no state and can scale to zero between calls.
type plan struct {
	reason           string
	slackLeaderboard bool
	slackStars       *window
	vesta            bool
	vestaStars       *window // show stars if any in the window, else a leaderboard page
	postEmpty        bool    // manual stars: also report "no new stars"
}

func (p plan) empty() bool { return !p.slackLeaderboard && p.slackStars == nil && !p.vesta }

func truncate(t time.Time, d time.Duration) time.Time {
	s := int64(d / time.Second)
	return time.Unix(t.Unix()/s*s, 0)
}

func (j *Job) planFor(now time.Time, o Options) plan {
	var p plan
	switch o.Mode {
	case ModeLeaderboard:
		p.slackLeaderboard, p.vesta = true, true
	case ModeStars:
		lb := o.Lookback
		if lb <= 0 {
			lb = j.Cfg.StarsLookback
		}
		w := &window{now.Add(-lb), now.Add(time.Second)}
		p.slackStars, p.vestaStars, p.vesta, p.postEmpty = w, w, true, true
	case ModeAuto:
		p = j.autoPlan(now)
	default:
		return plan{reason: fmt.Sprintf("unknown mode %q", o.Mode)}
	}
	switch o.Target {
	case TargetSlack:
		p.vesta, p.vestaStars = false, nil
	case TargetVesta:
		p.slackLeaderboard, p.slackStars = false, nil
	}
	if p.empty() && p.reason == "" {
		p.reason = "nothing to post for this target"
	}
	return p
}

func (j *Job) autoPlan(now time.Time) plan {
	c := j.Cfg
	l := now.In(c.Loc)
	if !c.AlwaysActive {
		if l.Year() != c.Year {
			return plan{reason: fmt.Sprintf("AOC_YEAR is %d but it is %d: update or remove the AOC_YEAR variable and redeploy", c.Year, l.Year())}
		}
		if l.Month() != time.December || l.Day() < c.FirstDay || l.Day() > c.LastDay {
			return plan{reason: fmt.Sprintf("outside the event days (December %d-%d)", c.FirstDay, c.LastDay)}
		}
	}
	var p plan
	h, m := l.Hour(), l.Minute()

	// Slack: leaderboard every morning, new stars every 15 minutes (silent if none).
	if h == c.SlackLeaderboardHour && m < 15 {
		p.slackLeaderboard = true
	}
	if h >= c.SlackStarsFrom && h < c.SlackStarsTo {
		s := truncate(now, slackStarsWindow)
		p.slackStars = &window{s.Add(-slackStarsWindow), s}
	}

	// Vestaboard: every half hour in office hours (Mon-Fri); a star replaces that slot's page.
	weekday := l.Weekday() >= time.Monday && l.Weekday() <= time.Friday
	if (weekday || !c.VestaWeekdaysOnly) && h >= c.VestaFrom && h < c.VestaTo && m%30 < 15 {
		s := truncate(now, vestaSlot)
		p.vesta = true
		p.vestaStars = &window{s.Add(-vestaSlot), s}
	}
	if p.empty() {
		p.reason = "nothing scheduled for this time"
	}
	return p
}

// Run performs whatever is due (auto) or what was asked for (manual mode).
func (j *Job) Run(ctx context.Context, o Options) (Result, error) {
	now := j.Now()
	p := j.planFor(now, o)
	if p.empty() {
		slog.Info("nothing to do", "reason", p.reason)
		return Result{Reason: p.reason}, nil
	}
	lb, err := j.AoC.Fetch(ctx)
	if err != nil {
		return Result{}, err
	}

	var res Result
	var errs []error
	do := func(label string, send func() (string, error)) {
		if o.Dry {
			res.Actions = append(res.Actions, label+" (dry run)")
			return
		}
		answer, err := send()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label, err))
			return
		}
		if answer != "" {
			label += " -> " + answer
		}
		res.Actions = append(res.Actions, label)
	}
	noAnswer := func(err error) (string, error) { return "", err }

	// Slack
	if j.Slack != nil {
		if p.slackLeaderboard {
			do("slack: leaderboard", func() (string, error) { return noAnswer(j.Slack.Send(ctx, SlackLeaderboard(lb, j.Cfg.Year))) })
		}
		if p.slackStars != nil {
			ev := lb.StarsBetween(p.slackStars.from, p.slackStars.to)
			if len(ev) > 0 || p.postEmpty {
				do(fmt.Sprintf("slack: %d new stars", len(ev)), func() (string, error) { return noAnswer(j.Slack.Send(ctx, SlackStars(ev, j.Cfg.Loc))) })
			}
		}
	} else if p.slackLeaderboard || p.slackStars != nil {
		res.Actions = append(res.Actions, "slack: skipped (SLACK_WEBHOOK_URL not set)")
	}

	// Vestaboard
	if p.vesta {
		if j.Vesta == nil {
			res.Actions = append(res.Actions, "vesta: skipped (VESTABOARD_API_KEY not set)")
		} else {
			var board Board
			label := ""
			if p.vestaStars != nil {
				if ev := lb.StarsBetween(p.vestaStars.from, p.vestaStars.to); len(ev) > 0 || p.postEmpty {
					board, label = StarsBoard(ev), fmt.Sprintf("vesta: %d new stars", len(ev))
				}
			}
			if label == "" { // no star to show: next page of the leaderboard (page = clock slot, so it cycles)
				pages := LeaderboardPages(lb)
				page := int((now.Unix() / int64(vestaSlot/time.Second)) % int64(pages))
				board, label = LeaderboardBoard(lb, j.Cfg.Year, page, pages), fmt.Sprintf("vesta: leaderboard page %d/%d", page+1, pages)
			}
			do(label, func() (string, error) { return j.Vesta.Send(ctx, board) })
		}
	}
	slog.Info("run", "actions", strings.Join(res.Actions, "; "))
	return res, errors.Join(errs...)
}
