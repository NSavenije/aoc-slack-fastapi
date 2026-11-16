# aoc-leaderboard-go

Go container for our private Advent of Code leaderboard. It posts to **Slack** and to the **Vestaboard**.

| | when (Europe/Amsterdam) | what |
|---|---|---|
| **Slack** – leaderboard | every morning at 08:00 (any day of the event) | the full leaderboard |
| **Slack** – new stars | every 15 minutes, 06:00–22:00 | one message with all stars earned in the previous 15 minutes; **no message if there are none** |
| **Vestaboard** | every 30 minutes, **Mon–Fri 08:00–17:00** | a star earned in the previous 30 minutes is shown instead (newest 5); otherwise the next **leaderboard page** (10 people per page, pages cycle with the clock) |

All of it only happens on the event days, **December 1–12** (`EVENT_FIRST_DAY`/`EVENT_LAST_DAY`), and only if `AOC_YEAR`
is the current year.

Everything is derived from the clock, so the service holds **no state** and is safe on a scale-to-zero FaaS platform. The
trade-off: "new stars" are the stars whose timestamp falls in the previous *aligned* 15/30-minute window (e.g. a call at 08:00:20
reports 07:45:00–08:00:00). If a scheduled call is skipped entirely, the stars of that window are never reported; if two calls land in
the same window, they are reported twice. GitHub's scheduler occasionally does either.

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
- `POST /run` – with no parameters it does whatever is due *now* (the schedule above); the response says what was posted
  (`done: slack: leaderboard; vesta: leaderboard page 2/3`) or why not (`skipped: outside the event days (December 1-12)`).
  Optional parameters, mainly for testing (they bypass the schedule and the event-day check):
  `?mode=leaderboard|stars`, `?target=slack|vesta` (only that target, e.g. to test the board without posting to Slack),
  `?lookback=8760h` (stars window for `mode=stars`, a unit is required) and `?dry=1` (fetch, post nothing).
  A manual `mode=stars` posts even when there are no stars. If `TRIGGER_TOKEN` is set, send `Authorization: Bearer <token>`.
- `GET /healthz`
- Listens on `$PORT` (default 8080); shuts down gracefully on SIGTERM.

## Configuration (env)
| var | required | |
|---|---|---|
| `AOC_LEADERBOARD_ID` | yes | number at the end of the private leaderboard URL; in CI it may be a repository variable or a secret |
| `AOC_SESSION` | yes | AoC session cookie, see below |
| `SLACK_WEBHOOK_URL` | no | Slack incoming webhook; skipped if unset |
| `VESTABOARD_API_KEY` | no | Vestaboard Read/Write key; skipped if unset |
| `AOC_YEAR` | no | event year; default: the current year |
| `TRIGGER_TOKEN` | no, but **set it** | protects `/run` (the function URL is public) |
| `EVENT_FIRST_DAY`, `EVENT_LAST_DAY` | no | December days on which the schedule is active (default `1` and `12`) |
| `SLACK_LEADERBOARD_HOUR` | no | morning leaderboard hour (default `8`) |
| `SLACK_STARS_FROM_HOUR`, `SLACK_STARS_TO_HOUR` | no | Slack new-stars hours, `to` exclusive (default `6` and `22`; puzzles unlock at 06:00) |
| `VESTA_FROM_HOUR`, `VESTA_TO_HOUR`, `VESTA_WEEKDAYS_ONLY` | no | Vestaboard hours (default `8`, `17`) and Mon–Fri only (default `true`) |
| `STARS_LOOKBACK` | no | default window for a manual `mode=stars` (default `30m`) |
| `ALWAYS_ACTIVE=true` | no | ignore the year and event-day check (opening hours still apply) |
| `TIMEZONE`, `AOC_USER_AGENT` | no | default `Europe/Amsterdam` |

Treat `AOC_SESSION`, `SLACK_WEBHOOK_URL`, `VESTABOARD_API_KEY` and `TRIGGER_TOKEN` as secrets.

