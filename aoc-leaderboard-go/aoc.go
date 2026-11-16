package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Leaderboard mirrors the AoC private leaderboard JSON.
type Leaderboard struct {
	Event   string             `json:"event"`
	OwnerID int                `json:"owner_id"`
	Members map[string]*Member `json:"members"`
}

type Member struct {
	ID         int                        `json:"id"`
	Name       string                     `json:"name"`
	Stars      int                        `json:"stars"`
	LocalScore int                        `json:"local_score"`
	LastStarTS int64                      `json:"last_star_ts"`
	Completion map[string]map[string]Star `json:"completion_day_level"`
}

type Star struct {
	StarIndex int   `json:"star_index"`
	GetStarTS int64 `json:"get_star_ts"`
}

// DisplayName returns the AoC name, or the anonymous fallback AoC itself uses.
func (m *Member) DisplayName() string {
	if m.Name == "" {
		return fmt.Sprintf("anon #%d", m.ID)
	}
	return m.Name
}

type AoCClient struct {
	HTTP          *http.Client
	BaseURL       string // default https://adventofcode.com
	Year          int
	BoardID       string
	SessionCookie string
	UserAgent     string
}

func (c *AoCClient) Fetch(ctx context.Context) (*Leaderboard, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://adventofcode.com"
	}
	url := fmt.Sprintf("%s/%d/leaderboard/private/view/%s.json", base, c.Year, c.BoardID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", "session="+c.SessionCookie)
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("advent of code returned HTTP %d (expired session cookie?)", resp.StatusCode)
	}
	var lb Leaderboard
	if err := json.NewDecoder(resp.Body).Decode(&lb); err != nil {
		return nil, fmt.Errorf("decode leaderboard: %w", err)
	}
	return &lb, nil
}

// Ranked returns members by local score (desc), then stars (desc), then name.
func (lb *Leaderboard) Ranked() []*Member {
	out := make([]*Member, 0, len(lb.Members))
	for _, m := range lb.Members {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.LocalScore != b.LocalScore {
			return a.LocalScore > b.LocalScore
		}
		if a.Stars != b.Stars {
			return a.Stars > b.Stars
		}
		return a.DisplayName() < b.DisplayName()
	})
	return out
}

// StarEvent is one star obtained by one member.
type StarEvent struct {
	Name string
	Day  int
	Part int // 1 = silver, 2 = gold
	At   time.Time
}

// StarsBetween lists stars earned in [from, to), newest first.
func (lb *Leaderboard) StarsBetween(from, to time.Time) []StarEvent {
	return lb.stars(func(at time.Time) bool { return !at.Before(from) && at.Before(to) })
}

// StarsSince lists all stars earned strictly after `since`, newest first.
func (lb *Leaderboard) StarsSince(since time.Time) []StarEvent {
	return lb.stars(func(at time.Time) bool { return at.After(since) })
}

func (lb *Leaderboard) stars(keep func(time.Time) bool) []StarEvent {
	var out []StarEvent
	for _, m := range lb.Members {
		for dayStr, parts := range m.Completion {
			day, err := strconv.Atoi(dayStr)
			if err != nil {
				continue
			}
			for partStr, s := range parts {
				part, err := strconv.Atoi(partStr)
				if err != nil {
					continue
				}
				at := time.Unix(s.GetStarTS, 0)
				if keep(at) {
					out = append(out, StarEvent{m.DisplayName(), day, part, at})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.After(out[j].At)
		}
		return out[i].Name < out[j].Name
	})
	return out
}
