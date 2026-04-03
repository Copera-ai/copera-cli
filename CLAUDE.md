# CLAUDE.md — Copera CLI Project

This file gives Claude Code context for working on the `copera` CLI project.

---

## Project Layout

```
copera-cli/                     <- YOU ARE HERE (repo root = CLI project root)
├── .github/workflows/          <- CI (ci.yml) and release (release.yml) pipelines
├── cmd/copera/                 <- CLI entry point
├── commands/                   <- CLI subcommands + helpers.go (shared helpers)
├── internal/
│   ├── api/                    <- Typed API client (client.go, boards.go, docs.go)
│   ├── auth/                   <- Auth token resolution & storage
│   ├── build/                  <- Version info injected via ldflags
│   ├── cache/                  <- Cache layer with Store interface (ADR-005)
│   ├── config/                 <- Multi-level config (ADR-002)
│   ├── exitcodes/              <- Exit code constants
│   ├── output/                 <- Printer + Table (ADR-003)
│   ├── testutil/               <- Test helpers (mock server, config, RunCommand)
│   └── updater/                <- Auto-update: version check + self-replace
├── docs/
│   ├── ARCHITECTURE.md
│   └── decisions/              <- Architecture Decision Records (ADRs)
├── .goreleaser.yml             <- Cross-platform release builds
├── install.sh                  <- curl | bash installer
├── go.mod / go.sum
├── Makefile
├── PLAN.md                     <- Work plan and phase checklist
└── CLAUDE.md                   <- This file
```

---

## The CLI (repo root)

