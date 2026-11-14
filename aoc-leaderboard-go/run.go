package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Mode string

const (
	ModeAuto        Mode = ""
	ModeLeaderboard Mode = "leaderboard"
	ModeStars       Mode = "stars"
)

// Config holds everything the job needs.
type Config struct {
	Year          int
	Loc           *time.Location
	ActiveFrom    int           // hour, inclusive (local time)
	ActiveTo      int           // hour, exclusive
	StarsLookback time.Duration // how far back "latest stars" looks
	AlwaysActive  bool          // ignore December / opening-hours check
}

type Job struct {
	Cfg   Config
	AoC   *AoCClient
	Vesta *Vestaboard // nil = disabled
	Slack *Slack      // nil = disabled
	Now   func() time.Time
}

// ModeForSlot alternates every half hour, derived from the clock rather than
// stored state, so it works on a stateless / scale-to-zero platform.
func ModeForSlot(t time.Time) Mode {
	if (t.Unix()/1800)%2 == 0 {
		return ModeLeaderboard
	}
	return ModeStars
}

// Active reports whether updates should be posted now (December, daytime).
func (j *Job) Active(t time.Time) bool {
	if j.Cfg.AlwaysActive {
		return true
	}
	l := t.In(j.Cfg.Loc)
	return l.Month() == time.December && l.Hour() >= j.Cfg.ActiveFrom && l.Hour() < j.Cfg.ActiveTo
}

// Run performs one update. It returns the mode that was executed.
// lookback overrides Cfg.StarsLookback when > 0 (handy to test against an old year).
func (j *Job) Run(ctx context.Context, mode Mode, dry bool, lookback time.Duration) (Mode, error) {
	now := j.Now()
	if mode == ModeAuto {
		if !j.Active(now) {
			slog.Info("outside active window, skipping", "now", now.In(j.Cfg.Loc))
			return "", nil
		}
		mode = ModeForSlot(now)
	}
	lb, err := j.AoC.Fetch(ctx)
	if err != nil {
		return mode, err
	}

	var board Board
	var msg string
	switch mode {
	case ModeLeaderboard:
		board, msg = LeaderboardBoard(lb, j.Cfg.Year), SlackLeaderboard(lb, j.Cfg.Year)
	case ModeStars:
		if lookback <= 0 {
			lookback = j.Cfg.StarsLookback
		}
		ev := lb.StarsSince(now.Add(-lookback))
		board, msg = StarsBoard(ev), SlackStars(ev, j.Cfg.Loc)
	default:
		return mode, fmt.Errorf("unknown mode %q", mode)
	}
	slog.Info("update", "mode", mode, "dry", dry)
	if dry {
		return mode, nil
	}
	var errs []error
	if j.Vesta != nil {
		if err := j.Vesta.Send(ctx, board); err != nil {
			errs = append(errs, fmt.Errorf("vestaboard: %w", err))
		}
	}
	if j.Slack != nil {
		if err := j.Slack.Send(ctx, msg); err != nil {
			errs = append(errs, fmt.Errorf("slack: %w", err))
		}
	}
	return mode, errors.Join(errs...)
}
