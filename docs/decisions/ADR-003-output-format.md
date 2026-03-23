# ADR-003: Dual-Mode Output (Human Tables + Machine JSON)

**Date:** 2026-03-23
**Status:** Accepted

---

## Context

The CLI has two distinct user types:
- **Humans** running it interactively — they want colored, aligned tables, spinners, and friendly messages.
- **LLM agents and scripts** running it programmatically — they want parseable JSON, no ANSI codes, structured errors, predictable exit codes.

We need an output strategy that serves both without requiring the user to pass flags every time.

---

## Decision

### Output modes

| Mode | When used | Description |
|------|-----------|-------------|
| `table` | Default when stdout is a TTY | Colored, aligned table with headers |
| `json` | Default when stdout is NOT a TTY | Compact JSON object or array |
| `plain` | Never automatic; `--output plain` only | One value per line, no headers, no color |

### Flags

- `--json` — force JSON output (shorthand for `--output json`); matches `gh` CLI convention
- `--output auto|json|table|plain` — explicit output format
- `--quiet` / `-q` — suppress informational messages; only emit result data
- `--no-color` / `NO_COLOR=1` env var — disable ANSI color codes (follows [no-color.org](https://no-color.org))

### Error output

Errors always go to **stderr** as JSON, regardless of `--output` mode:

```json
{
  "error": "resource_not_found",
  "message": "Board '66abc123def456789012abcd' not found",
  "suggestion": "Run 'copera boards list' to see accessible boards",
  "transient": false
}
```

Human-readable context (e.g., "Error: board not found") is also written to stderr when stdout is a TTY and `--quiet` is not set.

### Informational messages

Progress spinners, confirmation prompts, and status messages go to **stderr** only. This keeps stdout clean for piping.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General / unknown error |
| 2 | Usage error (bad flags, missing required args) |
| 3 | Resource not found (404) |
| 4 | Auth failure / permission denied (401/403) |
| 5 | Conflict — resource already exists (409) |
| 6 | Rate limit exceeded (429) |

---

## Options Considered

### Option A: Always JSON
**Pros:** Consistent for automation.
**Cons:** Terrible human UX. Defeats the purpose of a human-targeted CLI.

### Option B: Human tables only, no JSON
**Cons:** Agents can't use it reliably.

### Option C: Auto-detect TTY + explicit flags (chosen)
**Pros:** Matches `gh`, `stripe`, `fly`, `vercel` CLI behavior. Well-understood pattern. Agents use `--json` explicitly when TTY detection might be wrong. Humans get beautiful tables by default.

---

## Implementation Notes

- TTY detection: `github.com/mattn/go-isatty` for `isatty.IsTerminal(os.Stdout.Fd())`.
- Table renderer: `github.com/jedib0t/go-pretty/v6` — supports markdown, CSV, HTML, and terminal output from the same data.
- `internal/output.Printer` wraps both renderers; commands call `printer.PrintList(items)` or `printer.PrintOne(item)`.
- The `Printer` is constructed in `commands/root.go` with the resolved format and passed to subcommands via Cobra's `PersistentPreRun`.

---

## Consequences

- All commands must define their data as typed structs that implement both a JSON marshaler and a table-row method.
- Commands must never write to stdout directly — always go through `output.Printer`.
- Stderr is always human-readable text (for informational messages) or JSON (for errors).
- The `--json` flag is available on every command globally (persistent flag on root).
