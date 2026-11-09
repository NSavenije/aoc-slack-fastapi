package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const fixture = `{"event":"2026","owner_id":1,"members":{
"1":{"id":1,"name":"Bart van Dijk","stars":2,"local_score":30,"last_star_ts":1764587074,
 "completion_day_level":{"1":{"1":{"star_index":1,"get_star_ts":1764580000},"2":{"star_index":2,"get_star_ts":1764587074}}}},
"2":{"id":2,"name":"","stars":1,"local_score":30,"last_star_ts":1764570000,
 "completion_day_level":{"1":{"1":{"star_index":3,"get_star_ts":1764570000}}}},
"3":{"id":3,"name":"Idle","stars":0,"local_score":0,"last_star_ts":0,"completion_day_level":{}}}}`

func TestModeAlternates(t *testing.T) {
	base := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	a, b, c := ModeForSlot(base), ModeForSlot(base.Add(30*time.Minute)), ModeForSlot(base.Add(60*time.Minute))
	if a == b || a != c {
		t.Fatalf("not alternating: %s %s %s", a, b, c)
	}
	// anywhere inside a slot gives the same answer
	if ModeForSlot(base.Add(29*time.Minute)) != a {
		t.Fatal("mode changed inside a slot")
	}
}

func TestStarsSinceAndBoards(t *testing.T) {
	var lb Leaderboard
	if err := json.Unmarshal([]byte(fixture), &lb); err != nil {
		t.Fatal(err)
	}
	ev := lb.StarsSince(time.Unix(1764575000, 0))
	if len(ev) != 2 || ev[0].Part != 2 || ev[0].Name != "Bart van Dijk" {
		t.Fatalf("unexpected events: %+v", ev)
	}
	sb := StarsBoard(ev)
	if sb[1][18] != colorYellow || sb[1][19] != colorYellow || sb[2][19] == colorYellow {
		t.Fatalf("star markers wrong: %v %v", sb[1], sb[2])
	}
	lbd := LeaderboardBoard(&lb, 2026)
	if lbd[1][2] != vbChars['1'] { // rank right-aligned in cols 1-2
		t.Fatalf("rank missing: %v", lbd[1])
	}
	if lbd[2][2] != vbChars['1'] { // tie on 30 pts -> both rank 1
		t.Fatalf("tie should share rank: %v", lbd[2])
	}
	for r := range lbd {
		if len(lbd[r]) != 22 {
			t.Fatal("bad width")
		}
	}
	if !strings.Contains(SlackLeaderboard(&lb, 2026), "anon #2") {
		t.Fatal("anonymous member not rendered")
	}
}

func TestRunEndToEnd(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{}
	aoc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "session=sek" || !strings.HasSuffix(r.URL.Path, "/2026/leaderboard/private/view/42.json") {
			http.Error(w, "bad", 403)
			return
		}
		io.WriteString(w, fixture)
	}))
	defer aoc.Close()
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got[r.URL.Path] = string(b)
		if r.URL.Path == "/vesta" {
			got["key"] = r.Header.Get("X-Vestaboard-Read-Write-Key")
		}
		mu.Unlock()
	}))
	defer sink.Close()

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Date(2026, 12, 3, 10, 0, 0, 0, loc)
	c := http.DefaultClient
	j := &Job{
		Cfg:   Config{Year: 2026, Loc: loc, ActiveFrom: 8, ActiveTo: 21, StarsLookback: time.Hour},
		AoC:   &AoCClient{HTTP: c, BaseURL: aoc.URL, Year: 2026, BoardID: "42", SessionCookie: "sek", UserAgent: "t"},
		Vesta: &Vestaboard{HTTP: c, URL: sink.URL + "/vesta", APIKey: "k"},
		Slack: &Slack{HTTP: c, WebhookURL: sink.URL + "/slack"},
		Now:   func() time.Time { return now },
	}
	mode, err := j.Run(context.Background(), ModeLeaderboard, false)
	if err != nil || mode != ModeLeaderboard {
		t.Fatal(mode, err)
	}
	if !strings.Contains(got["/slack"], "leaderboard") || got["key"] != "k" {
		t.Fatalf("sinks not hit: %v", got)
	}
	var rows [][]int
	if err := json.Unmarshal([]byte(got["/vesta"]), &rows); err != nil || len(rows) != 6 || len(rows[0]) != 22 {
		t.Fatalf("bad vestaboard payload: %v %v", err, got["/vesta"])
	}

	// outside active hours / not December => skipped in auto mode
	now = time.Date(2026, 12, 3, 23, 0, 0, 0, loc)
	if m, err := j.Run(context.Background(), ModeAuto, false); m != "" || err != nil {
		t.Fatalf("expected skip, got %q %v", m, err)
	}
	now = time.Date(2026, 6, 3, 10, 0, 0, 0, loc)
	if m, _ := j.Run(context.Background(), ModeAuto, false); m != "" {
		t.Fatal("should skip outside December")
	}
}