## Getting the credentials
**AoC session cookie (`AOC_SESSION`).** Log in at adventofcode.com, open the browser dev tools →
Application (Chrome/Edge) or Storage (Firefox) → Cookies → `https://adventofcode.com` → copy the **value** of `session`
(a ~96 character hex string, without the `session=` prefix). It must belong to a member of the leaderboard (the owner is
safest). It is a login credential: keep it only in the GitHub secret. It expires after roughly a month and stops working
if you log out; symptom is `advent of code returned HTTP … (expired session cookie?)` – copy a new one and redeploy.

**Leaderboard id (`AOC_LEADERBOARD_ID`).** `adventofcode.com/<year>/leaderboard/private/view/<ID>`. It is normally the owner's AoC user id, so it stays the same across years (but the board for that year must exist). It isn't sensitive by itself, so a repository variable is fine.

**Slack webhook (`SLACK_WEBHOOK_URL`).** Reuse last year's Slack app: https://api.slack.com/apps → the app →
*Incoming Webhooks* (toggle on) → *Add New Webhook to Workspace* → pick the channel → copy the
`https://hooks.slack.com/services/…` URL. Remove last year's webhook once the new one works. Quick test:
`curl -X POST -H 'Content-type: application/json' --data '{"text":"test"}' <url>` should answer `ok`.

**Vestaboard (`VESTABOARD_API_KEY`).** The *Read/Write* key of the board. Not tested yet – verify at the office with a manual
**aoc-trigger** run.

## Deploy on STACKIT Functions
Per the Functions docs the container must be `linux/amd64`, serve HTTP on `$PORT`, handle SIGTERM and run as a numeric
non-root user – the Dockerfile does all four. The runtime only accepts a SemVer image tag (with or without `v`), or `latest` combined with a digest
(a commit-SHA tag is rejected: `oci image reference invalid`), so the pipeline tags images `1.0.<run number>` and stores the commit
in the `org.opencontainers.image.revision` label.

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
2. **Container registry** (by hand in the STACKIT portal; no IaC/CLI route known). Functions needs a registry to pull from:
   create a *Container Registry*, then a *project* inside it (its name becomes part of the image path), host is
   `registry.onstackit.cloud`. Then create **two robot accounts** in that registry project (*Robot Accounts* tab → *New Robot Account*;
   the password is shown only once, so copy it immediately; avoid short expiries, an expired account breaks deploys/pulls):

   | robot | permissions to tick | used for |
   |---|---|---|
   | pull (e.g. `stackit-functions-pull`) | **Repository → Pull** only | the Functions runtime (step 4) |
   | push (e.g. `github-actions-push`) | **Repository → Pull** and **Push** (add **Artifact → Create/Read** if a push is refused) | GitHub secrets `REGISTRY_USERNAME` / `REGISTRY_PASSWORD` |

   Robot usernames look like `robot$name`; paste them exactly (including the `$`).
3. **Manifests.** `sfn function create` only works inside an existing workspace, so generate one with `sfn function bootstrap`
   (Go runtime, HTTP trigger, name `aoc-leaderboard`). It creates a *nested* folder `<name>/` below where you ran it. Run
   `create` **inside that folder** (the function id is written into its `function.yaml`), then copy the manifests next to our code and delete the template:
   ```powershell
   sfn function bootstrap --project-id <id>
   cd .\aoc-leaderboard
   sfn function create --function-name aoc-leaderboard
   sfn functions list --project-id <id>                         # should list it
   cd .. ; Copy-Item -Recurse -Force .\aoc-leaderboard\.stackit-functions .\ ; Remove-Item -Recurse -Force .\aoc-leaderboard
   ```
   Commit `.stackit-functions/` (function id, project id, plan/concurrency – no secrets). The template's source, `image` and `runtime`
   are irrelevant because we deploy our own image. Already done for this repo; only needed again after a cleanup.
4. **Registry pull secret** for the runtime, with the *pull* robot: `sfn pull-secrets create --address registry.onstackit.cloud`
   (prompts for username/password; `sfn pull-secrets list` to check). Without it deploys fail with *502 Bad Gateway*.
   The Functions docs also mention `sfn config registry add <host>` for the same purpose; use `sfn pull-secrets --help` if one of them is not recognised.
5. **Service account** for the pipeline: portal → create a service account (e.g. `functions-deployment-pipeline`), give it the
   **Functions Admin** role on the project, create a key and copy the JSON.

