<p align="center">
  <img src="assets/suuntool.png" alt="suuntool" width="200">
</p>

<h1 align="center">suuntool</h1>

<p align="center">
  <a href="https://golang.org/"><img src="https://img.shields.io/github/go-mod/go-version/tajchert/suuntool?logo=go&logoColor=white" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tajchert/suuntool" alt="License"></a>
  <a href="https://github.com/tajchert/suuntool/releases"><img src="https://img.shields.io/github/v/release/tajchert/suuntool?include_prereleases&sort=semver" alt="Release"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey" alt="Platform">
  <img src="https://img.shields.io/badge/MCP-ready-7c3aed" alt="MCP ready">
</p>

Fast, scriptable, agent-friendly CLI for the Suunto / Sports-Tracker API — the same backend the Suunto mobile app uses. Log in, pull your profile, pipe it into `jq`, automate it, hand it to an LLM. No browser, no app, no clicks.

It also doubles as an [MCP server](#mcp-server) — point Claude Desktop, Claude Code, or Codex at it and they can call every Suunto endpoint directly with typed arguments.

```bash
$ suuntool login --email you@example.com
Password:
Logged in as alice (you@example.com). Session saved to ~/.config/suuntool/session.json.

$ suuntool workouts list --limit 5
Date              Act  Type           Distance  Duration  Ascent  Key
2026-05-11 07:42  1    RUNNING        8.42 km   0:44:18   62 m    wk_abc123
2026-05-10 18:05  2    CYCLING        32.10 km  1:12:04   210 m   wk_abc124
2026-05-09 06:30  11   HIKING         5.80 km   1:05:00   140 m   wk_abc125
2026-05-08 07:10  1    RUNNING        10.05 km  0:52:30   78 m    wk_abc126
2026-05-07 19:20  22   TRAIL_RUNNING  1.20 km   0:28:11   0 m     wk_abc127
5 workouts  57.57km  3:42:21

$ suuntool workouts list --since 14d --summary
workouts:  5
distance:  57.57km
time:      3:42:21
ascent:    490 m
descent:   480 m

Per activity:
Act  Type           Count  Distance  Duration  ΔWoW
1    RUNNING        2      18.47km   1:36:48   +1
2    CYCLING        1      32.10km   1:12:04   0
11   HIKING         1      5.80km    1:05:00   -1
22   TRAIL_RUNNING  1      1.20km    0:28:11   0

$ suuntool wellness sleep --since 3d | jq -c '{date:.timestamp, nap:.entryData.isNap, quality:(.entryData.quality*100|round), hrBpm:(.entryData.hrAvg*60|round)}'
{"date":"2026-05-09T18:25:00Z","nap":true,"quality":0,"hrBpm":78}
{"date":"2026-05-10T02:46:00Z","nap":false,"quality":73,"hrBpm":67}
{"date":"2026-05-10T14:50:00Z","nap":true,"quality":0,"hrBpm":84}
{"date":"2026-05-11T01:55:00Z","nap":false,"quality":77,"hrBpm":69}
```

## Why

- **Agent-ready.** Stable exit codes, JSON-on-pipe defaults, machine-readable error envelopes, and `suuntool endpoints --format json` so an LLM can map intents to commands without scraping help text.
- **Scriptable.** One static binary. No Python, no node, no app sandbox.
- **Honest.** The signing logic is locked to golden vectors, so when the contract drifts, the tool fails loudly instead of pretending.

## Install

macOS or Linux with Homebrew:

```bash
brew install --cask tajchert/tap/suuntool
```

Windows prebuilt binaries are available as `.zip` files on the
[GitHub releases page](https://github.com/tajchert/suuntool/releases). Extract
`suuntool.exe` and place it in a directory on your `PATH`.

Or install from source on any supported platform:

```bash
go install github.com/tajchert/suuntool@latest
```

## Quickstart

```bash
suuntool login --email you@example.com              # interactive password prompt
echo 'hunter2' | suuntool login \
   --email you@example.com --password-stdin         # non-interactive (CI, agents)

# Profile
suuntool whoami                                     # current session user
suuntool profile settings                           # full user settings DTO
suuntool profile follow                             # follower/following counts
suuntool profile user alice                         # look up any user by handle

# Workouts
suuntool workouts list --limit 5                    # most recent 5 workouts
suuntool workouts list --limit 100 --summary        # totals over a window (per-activity + ΔWoW delta, colored on TTY)
suuntool workouts list --since 7d                   # last 7 days (also: 12h, 2w, last-week, 2026-01-01)
suuntool workouts list --since 2025-01-01 --stream > 2025.ndjson  # NDJSON, auto-paginates across cursors
suuntool workouts list --fields key,startTime,totalDistance      # project to a few JSON fields (forces JSON)
suuntool workouts get wk_abc123                     # one workout's metadata
suuntool workouts stats                             # aggregate stats for you
suuntool workouts count                             # workout count
suuntool workouts sml wk_abc123 -o wk.sml.json      # full ~5MB sample data
suuntool workouts fit wk_abc123 -o wk.fit           # binary .fit export
suuntool workouts export wk_abc123 --bundle ./wk    # metadata+sml+fit+ext+comments in one shot

# 24/7 wellness (gzipped NDJSON, decoded on the fly)
suuntool wellness sleep                             # pretty table on TTY, raw NDJSON when piped
suuntool wellness sleep --since 7d -o sleep.ndjson  # last 7 days (also: 2026-01-01, last-week)
suuntool wellness activity   --since last-month | jq '.entryData.stepCount'
suuntool wellness recovery   --out ./wellness
suuntool wellness sleepstages --out ./wellness

# Workout interactions
suuntool workouts comments wk_abc123                # list comments
suuntool workouts comment wk_abc123 "great run"     # post a comment (x-totp)
suuntool workouts react wk_abc123                   # like (x-totp)
suuntool workouts edit wk_abc123 --set totalAscent=120
suuntool workouts share wk_abc123 --as gpx-track    # signed GPX URL
suuntool workouts extensions wk_abc123              # Fitness/Intensity/…
suuntool workouts upload --sml ./wk.sml             # multipart upload
suuntool workouts delete wk_abc123 --yes            # destructive — needs --yes off-TTY

# SuuntoPlus Guides — transport only. suuntool moves the zip archive
# (manifest.json + guide.json + icon.png) as opaque bytes; it does not parse,
# build, or validate guide.json content.
suuntool guides list                                # every guide on the account (no pagination)
suuntool guides download g1 -o g1.zip               # fetch the archive
suuntool guides upload ./workout.zip                # create a new guide
suuntool guides update g1 ./workout-v2.zip          # replace content (not pinned status)
suuntool guides pin g1                               # pin a guide
suuntool guides unpin g1                             # unpin a guide
suuntool guides priority                             # account's guide priority order
suuntool guides delete g1 --yes                      # destructive — needs --yes off-TTY

suuntool doctor                                     # connectivity + session check
suuntool logout
```

## MCP server

`suuntool mcp` exposes every endpoint above as a [Model Context Protocol](https://modelcontextprotocol.io) tool over stdio. Point any MCP-capable client at it and an LLM can call Suunto endpoints directly with typed arguments — no shell scraping.

```bash
suuntool login                                    # one-time; the MCP server cannot prompt for a password
suuntool mcp                                      # read-only (default)
suuntool mcp --allow-write                        # + comment/react/edit/batch-update/share/extensions/upload, guides upload/update/pin/unpin
suuntool mcp --allow-write --allow-destructive    # + delete/uncomment/unreact, guides delete
```

| Tier | Flag | Tools |
|------|------|-------|
| read (default) | none | `whoami`, `profile_settings`/`follow`/`user`, `workouts_list`/`get`/`count`/`stats`/`sml`/`fit`/`comments`, `wellness_sleep`/`activity`/`recovery`/`sleepstages`, `activity_type_name`, `doctor`, `guides_list`/`download`/`priority` |
| write | `--allow-write` | `workouts_comment`/`react`/`edit`/`batch_update`/`share`/`extensions`/`upload`, `guides_upload`/`update`/`pin`/`unpin` (file bodies via base64) |
| destructive | `--allow-destructive` (requires `--allow-write`) | `workouts_delete`/`uncomment`/`unreact`, `guides_delete` |

Guide tools are transport only, same as the CLI: they move the zip archive as
opaque bytes and don't parse or build `guide.json` content.

`login`/`logout` are intentionally **not** exposed. Workout responses are enriched with `activityName` next to the numeric `activityId` so the LLM doesn't need a second lookup. Wellness NDJSON streams are buffered into `{items: [...]}` arrays with an optional `limit` — keep windows short for long histories.

> **`workouts_sml` and the 1 MB response cap.** The raw `/sml` body is ~5 MB on a typical ride and will exceed MCP's 1 MB response limit on anything longer than ~30 min. Pass `streams` (and optionally `downsample`) to get a filtered, structured subset instead of the base64 blob:
>
> ```jsonc
> // power curve / HR analysis for a 2h ride
> { "key": "abc123", "streams": ["HR", "Power", "Cadence"], "downsample": 2 }
> // → { key, sample_count, samples: [ { TimeISO8601, HR, Power?, Cadence? }, … ] }
> ```
>
> Available inner-Sample fields include `HR`, `Power`, `Cadence`, `GPSAltitude`, `Latitude`, `Longitude`, `UTC`, `EHPE`, `NumberOfSatellites`, `Events`. Samples that don't contain any requested field are dropped. Omit both args to keep the legacy `{key, base64}` passthrough.

<details>
<summary><strong>Claude Desktop</strong></summary>

Edit the desktop config file (quit and reopen Claude after editing):

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "suuntool": {
      "command": "suuntool",
      "args": ["mcp"]
    }
  }
}
```

For writes/deletes, expand `args`:

```json
"args": ["mcp", "--allow-write", "--allow-destructive"]
```

Claude Desktop does **not** inherit your shell's `$PATH`, so if `which suuntool` returns something like `/Users/you/go/bin/suuntool` (a `go install` build), use the absolute path in `command`. Homebrew installs (`/opt/homebrew/bin/suuntool` on Apple Silicon, `/usr/local/bin/suuntool` on Intel) are usually picked up automatically.

The 🔨 tool icon in the chat input should show `suuntool` once the app restarts.

</details>

<details>
<summary><strong>Claude Code</strong></summary>

One command — uses Claude Code's built-in MCP registry:

```bash
claude mcp add suuntool suuntool mcp                                          # read-only
claude mcp add suuntool -- suuntool mcp --allow-write                         # + writes
claude mcp add suuntool -- suuntool mcp --allow-write --allow-destructive     # + deletes
```

Note the `--` separator before `suuntool mcp` when passing tier flags — without it, `claude mcp add` tries to parse `--allow-write` as one of its own options and errors out with `unknown option '--allow-write'`.

Scope flags control where the entry is stored:

- `--scope local` (default): current project, this machine only
- `--scope user`: every project, this machine
- `--scope project`: written to `.mcp.json`, committed for the team

Verify inside a Claude Code session with the `/mcp` slash command — `suuntool` should appear with its tool count.

If `suuntool` isn't on `$PATH`, pass the absolute path: `claude mcp add suuntool -- /Users/you/bin/suuntool mcp`.

</details>

<details>
<summary><strong>Codex (OpenAI CLI)</strong></summary>

Edit `~/.codex/config.toml`:

```toml
[mcp_servers.suuntool]
command = "suuntool"
args = ["mcp"]
```

For writes/deletes:

```toml
[mcp_servers.suuntool]
command = "suuntool"
args = ["mcp", "--allow-write", "--allow-destructive"]
```

Codex MCP servers are loaded on session start — restart any running `codex` process. List configured servers with `codex mcp list`; verify the tools at runtime with the `/mcp` slash command.

Use an absolute path in `command` if `suuntool` isn't on the Codex process's `$PATH`.

</details>

### Sanity check

In any client, ask:

> "Use suuntool to show my last 3 workouts."

The model should call `workouts_list` with `limit: 3` and return rows with both `activityId` and `activityName`. If you see `AUTH_EXPIRED`, run `suuntool login` in a terminal and reconnect — the MCP server loads the on-disk session at startup and never re-auths silently.

## Output

`suuntool` picks a sensible default and gets out of the way:

| Flag | Effect |
|------|--------|
| `--format auto` (default) | Pretty on a TTY, JSON when piped or redirected |
| `--format json` | Force JSON (2-space indent) |
| `--format pretty` | Force pretty rendering — aligned tables for list responses (`workouts list`, `workouts stats`, `workouts comments`, `wellness sleep`, `guides list`); key/value blocks for single records |
| `--format tsv` | Tab-separated values for list responses (`workouts list`, `workouts stats`, `workouts comments`, `workouts list --summary`, `guides list`). Non-tabular responses fall back to JSON. Embedded tabs/newlines in cells are replaced with spaces |
| `-o, --output <path>` | Write to a file instead of stdout — format inferred from extension (`.json`, `.tsv`) |
| `--fields a,b,c` | Project list/object output to just these JSON keys before render (forces JSON). Skips piping through `jq` for trivial selection |
| `--no-color` | Disable ANSI styling (also honors `NO_COLOR`) |
| `--quiet`, `--verbose` | Tune log verbosity on stderr |
| `--timeout <dur>` | HTTP timeout, e.g. `45s` |

```bash
suuntool profile settings -o settings.json          # save to file
suuntool whoami --format json | jq .username        # pipe into jq
NO_COLOR=1 suuntool whoami                          # plain-text TTY
```

## Commands

### Session

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `login --email <e>` | `POST /v1/login2` | no | Reads password from a no-echo TTY prompt or `--password-stdin` |
| `logout` | `GET /v1/logout` | yes | Invalidates server-side, clears local session |
| `whoami` | `GET /v1/user` | yes | Current session user |
| `doctor` | `GET /v1/servertime` + session check | no | Probe connectivity and session validity |

### Profile

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `profile settings` | `GET /v1/user/settings` | yes | Full user settings DTO (passed through) |
| `profile follow` | `GET /v1/user/follow` | yes | Follower / following / blocked counts |
| `profile user <name>` | `GET /v1/user/name/{name}` | yes | Look up a user by username |

### Workouts

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `workouts list` | `GET /v1/workouts` | yes | Paginated list with `--since`, `--limit`, `--offset`; returns cursor `until`. `--summary` collapses the window into totals + per-activity table with a ΔWoW (week-over-week count change) column, green/red on a TTY. `--stream` emits NDJSON and auto-paginates across every `until` cursor (no 100-page ceiling; `--limit` becomes optional cap) |
| `workouts get <key>` | `GET /v1/workouts/{key}` | yes | One workout's metadata |
| `workouts stats [user]` | `GET /v1/workouts/{user}/stats` | yes | Aggregate totals + per-activity breakdown |
| `workouts count` | `GET /v1/workouts/count` | yes | Requires `until` + `sharingFlags` server-side (defaults handled) |
| `workouts sml <key>` | `GET /v1/workouts/{key}/sml` | yes | Full ~5MB sample-by-sample JSON. Raw passthrough — use `-o` |
| `workouts fit <key>` | `GET /v1/workout/exportFit/{key}` | yes | Binary `.fit`. Raw passthrough — use `-o` |
| `workouts export <key> --bundle <dir>` | (composite) | yes | One-shot bundle: writes `workout.json`, `workout.sml.json`, `workout.fit`, `extensions.json`, `comments.json` into `<dir>`. `--no-fit`/`--no-sml`/`--no-extensions`/`--no-comments` skip parts; `--force` overwrites a non-empty dir |

### Wellness (24/7 health timeline)

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `wellness sleep` | `GET 247.../v1/sleep/export` | yes | **Pretty table on TTY** (dedup'd by `sleepId`, units converted: Hz→BPM, fractions→%); raw NDJSON when piped or with `-o`/`--out` |
| `wellness activity` | `GET 247.../v1/activity/export` | yes | Per-15-min `hr`/`steps`/`energy`. **`hr` is in Hz** — ×60 for BPM |
| `wellness recovery` | `GET 247.../v1/recovery/export` | yes | `balance` ∈ 0..1 = "wake-up resources" |
| `wellness sleepstages` | `GET 247.../v1/sleepstages/export` | yes | Stage timeline (light/deep/REM/…) |

### Workout interactions & writes ⚠️

These mutate server state. The `delete` command requires `--yes` in non-TTY contexts.

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `workouts comments <key>` | `GET /v1/workouts/comments/{key}` | yes | List comments on a workout (note plural `comments/`) |
| `workouts comment <key> [text]` | `POST /v1/workouts/comment/{key}` | yes + **x-totp** | Post a comment; `--stdin` for multi-line |
| `workouts uncomment <comment-key>` | `DELETE /v1/workouts/comment/{commentKey}` | yes | Delete a comment by comment-key (NOT workout-key) |
| `workouts react <key>` | `POST /v1/workouts/reaction/{key}` | yes + **x-totp** | `--reaction like` (only supported value) |
| `workouts unreact <key>` | `DELETE /v1/workouts/reaction/{key}` | yes | Remove your reaction |
| `workouts edit <key> --set field=<json>` | `PUT /v1/workouts/{key}/attributes` | yes | Partial attribute update; values parsed as JSON literals |
| `workouts batch-update --file <json>` | `POST /v1/workouts/batchUpdate` | yes | Bulk updates from a JSON array |
| `workouts share <key> --as gpx-route\|gpx-track` | `PUT /v1/workouts/{user}/{key}/share/{format}` | yes | Returns a signed GPX URL |
| `workouts extensions <key>` | `POST /v1/workout/extensions/{key}` | yes | Despite POST, this is a fetch — body is the filter list |
| `workouts upload --sml <path> [--extensions <path>]` | `POST /v1/workout` (multipart) | yes | Upload a pre-built SML file. Streams via `io.Pipe`. **Does NOT generate SML from raw GPS** |
| `workouts delete <key>` | `DELETE /v1/workouts/{key}/delete` | yes | **Destructive.** TTY confirmation prompt; pass `--yes` in scripts/agents |

> **`x-totp` writes.** `comment` and `react` require a fresh 6-digit TOTP. `suuntool` auto-derives one from your session — you don't need to do anything. Codes rotate every 30s, so commands generate per call.
>
> **`--yes` and destructive operations.** `workouts delete` refuses to run on a non-TTY without `--yes` (exit code 2). This prevents agents and CI scripts from accidentally bypassing the confirmation by piping nothing into stdin.
>
> **Rate limiting.** Suunto's quotas are conservative (a few QPS). Don't batch-spam comments or reactions — your account can be flagged.

### Guides (SuuntoPlus structured workouts) ⚠️

Guides are the structured-workout archives the watch follows. suuntool moves the zip (`manifest.json` + `guide.json` + `icon.png`) as **opaque bytes** — it does not parse, build, or validate `guide.json` content. Authoring a workout is a separate concern from shipping it.

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `guides list` | `GET /v1/suuntoplus/guides/items` | yes | Every guide on the account. **No pagination** — the server accepts no `--since`/`--limit`/`--offset` here, unlike `workouts list` |
| `guides download <id>` | `GET /v1/suuntoplus/guides/files/{id}` | yes | Zip archive. Raw passthrough — use `-o`. The server reconstitutes the archive rather than echoing the upload byte-for-byte |
| `guides upload <zip>` | `POST /v1/suuntoplus/guides/files` | yes | Raw `application/zip` body (**not** multipart). A `guide.json` `externalId` that collides with an existing guide returns exit 5 with the server's own `Conflict` description. The response's `owner` always comes back `"Suunto"` regardless of what the manifest sent — see callout below |
| `guides update <id> <zip>` | `PUT /v1/suuntoplus/guides/files/{id}` | yes | Content only — does **not** change pinned status or `owner`; the response reflects the owner already stored |
| `guides pin <id>` | `PATCH /v1/suuntoplus/guides/items/{id}` | yes | The only way to set pinned status; moves the guide to the front of the priority order |
| `guides unpin <id>` | `PATCH /v1/suuntoplus/guides/items/{id}` | yes | Clears pinned status |
| `guides priority` | `GET /v1/suuntoplus/guides/priority` | yes | Ordered `{id}` list for the account, most recently pinned first |
| `guides delete <id>` | `DELETE /v1/suuntoplus/guides/files/{id}` | yes | **Destructive.** TTY confirmation prompt; pass `--yes` in scripts/agents, same as `workouts delete` |

> **No `x-totp` on guide writes.** Unlike `comment` and `react`, none of the guide endpoints require a TOTP header.

> **`owner` asymmetry between create and update.** `guides upload`'s response always reports `owner: "Suunto"`, no matter what the manifest sent; `guides update`'s response reflects the owner actually stored. Confirmed live, not a fluke. Likely explanation (unconfirmed — there's no documentation for this private API to check it against): the server stamps `owner` from the authenticated client's identity on create, and every caller here presents as the same first-party app identity, so it's always `"Suunto"`; update only touches content and leaves the already-stored owner alone. A `guides list` after any write is the way to see what the server actually stored versus what was sent.

### MCP server

`suuntool mcp` runs an MCP stdio server. See the dedicated [MCP server](#mcp-server) section above for full config snippets for Claude Desktop, Claude Code, and Codex.

### Discovery / meta

| Command | Endpoint | Auth | Notes |
|---------|----------|------|-------|
| `endpoints` | — | no | Stable command → method/path table for agents (`--format json`) |
| `version` | — | no | Build version |

Run `suuntool <command> --help` for the full reference of any command. The mapping above is also available as a JSON document via `suuntool endpoints --format json`.

> **Raw passthrough.** `workouts sml`, `workouts fit`, `guides download`, and the four `wellness` subcommands stream their response body straight to stdout (or `-o`) without going through the `--format` pretty-printer. The body is already in its final shape (large JSON / binary `.fit` / zip / NDJSON) and reformatting it would waste memory and lose fidelity.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | OK |
| 1 | Generic error |
| 2 | Bad usage (flags/args) |
| 3 | Network (timeout, DNS, connection) |
| 4 | Auth — session missing or expired (run `suuntool login`) |
| 5 | Server error (5xx or malformed response) |
| 6 | Not found |
| 7 | Forbidden |

In `--format json` mode, errors are emitted to stderr as:

```json
{ "error": { "code": "AUTH_EXPIRED", "message": "...", "hint": "Run: suuntool login" } }
```

## Environment

| Variable | Purpose |
|----------|---------|
| `SUUNTOOL_SESSION_FILE` | Override session storage path (default `$XDG_CONFIG_HOME/suuntool/session.json`) |
| `SUUNTOOL_FORMAT` | Default output format |
| `SUUNTOOL_TIMEOUT` | Default HTTP timeout |
| `SUUNTOOL_BASE_URL` | Override API base URL (for testing) |
| `NO_COLOR` | Disable ANSI styling |

## Security & privacy

- Your session key is persisted to `$XDG_CONFIG_HOME/suuntool/session.json` with mode `0600`. Don't put it in your dotfiles repo.
- Passwords are read from a no-echo TTY prompt or stdin. `suuntool` never accepts `--password` on the command line.
- No telemetry, no analytics, no phone-home. The binary talks to Suunto's servers and nowhere else.

## ⚠️ Disclaimers

**This is an unofficial, experimental tool. Use at your own risk.**

- `suuntool` is **not affiliated with, endorsed by, or supported by Suunto Oy, Amer Sports, or Sports-Tracker**. All trademarks belong to their respective owners.
- The Suunto API is **not public**. The wire contract can change without notice. A future app release may break this tool with no warning.
- Using this tool may **violate Suunto's Terms of Service**. Read them before you run anything. Heavy or abusive usage may get your account flagged or banned. The author accepts no responsibility for account actions taken against you.
- Provided **"as is", without warranty of any kind**, express or implied, including but not limited to merchantability, fitness for a particular purpose, and noninfringement. In no event shall the authors or copyright holders be liable for any claim, damages, or other liability arising from your use of the software.
- For **your own data only.** Do not use this tool to scrape, harvest, or aggregate other users' data — you'll get your account banned, and you may be breaking the law.
- The author is **not** a lawyer. Nothing here is legal advice.

If any of the above makes you uncomfortable, use the official Suunto app instead.

## License

MIT.
