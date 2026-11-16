package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var ams, _ = time.LoadLocation("Europe/Amsterdam")

func testConfig() Config {
	return Config{
		Year: 2026, Loc: ams, FirstDay: 1, LastDay: 12,
		SlackLeaderboardHour: 8, SlackStarsFrom: 6, SlackStarsTo: 22,
		VestaFrom: 8, VestaTo: 17, VestaWeekdaysOnly: true,
		StarsLookback: 30 * time.Minute,
	}
}

func at(day, hour, min, sec int) time.Time { return time.Date(2026, 12, day, hour, min, sec, 0, ams) }

func TestCalendarAssumptions(t *testing.T) {
	if at(7, 0, 0, 0).Weekday() != time.Monday || at(5, 0, 0, 0).Weekday() != time.Saturday {
		t.Fatal("test dates are not the weekdays the tests assume")
	}
}

func TestSchedule(t *testing.T) {
	j := &Job{Cfg: testConfig()}
	type want struct{ slackLB, slackStars, vesta bool }
	cases := []struct {
		name string
		t    time.Time
		w    want
	}{
		{"Mon 05:45 too early", at(7, 5, 45, 0), want{false, false, false}},
		{"Mon 07:45 stars only", at(7, 7, 45, 0), want{false, true, false}},
		{"Mon 08:00 morning leaderboard + vesta", at(7, 8, 0, 20), want{true, true, true}},
		{"Mon 08:15 slack stars only", at(7, 8, 15, 5), want{false, true, false}},
		{"Mon 08:30 vesta slot", at(7, 8, 30, 5), want{false, true, true}},
		{"Mon 08:45 slack stars only", at(7, 8, 45, 5), want{false, true, false}},
		{"Mon 16:30 last vesta slot", at(7, 16, 30, 0), want{false, true, true}},
		{"Mon 17:00 office closed", at(7, 17, 0, 0), want{false, true, false}},
		{"Mon 21:45 last slack check", at(7, 21, 45, 0), want{false, true, false}},
		{"Mon 22:00 slack closed", at(7, 22, 0, 0), want{false, false, false}},
		{"Sat 10:00 weekend: slack only", at(5, 10, 0, 0), want{false, true, false}},
		{"Sat Dec 12 last event day", at(12, 10, 15, 0), want{false, true, false}},
		{"Sun Dec 13 after the event", at(13, 10, 0, 0), want{false, false, false}},
		{"Nov 30 before the event", time.Date(2026, 11, 30, 10, 0, 0, 0, ams), want{false, false, false}},
	}
	for _, c := range cases {
		p := j.autoPlan(c.t)
		got := want{p.slackLeaderboard, p.slackStars != nil, p.vesta}
		if got != c.w {
			t.Errorf("%s: got %+v want %+v (reason %q)", c.name, got, c.w, p.reason)
		}
		if p.empty() && p.reason == "" {
			t.Errorf("%s: empty plan without a reason", c.name)
		}
	}

	// windows are aligned to the slot, not to "now", so a late tick doesn't shift them
	p := j.autoPlan(at(7, 8, 0, 40))
	if !p.slackStars.from.Equal(at(7, 7, 45, 0)) || !p.slackStars.to.Equal(at(7, 8, 0, 0)) {
		t.Errorf("slack window %v", *p.slackStars)
	}
	if !p.vestaStars.from.Equal(at(7, 7, 30, 0)) || !p.vestaStars.to.Equal(at(7, 8, 0, 0)) {
		t.Errorf("vesta window %v", *p.vestaStars)
	}

	// forgotten AOC_YEAR (2025) in December 2026: skip and say why
	old := testConfig()
	old.Year = 2025
	p = (&Job{Cfg: old}).autoPlan(at(7, 10, 0, 0))
	if !p.empty() || !strings.Contains(p.reason, "AOC_YEAR") {
		t.Errorf("year mismatch not caught: %+v", p)
	}
	// ALWAYS_ACTIVE ignores the event days/year (but not opening hours)
	old.AlwaysActive = true
	p = (&Job{Cfg: old}).autoPlan(time.Date(2026, 11, 14, 10, 0, 0, 0, ams))
	if p.slackStars == nil {
		t.Errorf("always active ignored: %+v", p)
	}
}

func member(id int, name string, score int) *Member {
	return &Member{ID: id, Name: name, LocalScore: score, Stars: score / 10}
}