### 3. GitHub repository settings
Settings → Secrets and variables → Actions.

**Variables**
| name | value |
|---|---|
| `STACKIT_REGISTRY` | `registry.onstackit.cloud` (host only) |
| `STACKIT_IMAGE` | `registry.onstackit.cloud/<registry-project>/aoc-leaderboard` (no tag; `<registry-project>` is the project *inside the registry*, not the STACKIT project id) |
| `STACKIT_PROJECT_ID` | STACKIT project id |
| `AOC_LEADERBOARD_ID` | may be a variable *or* a secret (not sensitive on its own) |
| `AOC_YEAR` | optional; **remove it again after testing a past year** |
| `STACKIT_FUNCTION_NAME` / `STACKIT_PULL_SECRET_NAME` | optional (default function name `aoc-leaderboard`; pull secret name only for cleanup) |

**Secrets**
| name | value |
|---|---|
| `REGISTRY_USERNAME`, `REGISTRY_PASSWORD` | push robot account |
| `STACKIT_SERVICE_ACCOUNT_KEY` | the service account's JSON key (whole JSON) |
| `AOC_SESSION` | AoC session cookie |
| `AOC_TRIGGER_TOKEN` | long random string; deployed as `TRIGGER_TOKEN` *and* sent as bearer token by `aoc-trigger`. e.g. PowerShell: `-join ((48..57)+(65..90)+(97..122) \| Get-Random -Count 40 \| % {[char]$_})` |
| `SLACK_WEBHOOK_URL` | Slack incoming webhook |
| `VESTABOARD_API_KEY` | optional until the board is tested |
| `AOC_FUNCTION_URL` | `https://<id>.functions.onstackit.cloud`, known only after the first successful deploy (`sfn functions list`/`describe`); needed by `aoc-trigger` |

The deploy job starts with a **Check configuration** step that lists every missing variable/secret by name in one error.

## Workflows
| workflow | trigger | does |
|---|---|---|
| `deploy-stackit` | push to `master` that changes Go source (not `*_test.go`), `go.mod`/`go.sum` or the `Dockerfile`; or manual (*Run workflow*) | test → build amd64 image → push (tag = `1.0.<run number>`) → `sfn function deploy` with the runtime env |
| `aoc-trigger` | cron every 15 min on **December 1–12** only, or manual | `POST /run` on the function (manual inputs: `mode`, `target`, `lookback`) |
| `cleanup-stackit` | manual only | delete the function (type its name to confirm); optionally the pull secret and the registry repository |

Notes:
- The default branch is **`master`**; the deploy workflow triggers on that. Edits to the README, tests, workflows or `.stackit-functions/`
  (e.g. plan/concurrency) do **not** redeploy automatically – start *Run workflow* manually after changing those.
- Runtime variables are passed to `sfn function deploy` as repeated `--env KEY=VALUE` flags (empty optional ones are skipped; the
  log lists variable *names* only). `--env-file` was tried first and did **not** work: the revision never became ready, apparently
  because the variables didn't arrive and the app exits at startup without `AOC_LEADERBOARD_ID`/`AOC_SESSION`. The same image
  ran fine on STACKIT Functions when started with `--env`.
- `sfn function deploy` only waits about 2 minutes for a revision, but starting one can take longer (`Error: Function revision deployment
  timed out`, although it comes up afterwards). The workflow therefore deploys with `--no-wait` and polls
  `sfn functions describe` for up to 10 minutes until the state is `Active` on a *new* revision, then calls `/healthz`. It fails
  early if the state contains "fail"/"error" or the `Error` field is set.
- Registry deletion in `cleanup-stackit` assumes the Harbor API of STACKIT Container Registry and a registry account that may
  delete repositories (**not verified**).
- Cleanup does not remove the service account, repo secrets/variables, or the committed `.stackit-functions/` manifests
  (delete those manually; they reference the removed function id).
- The cron (`*/15 5-21 1-12 12 *`, UTC) only fires on December 1–12; the function enforces the exact Amsterdam windows itself, so
  a late or skipped GitHub tick is harmless. Change the day range in the workflow *and* `EVENT_LAST_DAY` if the event length changes
  (the function would otherwise still skip those days). GitHub may pause scheduled workflows on repos with no activity for 60 days.

