package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	vbRows = 6
	vbCols = 22

	colorOrange = 64
	colorYellow = 65
	colorBlue   = 67
)

var vbChars = map[rune]int{
	' ': 0, '!': 37, '@': 38, '#': 39, '$': 40, '(': 41, ')': 42, '-': 44, '+': 46,
	'&': 47, '=': 48, ';': 49, ':': 50, '\'': 52, '"': 53, '%': 54, ',': 55, '.': 56,
	'/': 59, '?': 60, '°': 62,
}

func init() {
	for i, r := range "ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		vbChars[r] = i + 1
	}
	for i, r := range "1234567890" {
		vbChars[r] = i + 27
	}
}

type Board [vbRows][vbCols]int

// text converts s to character codes (uppercased, unknown chars become spaces),
// padded/truncated to width.
func text(s string, width int) []int {
	out := make([]int, width)
	i := 0
	for _, r := range strings.ToUpper(s) {
		if i >= width {
			break
		}
		out[i] = vbChars[r] // missing -> 0 (blank)
		i++
	}
	return out
}

// frame paints the alternating orange/blue border on the left and right edge
// of rows 1..5 and a titled header on row 0.
func frame(title string) Board {
	var b Board
	b[0][0] = colorOrange
	copy(b[0][1:21], text(title, 20))
	b[0][21] = colorBlue
	for r := 1; r < vbRows; r++ {
		l, rr := colorBlue, colorOrange
		if r%2 == 0 {
			l, rr = colorOrange, colorBlue
		}
		b[r][0], b[r][21] = l, rr
	}
	return b
}

func shortName(n string) string {
	return strings.ReplaceAll(n, " ", "")
}

// LeaderboardPages is the number of 10-entry pages the leaderboard needs (at least 1).
func LeaderboardPages(lb *Leaderboard) int {
	n := (len(lb.Members) + 9) / 10
	if n < 1 {
		n = 1
	}
	return n
}

// LeaderboardBoard renders one page (0-based) of ten entries in two columns of five
// (rank + 7 chars). Ranks are global and ties share a rank.
func LeaderboardBoard(lb *Leaderboard, year, page, pages int) Board {
	title := fmt.Sprintf("ADVENT OF CODE %d!", year)
	if pages > 1 {
		title = fmt.Sprintf("AOC %d  PAGE %d/%d", year, page+1, pages)
	}
	b := frame(title)
	ranked := lb.Ranked()
	ranks := make([]int, len(ranked))
	for i, m := range ranked {
		ranks[i] = i + 1
		if i > 0 && m.LocalScore == ranked[i-1].LocalScore {
			ranks[i] = ranks[i-1]
		}
	}
	for k := 0; k < 10; k++ {
		i := page*10 + k
		if i >= len(ranked) {
			break
		}
		cell := text(fmt.Sprintf("%2d %-7.7s", ranks[i], shortName(ranked[i].DisplayName())), 10)
		row, col := 1+k%5, 1+(k/5)*10
		copy(b[row][col:col+10], cell)
	}
	return b
}

// StarsBoard renders the newest five star events: "NAME    DAY 5 **".
func StarsBoard(events []StarEvent) Board {
	if len(events) == 0 {
		return frame("  NO NEW STARS :(")
	}
	b := frame("  OUR LATEST STARS")
	for i := 0; i < 5 && i < len(events); i++ {
		e := events[i]
		copy(b[1+i][2:20], text(fmt.Sprintf("%-7.7s DAY %-2d", shortName(e.Name), e.Day), 15))
		b[1+i][18] = colorYellow
		if e.Part == 2 {
			b[1+i][19] = colorYellow // two stars for part 2
		}
	}
	return b
}

// Vestaboard posts boards through the Vestaboard Cloud API
// (https://docs.vestaboard.com/docs/read-write-api/endpoints): POST https://cloud.vestaboard.com/
// with the token in X-Vestaboard-Token and {"characters": [[...]]} as body.
type Vestaboard struct {
	HTTP   *http.Client
	URL    string // default https://cloud.vestaboard.com/ (https://rw.vestaboard.com/ in legacy mode)
	APIKey string
	// Force sends the message even while the board is muted or in quiet hours
	// (otherwise the API rejects it with HTTP 423). Cloud API only.
	Force bool
	// Legacy uses the old Read/Write API that last year's version used: rw.vestaboard.com,
	// X-Vestaboard-Read-Write-Key header and a bare array as body.
	Legacy bool
}

// Send posts the board. On success it returns a short description of the API's answer
// ("HTTP 200 {...}") so it can be shown in the run result; the API key is never part of it.
func (v *Vestaboard) Send(ctx context.Context, b Board) (string, error) {
	url, header := v.URL, "X-Vestaboard-Token"
	var payload any = struct {
		Characters Board `json:"characters"`
		Forced     bool  `json:"forced,omitempty"`
	}{b, v.Force}
	if v.Legacy {
		header, payload = "X-Vestaboard-Read-Write-Key", b
		if url == "" {
			url = "https://rw.vestaboard.com/"
		}
	}
	if url == "" {
		url = "https://cloud.vestaboard.com/"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set(header, v.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	answer := fmt.Sprintf("HTTP %d %s", resp.StatusCode, strings.Join(strings.Fields(string(snippet)), " "))
	switch {
	case resp.StatusCode == http.StatusConflict && strings.Contains(string(snippet), "FingerprintMatch"):
		// The Cloud API refuses to resend the message it has on record as displayed. That is not
		// a failure (e.g. a one-page leaderboard is identical every half hour) - but keep it visible.
		return "unchanged, the Cloud API says this exact message is already displayed: " + answer, nil
	case resp.StatusCode == http.StatusLocked:
		return "", fmt.Errorf("vestaboard is muted / in quiet hours, answered %s (set VESTABOARD_FORCE=true to send anyway)", answer)
	case resp.StatusCode/100 != 2:
		return "", fmt.Errorf("vestaboard answered %s", answer)
	}
	return answer, nil
}
