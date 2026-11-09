package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// LeaderboardBoard renders the top 10 in two columns of five (rank + 7 chars).
func LeaderboardBoard(lb *Leaderboard, year int) Board {
	b := frame(fmt.Sprintf("ADVENT OF CODE %d!", year))
	ranked := lb.Ranked()
	prevScore, prevRank := -1, 0
	for i := 0; i < 10 && i < len(ranked); i++ {
		m := ranked[i]
		rank := i + 1
		if m.LocalScore == prevScore {
			rank = prevRank // tie
		}
		prevScore, prevRank = m.LocalScore, rank
		cell := text(fmt.Sprintf("%2d %-7.7s", rank, shortName(m.DisplayName())), 10)
		row, col := 1+i%5, 1+(i/5)*10
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

type Vestaboard struct {
	HTTP   *http.Client
	URL    string // default https://rw.vestaboard.com/
	APIKey string
}

func (v *Vestaboard) Send(ctx context.Context, b Board) error {
	url := v.URL
	if url == "" {
		url = "https://rw.vestaboard.com/"
	}
	body, err := json.Marshal(b)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-Vestaboard-Read-Write-Key", v.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("vestaboard returned HTTP %d", resp.StatusCode)
	}
	return nil
}