## Choosing the year / testing on 2025
The year is the `AOC_YEAR` env var (default: current year). Set it permanently with the repository variable `AOC_YEAR`,
or per deploy via *Run workflow* → `year`. To test against last year's board outside December:
1. Run **deploy-stackit** manually with `year = 2025` (tick `always_active` only if you want *scheduled* runs outside December to post).
2. Run **aoc-trigger** manually with `mode = leaderboard`, or `mode = stars` and `lookback = 8760h` (add `target = vesta` to leave Slack alone)
   (otherwise "latest stars" only covers the last hour and is empty for old data).
3. Redeploy with `year` empty (and the `AOC_YEAR` variable removed) before December 2026. If you forget, the scheduled run refuses to post
   (`AOC_YEAR is 2025 but it is 2026 …`) instead of reposting last year's board.

`AOC_LEADERBOARD_ID` is per year – update it if the 2025 board has a different id, and make sure the session cookie
belongs to a member of that board.

## Troubleshooting
| symptom | cause / fix |
|---|---|
| `Unknown operating system: MINGW64_NT…` | install script is Linux/macOS only; use the Windows `.zip` (above) |
| `sfn` not found after adding to PATH | open a new terminal (also Git Bash / VS Code); check `where.exe sfn` |
| `Error: Required manifest missing in workspace` | run `sfn function bootstrap` first (step 2), `create` needs an existing workspace |
| `Could not verify connection … Service not enabled` | just enabled; wait a minute or two and retry; check your role on the project |
| deploy → 502 Bad Gateway | no registry pull secret (`sfn pull-secrets create`) or the pull robot lacks Repository → Pull |
| `unexpected value 'false' for '--build'` | `--build`/`--push` are bare flags in `sfn`; the docs' `--build=false` example is wrong. The workflow uses `FUNC_BUILD=false` / `FUNC_PUSH=false` instead |
| `oci image reference invalid … Invalid tag` | the tag isn't SemVer (a commit SHA fails); the pipeline uses `1.0.<run number>` |
| revision stays `Creating` (ingress answers `404` from `istio-envoy`, no logs) | the container isn't starting; most often the required variables didn't reach it (the app exits without `AOC_LEADERBOARD_ID`/`AOC_SESSION`). Use `--env`, not `--env-file`; verify with `curl <function-url>/healthz` (should be `ok`) |
| deploy log: `Function revision deployment timed out` (older workflow versions) | the revision was created and usually comes up a few minutes later; check `sfn functions list` (State `Active`). Current workflows poll instead of failing |
| `denied` while `sfn` pulls `ghcr.io/stackitcloud/functions-builder` (only when building templates locally) | stale ghcr.io login in Docker: `docker logout ghcr.io` |
| `aoc-trigger` is green but nothing is posted | older versions of the workflow hid redirects/empty replies; the current one prints the HTTP status and body and fails on anything but a 2xx with a body. Check `AOC_FUNCTION_URL` starts with `https://`, and `lookback` has a unit (`8760h`, not `8760`) |
| deploy: `Deploy not configured` | the error lists the missing variables/secrets (Settings → Secrets and variables → Actions) |
| `docker login` / push denied | wrong push-robot credentials, or the robot lacks Repository → Push |
| deploy workflow never starts | it triggers on `master`; check the path filter or start it manually |
| `advent of code returned HTTP …` | expired/wrong `AOC_SESSION`, or wrong leaderboard id/year |
| `/run` answers 401 | missing/incorrect `Authorization: Bearer <TRIGGER_TOKEN>` |
| `/run` answers `skipped: …` | the reason is in the message: outside December 1–12, wrong `AOC_YEAR`, or nothing scheduled at that time (e.g. Vestaboard is Mon–Fri 08:00–17:00 on the half hour). Use `?mode=` / `?target=` to test manually |
| "No new stars" in the test year | `lookback` too short; use `lookback=8760h` |

## Develop
```sh
go test ./...
AOC_LEADERBOARD_ID=… AOC_SESSION=… ALWAYS_ACTIVE=true go run . &
curl -XPOST 'localhost:8080/run?mode=stars&dry=1'
```
