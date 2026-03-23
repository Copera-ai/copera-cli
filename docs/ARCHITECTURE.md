# Copera CLI — Architecture Overview

> Version: 0.1 (draft)
> Date: 2026-03-23

---

## Purpose

`copera` is a command-line interface for the [Copera](https://copera.ai) platform. It wraps the [public REST API](https://developers.copera.ai/) to allow users and automated agents to manage boards, tables, rows, docs, and messaging from a terminal.

---

## Design Goals

| Goal | Description |
|------|-------------|
| **Human-friendly** | Colored tables, helpful prompts, clear error messages |
| **Agent-friendly** | JSON output, structured errors, exit codes, non-interactive mode, pipe support |
| **Simple config** | Config lives at multiple levels; overridable per-project and per-profile |
| **Composable** | Plays well with `jq`, `xargs`, shell scripts, and LLM tool-use pipelines |
| **Extensible** | New API endpoints slot in without restructuring |

---

## System Context

```
┌─────────────────────────────────────────────────────┐
│                   User / LLM Agent                   │
└──────────────────────┬──────────────────────────────┘
                       │ stdin / args / flags
                       ▼
┌─────────────────────────────────────────────────────┐
│                   copera CLI                         │
│  ┌──────────┐  ┌──────────┐  ┌────────────────────┐ │
│  │ commands │→ │  config  │→ │    api client      │ │
│  └──────────┘  └──────────┘  └────────┬───────────┘ │
│  ┌──────────┐  ┌──────────┐           │             │
│  │  output  │  │  cache   │           │             │
│  └──────────┘  └──────────┘           │             │
└───────────────────────────────────────┼─────────────┘
                                        ▼
                         ┌──────────────────────────┐
                         │  Copera REST API          │
                         │  api.copera.ai/public/v1  │
                         └──────────────────────────┘
```

---

## Internal Package Map

### `internal/api`
Typed HTTP client for the Copera API. Each resource group has its own file.

```
client.go     — base HTTP client, auth header injection, retry, rate-limit handling
boards.go     — ListBoards, GetBoard, ListTables, GetTable, ListRows, GetRow, CreateRow
chat.go       — SendMessage
docs.go       — stub (API not yet public)
types.go      — shared response types (Board, Table, Row, Column, Message…)
errors.go     — typed API error + HTTP status mapping to CLI exit codes
```

Responsibilities:
- Inject `Authorization: Bearer <token>` on every request.
- Parse `x-ratelimit-*` headers; surface rate-limit info to caller.
- Retry on 429 with `Retry-After` delay (max 3 attempts).
- Deserialize API errors into `APIError{Code, Message}` struct.
- Map HTTP status codes to CLI exit codes (see ADR-003).

### `internal/config`
Multi-level configuration loader built on Viper.

```
config.go     — Config struct, Load(), Defaults(), Validate()
levels.go     — file path resolution for each config level
profile.go    — profile management (list, create, select)
```

Config file resolution order (highest wins):
1. `COPERA_CLI_AUTH_TOKEN` env var (token only)
2. `--token` / `--profile` CLI flags
3. `.copera.local.toml` (CWD)
4. `.copera.toml` (CWD)
5. `~/.copera.toml`

Within each file, the active profile (`[profiles.<name>]`) is selected via `COPERA_PROFILE` env var or `--profile` flag (default: `"default"`). Each profile holds its own `token`, `board_id`, `table_id`, `row_id`, `channel_id`.

Full schema documented in `config.example.toml`.

### `internal/auth`
Auth token resolution and secure storage.

```
auth.go       — Resolve() returns token from highest-priority source
store.go      — read/write token to config file at chosen level
types.go      — auth types and token resolution result
```

### `internal/output`
Output formatting and TTY detection.

```
output.go     — Printer struct; TTY detection; route to formatter
json.go       — JSON / NDJSON renderer
table.go      — table renderer (go-pretty)
plain.go      — plain text (one value per line, for scripts)
errors.go     — structured JSON error emitter to stderr
```

Rule: commands never call `fmt.Print` directly — they call `output.Printer` methods.

### `internal/cache`
Disk cache for docs content (and future heavy GET responses).

```
cache.go      — Get/Set/Invalidate keyed by resource ID + hash
```

Default location: `$TMPDIR/copera-cli/` (configurable via `cache.dir`).
TTL configurable via `cache.ttl` (default: 1 hour).

---

## Command Layer (`commands/`)

Each file maps to a command group and follows this structure:
1. Define `cobra.Command` with `Use`, `Short`, `Long`, and `Example` fields.
2. Bind flags; validate required args.
3. Resolve config/defaults (board ID, channel ID, etc.).
4. Call `internal/api` function.
5. Pass result to `internal/output.Printer`.

```
root.go       — root command, global flags (--token, --profile, --output, --json, --quiet, --no-input)
auth.go       — auth login|status|logout
docs.go       — docs tree|list|search|get|update
send.go       — send message
boards.go     — boards list|get  (alias: bases)
tables.go     — tables list|get
rows.go       — rows list|get|create|update|delete
config.go     — config set|get|list
version.go    — version (--json)
schema.go     — schema <command>
```

---

## Auth Flow

```
User runs: copera boards list

1. Check COPERA_CLI_AUTH_TOKEN env var
   → found: use it
   → not found: continue

2. Check --token flag
   → found: use it
   → not found: continue

3. Load config (multi-level)
   → found auth.token: use it
   → not found: emit error

   {
     "error": "auth_required",
     "message": "No API token found",
     "suggestion": "Run 'copera auth login' or set COPERA_CLI_AUTH_TOKEN",
     "transient": false
   }
   exit 4
```

---

## Output Format Decision Tree

```
Is --json flag set?
  YES → emit JSON to stdout
  NO  →
    Is --output <format> set?
      YES → use that format
      NO  →
        Is stdout a TTY?
          YES → emit human table with color
          NO  → emit JSON (agent-safe default)
```

---

## Config Hierarchy Diagram

```
Priority (high → low)
─────────────────────────────────────────────────────
  COPERA_CLI_AUTH_TOKEN     ← env var (token only)
  --token flag              ← per-invocation override
  .copera.local.toml        ← project-local secret (git-ignored)
  .copera.toml              ← project config (safe to commit, no tokens)
  ~/.copera.toml            ← personal config with all profiles
─────────────────────────────────────────────────────
Active profile: COPERA_PROFILE env var or --profile flag (default: "default")
Each profile holds: token, board_id, table_id, row_id, channel_id
─────────────────────────────────────────────────────
```

---

## Error Strategy

All errors emitted to **stderr** as JSON:

```json
{
  "error": "resource_not_found",
  "message": "Board '66abc123...' not found or not accessible",
  "suggestion": "Run 'copera boards list' to see accessible boards",
  "transient": false,
  "code": 3
}
```

Exit codes:
| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General / unknown error |
| 2 | Usage error (bad flags, missing args) |
| 3 | Resource not found |
| 4 | Auth failure / permission denied |
| 5 | Conflict (resource exists) |
| 6 | Rate limit exceeded |

---

## Pipe & Stdin Support

Commands that accept content accept stdin via `-` as argument:

```bash
# Update doc from file
copera docs update <id> --content-file content.md

# Update doc from pipe
cat content.md | copera docs update <id>

# Send message from pipe
echo "Deploy complete" | copera send message --channel <id>

# Combine with jq
copera boards list --json | jq '.[].name'
```

---

## Caching

Docs content is cached to avoid redundant API calls:
- Cache key: `<resource-type>/<id>/<etag-or-hash>`
- Default TTL: 1 hour
- Location: `$TMPDIR/copera-cli/` or `cache.dir` from config
- `--no-cache` flag bypasses read; `--refresh-cache` forces refresh
- Cache is invalidated on successful update

---

## Future Extensibility

New API resources slot in with minimal changes:
1. Add types to `internal/api/types.go`
2. Add API calls to `internal/api/<resource>.go`
3. Add commands to `commands/<resource>.go`
4. Register in `commands/root.go`

The output, config, auth, and cache layers are shared infrastructure — new commands get them for free.
