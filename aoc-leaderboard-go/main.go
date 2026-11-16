package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
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
	lookback, err := time.ParseDuration(env("STARS_LOOKBACK", "30m"))
	if err != nil {
		slog.Error("bad STARS_LOOKBACK", "err", err)
		os.Exit(1)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	j := &Job{
		Cfg: Config{
			Year: year, Loc: loc,
			FirstDay:             atoi(env("EVENT_FIRST_DAY", ""), 1),
			LastDay:              atoi(env("EVENT_LAST_DAY", ""), 12),
			SlackLeaderboardHour: atoi(env("SLACK_LEADERBOARD_HOUR", ""), 8),
			SlackStarsFrom:       atoi(env("SLACK_STARS_FROM_HOUR", ""), 6),
			SlackStarsTo:         atoi(env("SLACK_STARS_TO_HOUR", ""), 22),
			VestaFrom:            atoi(env("VESTA_FROM_HOUR", ""), 8),
			VestaTo:              atoi(env("VESTA_TO_HOUR", ""), 17),
			VestaWeekdaysOnly:    env("VESTA_WEEKDAYS_ONLY", "true") != "false",
			StarsLookback:        lookback,
			AlwaysActive:         env("ALWAYS_ACTIVE", "") == "true",
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
		j.Vesta = &Vestaboard{
			HTTP: client, APIKey: k, URL: os.Getenv("VESTABOARD_URL"),
			Force:  env("VESTABOARD_FORCE", "") == "true",
			Legacy: env("VESTABOARD_LEGACY", "") == "true",
		}
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
	// POST /run[?mode=leaderboard|stars][&target=slack|vesta][&lookback=8760h][&dry=1] — called by the schedule.
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
		q := r.URL.Query()
		opts := Options{Mode: Mode(q.Get("mode")), Target: Target(q.Get("target")), Dry: q.Get("dry") == "1"}
		switch opts.Mode {
		case ModeAuto, ModeLeaderboard, ModeStars:
		default:
			http.Error(w, "bad mode (use leaderboard or stars, or omit)", http.StatusBadRequest)
			return
		}
		switch opts.Target {
		case TargetBoth, TargetSlack, TargetVesta:
		default:
			http.Error(w, "bad target (use slack or vesta, or omit)", http.StatusBadRequest)
			return
		}
		if v := q.Get("lookback"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				http.Error(w, "bad lookback: "+err.Error(), http.StatusBadRequest)
				return
			}
			opts.Lookback = d
		}
		res, err := job.Run(ctx, opts)
		if err != nil {
			slog.Error("run failed", "err", err, "done", res.Actions)
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if len(res.Actions) == 0 {
			fmt.Fprintf(w, "skipped: %s\n", res.Reason)
			return
		}
		fmt.Fprintf(w, "done: %s\n", strings.Join(res.Actions, "; "))
	})

	addr := ":" + env("PORT", "8080")
	slog.Info("listening", "addr", addr, "year", job.Cfg.Year)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	// The platform sends SIGTERM when scaling down: finish in-flight runs first.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
