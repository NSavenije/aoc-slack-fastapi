package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // timezone data inside minimal images
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		slog.Error("missing required environment variable", "name", k)
		os.Exit(1)
	}
	return v
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

func newJob() *Job {
	loc, err := time.LoadLocation(env("TIMEZONE", "Europe/Amsterdam"))
	if err != nil {
		slog.Error("bad TIMEZONE", "err", err)
		os.Exit(1)
	}
	year := atoi(env("AOC_YEAR", ""), time.Now().In(loc).Year())
	lookback, err := time.ParseDuration(env("STARS_LOOKBACK", "60m"))
	if err != nil {
		slog.Error("bad STARS_LOOKBACK", "err", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	j := &Job{
		Cfg: Config{
			Year: year, Loc: loc,
			ActiveFrom:    atoi(env("ACTIVE_FROM_HOUR", "8"), 8),
			ActiveTo:      atoi(env("ACTIVE_TO_HOUR", "21"), 21),
			StarsLookback: lookback,
			AlwaysActive:  env("ALWAYS_ACTIVE", "") == "true",
		},
		AoC: &AoCClient{
			HTTP: client, Year: year,
			BoardID:       mustEnv("AOC_LEADERBOARD_ID"),
			SessionCookie: mustEnv("AOC_SESSION"),
			UserAgent:     env("AOC_USER_AGENT", "github.com/nsavenije/aoc-leaderboard-go (private leaderboard notifier)"),
		},
		Now: time.Now,
	}
	if k := os.Getenv("VESTABOARD_API_KEY"); k != "" {
		j.Vesta = &Vestaboard{HTTP: client, APIKey: k}
	}
	if u := os.Getenv("SLACK_WEBHOOK_URL"); u != "" {
		j.Slack = &Slack{HTTP: client, WebhookURL: u}
	}
	if j.Vesta == nil && j.Slack == nil {
		slog.Warn("neither VESTABOARD_API_KEY nor SLACK_WEBHOOK_URL set: nothing will be posted")
	}
	return j
}

func main() {
	job := newJob()
	token := os.Getenv("TRIGGER_TOKEN")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	// POST /run[?mode=leaderboard|stars][&dry=1] — called by the platform's schedule.
	mux.HandleFunc("POST /run", func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		mode, err := job.Run(ctx, Mode(r.URL.Query().Get("mode")), r.URL.Query().Get("dry") == "1")
		if err != nil {
			slog.Error("run failed", "err", err)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if mode == "" {
			fmt.Fprintln(w, "skipped (outside active window)")
			return
		}
		fmt.Fprintf(w, "posted %s\n", mode)
	})

	// Optional: for platforms without a scheduler, tick at :00 and :30 ourselves.
	if env("INTERNAL_SCHEDULER", "") == "true" {
		go func() {
			for {
				now := time.Now()
				time.Sleep(now.Truncate(30 * time.Minute).Add(30 * time.Minute).Sub(now))
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				if _, err := job.Run(ctx, ModeAuto, false); err != nil {
					slog.Error("scheduled run failed", "err", err)
				}
				cancel()
			}
		}()
	}

	addr := ":" + env("PORT", "8080")
	slog.Info("listening", "addr", addr, "year", job.Cfg.Year)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
