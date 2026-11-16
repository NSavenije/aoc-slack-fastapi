package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Slack struct {
	HTTP       *http.Client
	WebhookURL string
}

func (s *Slack) Send(ctx context.Context, msg string) error {
	body, _ := json.Marshal(map[string]string{"text": msg})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("slack returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func SlackLeaderboard(lb *Leaderboard, year int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, ":trophy: *Advent of Code %d – leaderboard*\n```\n", year)
	prevScore, prevRank := -1, 0
	for i, m := range lb.Ranked() {
		rank := i + 1
		if m.LocalScore == prevScore {
			rank = prevRank
		}
		prevScore, prevRank = m.LocalScore, rank
		fmt.Fprintf(&sb, "%2d. %-24.24s %5d pts  %2d ★\n", rank, m.DisplayName(), m.LocalScore, m.Stars)
	}
	sb.WriteString("```")
	return sb.String()
}

func SlackStars(events []StarEvent, loc *time.Location) string {
	if len(events) == 0 {
		return ":star: No new stars in this period."
	}
	var sb strings.Builder
	sb.WriteString(":star: *New stars*\n")
	for _, e := range events {
		stars := ":star:"
		if e.Part == 2 {
			stars = ":star::star:"
		}
		fmt.Fprintf(&sb, "• %s – day %d %s (%s)\n", e.Name, e.Day, stars, e.At.In(loc).Format("15:04"))
	}
	return strings.TrimRight(sb.String(), "\n")
}