func TestBoards(t *testing.T) {
	lb := &Leaderboard{Members: map[string]*Member{}}
	for i := 1; i <= 12; i++ {
		lb.Members[fmt.Sprint(i)] = member(i, fmt.Sprintf("Player%d", i), 100-i)
	}
	lb.Members["13"] = member(13, "", 88) // anonymous, ties with Player12
	if LeaderboardPages(lb) != 2 {
		t.Fatalf("pages = %d", LeaderboardPages(lb))
	}
	p1 := LeaderboardBoard(lb, 2026, 0, 2)
	p2 := LeaderboardBoard(lb, 2026, 1, 2)
	if p1[0][1] != vbChars['A'] || p1[0][2] != vbChars['O'] || p1[0][3] != vbChars['C'] {
		t.Fatalf("title: %v", p1[0])
	}
	if p1[1][2] != vbChars['1'] { // rank 1 right-aligned in cols 1-2
		t.Fatalf("p1 row1: %v", p1[1])
	}
	// page 2 holds ranks 11 and 12 (the tie shares rank 12), then blanks
	if p2[1][1] != vbChars['1'] || p2[1][2] != vbChars['1'] || p2[2][1] != vbChars['1'] || p2[2][2] != vbChars['2'] || p2[3][2] != vbChars['2'] {
		t.Fatalf("page 2: %v / %v / %v", p2[1], p2[2], p2[3])
	}
	for c := 1; c < 11; c++ {
		if p2[4][c] != 0 || p2[5][c] != 0 {
			t.Fatalf("page 2 should have empty rows 4-5: %v %v", p2[4], p2[5])
		}
	}
	if !strings.Contains(SlackLeaderboard(lb, 2026), "anon #13") {
		t.Fatal("anonymous member not rendered in Slack")
	}

	ev := []StarEvent{{"Bart van Dijk", 3, 2, time.Now()}, {"Eelco", 3, 1, time.Now()}}
	sb := StarsBoard(ev)
	if sb[1][18] != colorYellow || sb[1][19] != colorYellow || sb[2][18] != colorYellow || sb[2][19] == colorYellow {
		t.Fatalf("star markers: %v %v", sb[1], sb[2])
	}
}

func TestStarsBetweenIsHalfOpen(t *testing.T) {
	lb := &Leaderboard{Members: map[string]*Member{
		"1": {ID: 1, Name: "A", Completion: map[string]map[string]Star{"1": {"1": {GetStarTS: 1000}, "2": {GetStarTS: 2000}}}},
	}}
	got := lb.StarsBetween(time.Unix(1000, 0), time.Unix(2000, 0))
	if len(got) != 1 || got[0].Part != 1 {
		t.Fatalf("[from, to) violated: %+v", got)
	}
}

// ---- end to end against fake AoC / Slack / Vestaboard servers ----

type sinks struct {
	mu     sync.Mutex
	slack  []string
	vesta  []Board
	aocURL string
	path   string
	close  func()
}

func newSinks(t *testing.T, board string) *sinks {
	s := &sinks{}
	aoc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.path = r.URL.Path
		s.mu.Unlock()
		if r.Header.Get("Cookie") != "session=sek" {
			http.Error(w, "bad", 403)
			return
		}
		io.WriteString(w, board)
	}))
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		defer s.mu.Unlock()
		switch r.URL.Path {
		case "/slack":
			s.slack = append(s.slack, string(b))
		case "/vesta":
			var bd Board
			if err := json.Unmarshal(b, &bd); err != nil {
				t.Errorf("bad vestaboard payload: %v", err)
			}
			s.vesta = append(s.vesta, bd)
		}
	}))
	s.aocURL = aoc.URL
	s.close = func() { aoc.Close(); sink.Close() }
	t.Cleanup(s.close)
	s.aocURL = aoc.URL
	s.path = ""
	sinkURL = sink.URL
	return s
}

var sinkURL string

func (s *sinks) job(now *time.Time, cfg Config) *Job {
	return &Job{
		Cfg:   cfg,
		AoC:   &AoCClient{HTTP: http.DefaultClient, BaseURL: s.aocURL, Year: cfg.Year, BoardID: "42", SessionCookie: "sek", UserAgent: "t"},
		Vesta: &Vestaboard{HTTP: http.DefaultClient, URL: sinkURL + "/vesta", APIKey: "k"},
		Slack: &Slack{HTTP: http.DefaultClient, WebhookURL: sinkURL + "/slack"},
		Now:   func() time.Time { return *now },
	}
}

func boardJSON(starAt time.Time) string {
	return fmt.Sprintf(`{"event":"2026","owner_id":1,"members":{
"1":{"id":1,"name":"Bart van Dijk","stars":2,"local_score":30,"completion_day_level":{"1":{"1":{"star_index":1,"get_star_ts":1764580000},"2":{"star_index":2,"get_star_ts":%d}}}},
"2":{"id":2,"name":"Idle","stars":0,"local_score":0,"completion_day_level":{}}}}`, starAt.Unix())
}