### What it is
A Go CLI named `copera` that wraps the [Copera public API](https://api.copera.ai/public/v1/).
Designed to be used by both **humans** (interactive, list/tree output) and **LLM agents** (JSON output, structured errors, non-interactive mode).

### Tech Stack
- **CLI framework**: `github.com/spf13/cobra`
- **Config**: `github.com/spf13/viper` (multi-level config, see ADR-002)
- **Config format**: TOML (`.copera.toml`, `.copera.local.toml`, profiles)
- **Table output**: `github.com/jedib0t/go-pretty/v6` (used only in `rows list`)
- **TTY detection**: `github.com/mattn/go-isatty`

### Key Patterns to Follow
1. **Output format**: Auto-detect TTY. JSON when piped. Human output when interactive. `--json` always forces JSON. `--output auto|json|table|plain`.
2. **Human output style**: Prefer metadata lists (`ID: ... / Name: ... / Updated: ...`) over tables for most commands. Tables are only for dense tabular data like `rows list`. Tree output uses `tree`-style connectors (`├──`, `└──`) for hierarchical data like `docs tree`.
3. **Errors**: Always emit structured JSON errors to stderr: `{"error":"type","message":"...","suggestion":"...","transient":false}`.
4. **Exit codes**: `0=ok, 1=fail, 2=usage, 3=not found, 4=auth, 5=conflict, 6=rate limit`.
5. **Non-interactive**: Never prompt if `--no-input`, `CI=true`, or stdout is not a TTY for destructive ops.
6. **Config hierarchy**: env var `COPERA_CLI_AUTH_TOKEN` > `--token` flag > `.copera.local.toml` > `.copera.toml` > `~/.copera.toml`.
7. **Profiles**: Each profile (`[profiles.name]`) holds its own `token`, `board_id`, `table_id`, `row_id`, `channel_id`, `doc_id`. Active profile resolution: `--profile` flag > `COPERA_PROFILE` env > `default_profile` config key > `"default"`.
8. **Default IDs**: Read from the active profile in config before requiring flags. Use `resolveID(args, flagValue, configDefault, name)` from `commands/helpers.go`.
9. **Pipe support**: Commands that accept content (`docs update`, `rows create`) must accept stdin when no flag provides the data.
10. **Caching**: Use the `Store` interface (ADR-005). Production uses `DiskStore`, tests use `MemStore`. Access cache through `newDocCache(cli, cfg)` or `newTableCache(cli, cfg)` helpers — never construct caches directly in commands.

### Implemented Commands
```
copera auth login|status|logout
copera boards list|get          (alias: bases)
copera tables list|get
copera rows list|get|create
copera docs tree|search|get|content|update|create|delete
copera drive tree|search|get|download|upload|mkdir
copera cache status|clean
copera update [--version] [--force]
copera version
copera completion bash|zsh|fish
```

### Not Yet Implemented
```
copera send message             (Phase 4 — needs API validation)
copera rows update|delete       (API does not support these endpoints yet)
copera config set|get|list
copera schema <command>
```

---

## API Facts

- Base URL: `https://api.copera.ai/public/v1/`
- Auth: `Authorization: Bearer <token>`
- Token types: PAT (`cp_pat_`) for everything including docs; Integration key (`cp_key_`) for boards/channels only
- All IDs: 24-char hex ObjectId strings
- Rate limits: 50 req/min (board reads), 30 req/min (row create), 100 req/min (chat); docs not published
- Rate limit headers: `x-ratelimit-limit`, `x-ratelimit-remaining`, `x-ratelimit-reset`
- Error format: `{"error": "message", "code": "MACHINE_CODE"}`

### Board endpoints (prefix: `/board/`)
- `GET /board/list-boards` — list all boards (returns array)
- `GET /board/{boardId}` — get board
- `GET /board/{boardId}/tables` — list tables (returns array with columns)
- `GET /board/{boardId}/table/{tableId}` — get table with columns
- `GET /board/{boardId}/table/{tableId}/rows` — list rows
- `GET /board/{boardId}/table/{tableId}/row/{rowId}` — get row
- `POST /board/{boardId}/table/{tableId}/row` — create row
- `POST /board/{boardId}/table/{tableId}/row/{rowId}/comment` — create comment
- `GET /board/{boardId}/table/{tableId}/row/{rowId}/comments` — list comments (paginated)

### Docs endpoints (prefix: `/docs/`)
- `GET /docs/tree?parentId=` — doc tree (children are fully hydrated `DocNode` objects)
- `GET /docs/search?q=&sortBy=&sortOrder=&limit=` — full-text search
- `GET /docs/{id}` — doc metadata
- `GET /docs/{id}/md` — markdown content
- `POST /docs/{id}/md` — update content (async, 202); body: `{operation, content}`
- `POST /docs/` — create doc; body: `{title, parent?, content?}`
- `DELETE /docs/{id}` — soft-delete

### Key API quirks
- Docs tree: children are fully hydrated `DocNode` objects (not stubs). No `--depth` parameter needed.
- Docs search: `createdAt`/`updatedAt` are millisecond epoch strings, not RFC3339. `parents` is `[]SearchParent` (objects with `_id` and `title`), not string IDs.
- Row LINK columns: `value` is an array of foreign row IDs, `linkValue` is an array of display strings (index-matched). The table schema does not reference the target table.

---

## ADR Summary

| ADR | Decision |
|-----|----------|
| ADR-001 | Use Cobra + Viper for the new CLI (not raw `flag`) |
| ADR-002 | Cosmic-config-style 5-level config hierarchy |
| ADR-003 | Auto TTY detection; `--json`/`--output` flags |
| ADR-004 | Token-based auth (no type distinction); env var always wins |
| ADR-005 | File-based caching with Store abstraction; versioned temp dir; custom text format for table schemas |

See `docs/decisions/` for full records.

---

## Working on This Project

### Before starting a new feature
1. Read `PLAN.md` to understand current phase and what's been done.
2. Read the relevant ADR in `docs/decisions/` for the area you're changing.
3. Read existing code in the same package before writing new code.

### Code conventions
- Package names: lowercase, single word.
- Errors returned up the stack; only commands call `os.Exit`.
- All public API responses deserialized into typed structs in `internal/api/`.
- Config access only through `internal/config` package (never call `viper.Get` in commands directly).
- Use `internal/output` for all output — never `fmt.Print` directly in commands.
- Shared command helpers live in `commands/helpers.go`: `requireAPIClient`, `apiError`, `handleConfigErr`, `resolveID`, `newDocCache`, `newTableCache`, `truncate`.

### When adding a new command
1. Add types to `internal/api/` for the new endpoint.
2. Add the command file to `commands/`.
3. Register it in `commands/root.go` via `cmd.AddCommand(newXxxCmd(cli))`.
4. Add `--json` and `--output` flag support via `cli.Printer.IsJSON()` / `cli.Printer.PrintJSON()`.
5. For human output, prefer metadata list format (`cli.Printer.PrintLine(fmt.Sprintf("Label: %s", value))`).
6. Use `requireAPIClient(cli)` to get the API client and config.
7. Use `resolveID(args, flagValue, cfg.XxxID, "description")` for ID resolution.
8. Add a `commands/<name>_test.go` using `testutil.RunCommand`.
9. Update `PLAN.md` to check off the completed item.

### Testing

**Run tests:**
- `make test` — unit tests only (no network, no real tokens)
- `make test-integration` — integration tests (require `COPERA_CLI_AUTH_TOKEN`)

**Test infrastructure (`internal/testutil`):**
- `testutil.WriteTempConfigAt(t, path, tomlContent)` — writes a TOML config to a specific path, auto-cleanup
- `testutil.SetEnv(t, key, value)` — sets env var with auto-restore
- `testutil.NewMockServer(t, routes)` — wraps `httptest.NewServer`, returns URL to point config at
- `testutil.RunCommand(t, args, stdin)` — executes the root Cobra command with a fresh `MemStore` (no disk I/O), returns `Result{Stdout, Stderr, ExitCode}`
- `testutil.RunCommandWithStore(t, args, stdin, store)` — same but with a shared `cache.Store` for tests verifying cache behavior across multiple invocations

**Cache testing pattern:**
- Single-call tests: use `testutil.RunCommand` (each call gets its own `MemStore`)
- Multi-call cache tests: create `store := cache.NewMemStore()` and pass to `testutil.RunCommandWithStore` for both calls

**Rules:**
- Never hit the real API in unit tests — always use `testutil.NewMockServer`
- Never use real tokens in tests — use `"tok_test_fake"`
- Never read/write real files from disk during tests — use `MemStore` for caches, temp dirs for config
- Use `require.NoError` for setup, `assert.*` for behaviour checks
- Table-driven tests for anything with 3+ input variations

### Release

- **CI:** `.github/workflows/ci.yml` — tests + lint on push/PR to main
- **Release:** `.github/workflows/release.yml` — on tag push (`v*`): tests -> goreleaser -> upload to S3 CDN
- **CDN:** `https://cli.copera.ai` (CloudFront -> S3). All downloads go through CDN, not GitHub Releases.
- **Auto-update:** Background version check on every command (cached 24h, suppressed in JSON/CI/quiet). `copera update` for explicit self-update.
- **Install:** `curl -fsSL https://cli.copera.ai/install.sh | bash`

---

## AGENTS.md

There is a separate `AGENTS.md` at the repo root with agent-specific instructions for using the CLI once it's built.
