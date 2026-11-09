# aoc-leaderboard-go

Go container for our private Advent of Code leaderboard. Every half hour it posts to
**Slack** and the **Vestaboard**, alternating between:

| half-hour slot | content |
|---|---|
| even (:00) | leaderboard (top 10 on the board, full list in Slack) |
| odd (:30)  | latest stars (stars earned in the last `STARS_LOOKBACK`, default 60m) |

The choice is derived from the clock (`unix/1800 % 2`), so the service holds **no state**
and is safe on a scale-to-zero FaaS platform. Updates are only posted during December,
between `ACTIVE_FROM_HOUR` and `ACTIVE_TO_HOUR` (Europe/Amsterdam, 08–21 by default).

## HTTP contract
- `POST /run` – do one update. Optional `?mode=leaderboard|stars` (bypasses the active-window check) and `?dry=1`.
  If `TRIGGER_TOKEN` is set, send `Authorization: Bearer <token>`.
- `GET /healthz`
- Listens on `$PORT` (default 8080).

## Configuration (env)
| var | required | |
|---|---|---|
| `AOC_LEADERBOARD_ID` | yes | |
| `AOC_SESSION` | yes | session cookie of a leaderboard member |
| `SLACK_WEBHOOK_URL` | no | Slack incoming webhook; skipped if unset |
| `VESTABOARD_API_KEY` | no | Read/Write key; skipped if unset |
| `AOC_YEAR` | no | default: current year |
| `TRIGGER_TOKEN` | no | protects `/run` |
| `STARS_LOOKBACK`, `ACTIVE_FROM_HOUR`, `ACTIVE_TO_HOUR`, `TIMEZONE`, `ALWAYS_ACTIVE=true`, `AOC_USER_AGENT` | no | |
| `INTERNAL_SCHEDULER=true` | no | tick at :00/:30 inside the process (only for always-on containers) |

Store `AOC_SESSION`, `SLACK_WEBHOOK_URL`, `VESTABOARD_API_KEY` and `TRIGGER_TOKEN` as secrets, not plain env vars.

## Deploy on STACKIT
```sh
docker build -t <registry>/aoc-leaderboard-go:2026 .
docker push <registry>/aoc-leaderboard-go:2026
```
Create the function from that image, set the env vars above, and add a schedule/cron
trigger `*/30 * * * *` that does `POST /run`. (If the platform's scheduler can't call an
HTTP endpoint, run it as an always-on container with `INTERNAL_SCHEDULER=true`.)

## Develop
```sh
go test ./...
AOC_LEADERBOARD_ID=… AOC_SESSION=… ALWAYS_ACTIVE=true go run . &
curl -XPOST 'localhost:8080/run?mode=stars&dry=1'
```
