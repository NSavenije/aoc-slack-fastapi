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
- `POST /run` – do one update. Optional `?mode=leaderboard|stars` (bypasses the active-window check), `?lookback=8760h` (override the stars window) and `?dry=1`.
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
| `AOC_YEAR` | no | event year; default: the current year |
| `TRIGGER_TOKEN` | no | protects `/run` |
| `STARS_LOOKBACK`, `ACTIVE_FROM_HOUR`, `ACTIVE_TO_HOUR`, `TIMEZONE`, `ALWAYS_ACTIVE=true`, `AOC_USER_AGENT` | no | |

Store `AOC_SESSION`, `SLACK_WEBHOOK_URL`, `VESTABOARD_API_KEY` and `TRIGGER_TOKEN` as secrets, not plain env vars.

## Deploy on STACKIT Functions
Per the [Functions docs](https://docs.functions.onstackit.cloud/basics/concepts.html) the container must be
`linux/amd64`, serve HTTP on `$PORT`, handle SIGTERM and run as a numeric non-root user – the Dockerfile does all four.

```sh
docker build --platform linux/amd64 -t <registry>/<repo>/aoc-leaderboard:2026 .
sfn function deploy --build=false --push=true --image <registry>/<repo>/aoc-leaderboard:2026
```
(Run `sfn function create` first to get the `.stackit-functions/` manifests; the `latest` tag is rejected unless combined with a digest, so use a real tag.)

Environment variables go in `.stackit-functions/revision.yaml` under `spec.envs`. Read the secrets from your local
shell at deploy time rather than committing them:
```yaml
spec:
  envs:
    AOC_LEADERBOARD_ID: "123456"
    AOC_SESSION:        { env: AOC_SESSION }
    SLACK_WEBHOOK_URL:  { env: SLACK_WEBHOOK_URL }
    VESTABOARD_API_KEY: { env: VESTABOARD_API_KEY }
    TRIGGER_TOKEN:      { env: TRIGGER_TOKEN }
```
The function URL is public, so **always set `TRIGGER_TOKEN`**.

### CI/CD (GitHub Actions)
`.github/workflows/deploy-stackit.yml` tests, builds the image, pushes it (tag = commit SHA) and runs
`sfn function deploy` on every push to `main` that touches this directory (or manually via *Run workflow*).

One-time setup:
1. Locally: `sfn auth login --project-id <id>`, then in `aoc-leaderboard-go/` run
   `sfn function create --function-name aoc-leaderboard` and **commit** the generated `.stackit-functions/` manifests
   (they hold the function id; keep secrets out of them).
2. Registry pull secret for the runtime: `sfn pull-secrets create --address <registry>` (use a pull-only robot
   account). Without it deploys fail with a 502.
3. A service account with the **Functions Admin** role, and a JSON key for it.
4. Repository **variables**: `STACKIT_REGISTRY`, `STACKIT_IMAGE` (no tag), `STACKIT_PROJECT_ID`,
   optional `STACKIT_FUNCTION_NAME` (default `aoc-leaderboard`), `AOC_YEAR`, `STACKIT_PULL_SECRET_NAME` (for cleanup).
5. Repository **secrets**: `STACKIT_SERVICE_ACCOUNT_KEY`, `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` (push access),
   `AOC_LEADERBOARD_ID`, `AOC_SESSION`, `SLACK_WEBHOOK_URL`, `VESTABOARD_API_KEY`, `AOC_TRIGGER_TOKEN`.
   The runtime variables are written to a temporary `.deploy.env` and passed with `--env-file`.

### Choosing the year / testing on 2025
The year is the `AOC_YEAR` env var (default: current year). Set it permanently with the repository variable `AOC_YEAR`,
or per deploy via *Run workflow* → `year`. To test against last year's board outside December:
1. Run **deploy-stackit** manually with `year = 2025` (tick `always_active` if you also want scheduled runs to post).
2. Run **aoc-trigger** manually with `mode = leaderboard`, or `mode = stars` and `lookback = 8760h`
   (otherwise "latest stars" only covers the last hour and will be empty for old data).
3. Redeploy with `year` empty (or remove the `AOC_YEAR` variable) before December 2026.

Note the AoC session cookie must belong to a member of that year's private leaderboard, and `AOC_LEADERBOARD_ID`
is per year – update the secret if the 2025 board has a different id.

### Cleanup
**cleanup-stackit** (manual) deletes the function and its revisions after you type the function name. Optional extras:
delete the registry pull secret (`STACKIT_PULL_SECRET_NAME`) and delete the image repository from the registry (assumes the
Harbor API of STACKIT Container Registry). It does not remove the service account, the repo secrets/variables, or the
committed `.stackit-functions/` manifests – delete the manifests yourself, as they reference the removed function id.

### Scheduling
STACKIT Functions currently supports only HTTP triggers and scales to zero, so it can't run a cron itself.
`.github/workflows/aoc-trigger.yml` calls `POST /run` every 30 minutes in December. Add the repository secrets
`AOC_FUNCTION_URL` (e.g. `https://<id>.functions.onstackit.cloud`) and `AOC_TRIGGER_TOKEN` (same value as `TRIGGER_TOKEN`).
Because the leaderboard/stars choice comes from the clock, a late GitHub tick is harmless. Any other HTTP cron
caller works equally well.

## Develop
```sh
go test ./...
AOC_LEADERBOARD_ID=… AOC_SESSION=… ALWAYS_ACTIVE=true go run . &
curl -XPOST 'localhost:8080/run?mode=stars&dry=1'
```
