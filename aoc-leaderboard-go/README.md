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

## How it fits together
```
GitHub Actions cron (aoc-trigger) ──POST /run──▶ STACKIT Function (this container) ──▶ adventofcode.com
                                                                    ├──▶ Slack (incoming webhook)
GitHub Actions (deploy-stackit) ──build/push/deploy──▶              └──▶ Vestaboard (Read/Write API)
```
STACKIT Functions only supports HTTP triggers and scales to zero, so it can't run a schedule itself – the cron
lives in GitHub Actions. There is no Infrastructure-as-Code (Terraform etc.) for Functions that we know of; as far as
the [Functions docs](https://docs.functions.onstackit.cloud/basics/concepts.html) go, the `sfn` CLI and its
`.stackit-functions/*.yaml` manifests are the way to manage it.

## HTTP contract
- `POST /run` – do one update. Optional `?mode=leaderboard|stars` (bypasses the active-window check),
  `?lookback=8760h` (override the stars window) and `?dry=1` (fetch only, post nothing).
  If `TRIGGER_TOKEN` is set, send `Authorization: Bearer <token>`.
- `GET /healthz`
- Listens on `$PORT` (default 8080); shuts down gracefully on SIGTERM.

## Configuration (env)
| var | required | |
|---|---|---|
| `AOC_LEADERBOARD_ID` | yes | number at the end of the private leaderboard URL |
| `AOC_SESSION` | yes | AoC session cookie, see below |
| `SLACK_WEBHOOK_URL` | no | Slack incoming webhook; skipped if unset |
| `VESTABOARD_API_KEY` | no | Vestaboard Read/Write key; skipped if unset |
| `AOC_YEAR` | no | event year; default: the current year |
| `TRIGGER_TOKEN` | no, but **set it** | protects `/run` (the function URL is public) |
| `STARS_LOOKBACK`, `ACTIVE_FROM_HOUR`, `ACTIVE_TO_HOUR`, `TIMEZONE`, `ALWAYS_ACTIVE=true`, `AOC_USER_AGENT` | no | |

Treat `AOC_SESSION`, `SLACK_WEBHOOK_URL`, `VESTABOARD_API_KEY` and `TRIGGER_TOKEN` as secrets.

## Getting the credentials
**AoC session cookie (`AOC_SESSION`).** Log in at adventofcode.com, open the browser dev tools →
Application (Chrome/Edge) or Storage (Firefox) → Cookies → `https://adventofcode.com` → copy the **value** of `session`
(a ~96 character hex string, without the `session=` prefix). It must belong to a member of the leaderboard (the owner is
safest). It is a login credential: keep it only in the GitHub secret. It expires after roughly a month and stops working
if you log out; symptom is `advent of code returned HTTP … (expired session cookie?)` – copy a new one and redeploy.

**Leaderboard id (`AOC_LEADERBOARD_ID`).** `adventofcode.com/<year>/leaderboard/private/view/<ID>`. The id can differ per year.

**Slack webhook (`SLACK_WEBHOOK_URL`).** Reuse last year's Slack app: https://api.slack.com/apps → the app →
*Incoming Webhooks* (toggle on) → *Add New Webhook to Workspace* → pick the channel → copy the
`https://hooks.slack.com/services/…` URL. Remove last year's webhook once the new one works. Quick test:
`curl -X POST -H 'Content-type: application/json' --data '{"text":"test"}' <url>` should answer `ok`.

**Vestaboard (`VESTABOARD_API_KEY`).** The *Read/Write* key of the board. Not tested yet – verify at the office with a manual
**aoc-trigger** run.

## Deploy on STACKIT Functions
Per the Functions docs the container must be `linux/amd64`, serve HTTP on `$PORT`, handle SIGTERM and run as a numeric
non-root user – the Dockerfile does all four. Images tagged `latest` are rejected unless combined with a digest, so the
pipeline tags with the commit SHA.

### 1. Install `sfn` (once, locally)
- **Linux/macOS:** `curl -o install-sfn.sh https://raw.githubusercontent.com/stackitcloud/stackit-functions-cli/refs/heads/main/install-sfn.sh && bash install-sfn.sh`
- **Windows:** the install script does **not** work (not even in Git Bash: `Unknown operating system: MINGW64_NT-…`). Download the
  `sfn_<version>_windows_amd64.zip` from <https://github.com/stackitcloud/stackit-functions-cli/releases/latest>, extract it
  somewhere (e.g. `%LOCALAPPDATA%\sfn`), add that folder to your user `PATH` and **open a new terminal** (already-open shells keep
  the old PATH). Check with `sfn --version`. There is no known Chocolatey package for it – don't `choco install sfn`.
  Unblock the exe (`Unblock-File`) if Windows blocks it. Or use WSL with the Linux script.

### 2. One-time STACKIT setup
1. `sfn auth login --project-id <id>` (opens a browser; a user account is needed at least once because it enables the
   Functions service for the project). Your account needs the owner, editor or *Functions Admin* role.
   *If it says "Service not enabled" right after enabling it, wait a minute or two and retry* (`sfn functions list` verifies).
2. Generate the manifests. `sfn function create` only works inside an existing workspace, so first bootstrap one in a scratch
   folder and copy its manifests over:
   ```sh
   cd ~ && sfn function bootstrap --project-id <id>      # Go, HTTP trigger, name: aoc-leaderboard
   cp -r ~/aoc-leaderboard/.stackit-functions <repo>/aoc-leaderboard-go/
   cd <repo>/aoc-leaderboard-go && sfn function create --function-name aoc-leaderboard
   ```
   Commit `.stackit-functions/` (it holds the function id). Ignore the template's source code; the template's `image`/`runtime`
   fields are irrelevant because we deploy our own image.
3. Registry pull secret for the runtime: `sfn pull-secrets create --address <registry>` (use a pull-only robot account).
   Without it deploys fail with *502 Bad Gateway*.
4. A service account with the **Functions Admin** role and a JSON key for it.

### 3. GitHub repository settings
**Variables:** `STACKIT_REGISTRY` (e.g. `registry.onstackit.cloud`), `STACKIT_IMAGE` (full image path, no tag),
`STACKIT_PROJECT_ID`; optional `STACKIT_FUNCTION_NAME` (default `aoc-leaderboard`), `AOC_YEAR`, `STACKIT_PULL_SECRET_NAME` (cleanup only).

**Secrets:** `STACKIT_SERVICE_ACCOUNT_KEY`, `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` (push access), `AOC_LEADERBOARD_ID`,
`AOC_SESSION`, `SLACK_WEBHOOK_URL`, `VESTABOARD_API_KEY`, `AOC_TRIGGER_TOKEN` (same value deployed as `TRIGGER_TOKEN`),
`AOC_FUNCTION_URL` (`https://<id>.functions.onstackit.cloud`, printed by the first successful deploy).

## Workflows
| workflow | trigger | does |
|---|---|---|
| `deploy-stackit` | push to `master` touching this folder, or manual | test → build amd64 image → push (tag = SHA) → `sfn function deploy` with the runtime env |
| `aoc-trigger` | cron every 30 min in December, or manual | `POST /run` on the function |
| `cleanup-stackit` | manual only | delete the function (type its name to confirm); optionally the pull secret and the registry repository |

Notes:
- The default branch is **`master`**; the deploy workflow triggers on that.
- Runtime variables are written to a temporary, gitignored `.deploy.env` and passed with `sfn function deploy --env-file`.
  The CLI reference only says "name of the environment variables file"; **not verified yet** how that combines with
  `spec.envs` in `revision.yaml`. If the first deployed revision lacks the variables, put them in `revision.yaml` instead
  (`MY_VAR: { env: LOCAL_ENV_NAME }` reads from the shell at deploy time).
- Registry deletion in `cleanup-stackit` assumes the Harbor API of STACKIT Container Registry and a registry account that may
  delete repositories (**not verified**).
- Cleanup does not remove the service account, repo secrets/variables, or the committed `.stackit-functions/` manifests
  (delete those manually; they reference the removed function id).
- The scheduled workflow only fires in December (UTC hours 06–20); the function enforces the exact Amsterdam window itself,
  so a late or skipped GitHub tick is harmless. GitHub may pause scheduled workflows on repos with no activity for 60 days.

## Choosing the year / testing on 2025
The year is the `AOC_YEAR` env var (default: current year). Set it permanently with the repository variable `AOC_YEAR`,
or per deploy via *Run workflow* → `year`. To test against last year's board outside December:
1. Run **deploy-stackit** manually with `year = 2025` (tick `always_active` if scheduled runs should post outside December too).
2. Run **aoc-trigger** manually with `mode = leaderboard`, or `mode = stars` and `lookback = 8760h`
   (otherwise "latest stars" only covers the last hour and is empty for old data).
3. Redeploy with `year` empty (and the `AOC_YEAR` variable removed) before December 2026.

`AOC_LEADERBOARD_ID` is per year – update the secret if the 2025 board has a different id, and make sure the session cookie
belongs to a member of that board.

## Troubleshooting
| symptom | cause / fix |
|---|---|
| `Unknown operating system: MINGW64_NT…` | install script is Linux/macOS only; use the Windows `.zip` (above) |
| `sfn` not found after adding to PATH | open a new terminal (also Git Bash / VS Code); check `where.exe sfn` |
| `Error: Required manifest missing in workspace` | run `sfn function bootstrap` first (step 2), `create` needs an existing workspace |
| `Could not verify connection … Service not enabled` | just enabled; wait a minute or two and retry; check your role on the project |
| deploy → 502 Bad Gateway | no registry pull secret (`sfn pull-secrets create`) |
| deploy workflow never starts | it triggers on `master`; check the path filter or start it manually |
| `advent of code returned HTTP …` | expired/wrong `AOC_SESSION`, or wrong leaderboard id/year |
| `/run` answers 401 | missing/incorrect `Authorization: Bearer <TRIGGER_TOKEN>` |
| `/run` answers "skipped (outside active window)" | not December or outside 08–21; use `?mode=` or `ALWAYS_ACTIVE=true` |
| "No new stars" in the test year | `lookback` too short; use `lookback=8760h` |

## Develop
```sh
go test ./...
AOC_LEADERBOARD_ID=… AOC_SESSION=… ALWAYS_ACTIVE=true go run . &
curl -XPOST 'localhost:8080/run?mode=stars&dry=1'
```