func TestRunAutoFollowsTheSchedule(t *testing.T) {
	now := at(7, 8, 0, 30) // Monday 08:00: morning leaderboard + stars + vesta slot
	s := newSinks(t, boardJSON(at(7, 7, 50, 0)))
	j := s.job(&now, testConfig())

	res, err := j.Run(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.slack) != 2 || !strings.Contains(s.slack[0], "leaderboard") || !strings.Contains(s.slack[1], "New stars") {
		t.Fatalf("slack: %v (actions %v)", s.slack, res.Actions)
	}
	if len(s.vesta) != 1 || s.vesta[0][1][18] != colorYellow { // star shown instead of a leaderboard page
		t.Fatalf("vesta should show the star: %v", s.vesta)
	}
	if !strings.HasSuffix(s.path, "/2026/leaderboard/private/view/42.json") {
		t.Fatalf("wrong AoC url %q", s.path)
	}

	// 08:15: only Slack looks for new stars; the 07:50 star is outside [08:00, 08:15) -> silence
	now = at(7, 8, 15, 5)
	s.slack, s.vesta = nil, nil
	res, err = j.Run(context.Background(), Options{})
	if err != nil || len(s.slack) != 0 || len(s.vesta) != 0 {
		t.Fatalf("08:15 should be silent: %v %v %v %v", s.slack, s.vesta, res, err)
	}

	// 08:30: vesta slot without a star in [08:00, 08:30) -> a leaderboard page; slack stays silent
	now = at(7, 8, 30, 5)
	res, err = j.Run(context.Background(), Options{})
	if err != nil || len(s.slack) != 0 || len(s.vesta) != 1 || s.vesta[0][0][1] != vbChars['A'] {
		t.Fatalf("08:30: %v %v %v %v", s.slack, s.vesta, res, err)
	}

	// Saturday: no vesta, slack still checks for stars
	now = at(5, 10, 0, 5)
	s.slack, s.vesta = nil, nil
	if _, err = j.Run(context.Background(), Options{}); err != nil || len(s.vesta) != 0 {
		t.Fatalf("weekend: %v %v", s.vesta, err)
	}

	// after the event: nothing, and AoC isn't even called
	now = at(13, 10, 0, 5)
	s.path = ""
	res, err = j.Run(context.Background(), Options{})
	if err != nil || len(res.Actions) != 0 || res.Reason == "" || s.path != "" {
		t.Fatalf("after event: %+v %v path=%q", res, err, s.path)
	}
}

func TestManualModesAndTargets(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, ams) // not December: manual modes still work
	s := newSinks(t, boardJSON(time.Unix(1764580000, 0)))
	cfg := testConfig()
	cfg.Year = 2025
	j := s.job(&now, cfg)

	// target=vesta only touches the Vestaboard
	if _, err := j.Run(context.Background(), Options{Mode: ModeLeaderboard, Target: TargetVesta}); err != nil {
		t.Fatal(err)
	}
	if len(s.slack) != 0 || len(s.vesta) != 1 {
		t.Fatalf("target=vesta: slack=%v vesta=%d", s.slack, len(s.vesta))
	}
	if !strings.Contains(s.path, "/2025/") {
		t.Fatalf("configured year not used: %s", s.path)
	}

	// manual stars with a long lookback finds the old star; with a short one it still reports "none"
	if _, err := j.Run(context.Background(), Options{Mode: ModeStars, Target: TargetSlack, Lookback: 24 * time.Hour * 365}); err != nil {
		t.Fatal(err)
	}
	if len(s.slack) != 1 || !strings.Contains(s.slack[0], "Bart van Dijk") {
		t.Fatalf("lookback: %v", s.slack)
	}
	if _, err := j.Run(context.Background(), Options{Mode: ModeStars, Target: TargetSlack, Lookback: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if len(s.slack) != 2 || !strings.Contains(s.slack[1], "No new stars") {
		t.Fatalf("empty manual stars: %v", s.slack)
	}

	// dry run: fetches but posts nothing
	s.slack, s.vesta = nil, nil
	res, err := j.Run(context.Background(), Options{Mode: ModeLeaderboard, Dry: true})
	if err != nil || len(s.slack)+len(s.vesta) != 0 || len(res.Actions) != 2 {
		t.Fatalf("dry run: %v %v %+v %v", s.slack, s.vesta, res, err)
	}
}
