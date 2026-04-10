# AGENTS.md — Copera CLI for LLM Agents

This file provides instructions for LLM agents (Claude, GPT, Gemini, Codex, etc.) that use the `copera` CLI as a tool. It follows the [AGENTS.md open format](https://github.com/agentsmd/agents.md).

---

## Setup (run once per session)

```bash
# Option A (recommended for agents): set API token via environment variable
export COPERA_CLI_AUTH_TOKEN="your_token"

# Option B (also works for agents): save via --token flag directly
copera auth login --token=your_token   # no browser, no prompts

# Recommended: disable interactive prompts
export CI=true

# Verify setup works
copera auth status --json
```

**Never run bare `copera auth login`** — by default that command opens
the system browser and waits for a pasted token, which agents cannot do.
Agents should always use the env var (Option A) or `--token=<value>`
(Option B). Running `auth login` in non-interactive mode without a
`--token=<value>` flag exits with an error.

If `auth status` returns exit code 4, the token is missing or invalid.

**Token types:**
- `cp_pat_...` — Personal Access Token. Required for docs commands, works for everything.
- `cp_key_...` — Integration API Key. For boards and channels only.

---

## Output Format

**Always use `--json` for programmatic use:**

```bash
copera boards list --json
```

When `CI=true` or stdout is not a TTY, JSON is the default. Use `--json` explicitly to be certain.

**Errors are on stderr as JSON:**
```json
{"error":"resource_not_found","message":"Board 'abc123' not found","suggestion":"Run 'copera boards list' to see accessible boards","transient":false}
```

---

## Exit Codes

Check exit codes to decide what to do next:

| Code | Meaning | Action |
|------|---------|--------|
| `0` | Success | Continue |
| `1` | General error | Read stderr for details |
| `2` | Usage error | Fix command arguments |
| `3` | Not found | Resource doesn't exist; verify ID |
| `4` | Auth failure | Check `COPERA_CLI_AUTH_TOKEN` |
| `5` | Conflict | Resource already exists; skip or handle |
| `6` | Rate limited | Wait and retry |

---

## Resource Discovery Workflow

When you don't know IDs, discover them:

```bash
# 1. List boards
copera boards list --json
# -> [{"_id": "...", "name": "...", "description": "..."}, ...]

# 2. List tables in a board
copera tables list --board <board-id> --json
# -> [{"_id": "...", "name": "...", "columns": [...]}]
# Note: columns include their IDs, labels, and types — use these for row creation

# 3. Get full column definitions (including options for STATUS/DROPDOWN/LABELS)
copera tables get <table-id> --board <board-id> --json

# 4. List rows
copera rows list --board <board-id> --table <table-id> --json
```

---

## Creating Rows

Column IDs come from the table schema (step 2/3 above).

```bash
copera rows create \
  --board <board-id> \
  --table <table-id> \
  --json \
  --data '{"columns":[{"columnId":"<col-id>","value":"<value>"}]}'
```

Or from stdin:
```bash
echo '{"columns":[{"columnId":"abc123","value":"Hello"}]}' | copera rows create \
  --board <board-id> --table <table-id> --json
```

Note: unsupported column types are silently ignored by the API — the row is created without those values.

---

## Docs Discovery

Docs have no list-all endpoint. The two discovery tools are `tree` and `search`.

### When to use search vs tree

**Use `docs search` when you know what you're looking for** — it's fast, full-text, and returns ranked results with highlights:

```bash
# Find docs by topic
copera docs search "deployment runbook" --json
copera docs search "onboarding" --limit 10 --json

# Sort by most recently updated
copera docs search "api" --sort-by updatedAt --sort-order desc --json
```

The response includes a `highlight` field showing which part of the doc matched — use this to confirm relevance before fetching full content.

**Use `docs tree` when you need to understand structure** — to see what exists, how docs are organized, or to find a doc by its location in the hierarchy:

```bash
# See the workspace root docs with their children
copera docs tree --json

# Drill into a subtree
copera docs tree --parent <doc-id> --json
```

The tree response includes fully hydrated children. Each node shows its title, ID, and nested children.

### Recommended discovery workflow

```bash
# Step 1: search first if you have a keyword
copera docs search "topic" --json

# Step 2: if search doesn't help, browse the tree structure
copera docs tree --json

# Step 3: once you have an ID, get metadata
copera docs get <doc-id> --json

# Step 4: fetch full content only when needed (it may be large)
copera docs content <doc-id>
```

### Docs require a PAT token

Board and channel commands work with integration keys (`cp_key_...`). Docs commands require a **Personal Access Token** (`cp_pat_...`), created in Workspace Settings -> Integrations -> Personal Tokens.

If using both token types, store them in separate profiles:

```toml
# ~/.copera.toml
default_profile = "docs"

[profiles.boards]
token = "cp_key_..."
board_id = "66abc123..."

[profiles.docs]
token = "cp_pat_..."
```

```bash
copera boards list --profile boards --json
copera docs tree --profile docs --json
```

---

## Using Default IDs (avoid repeating flags)

Set defaults in your profile to skip `--board`, `--table` flags:

```toml
# ~/.copera.toml
[profiles.default]
token    = "cp_key_..."
board_id = "66abc123def456789012abcd"
table_id = "66pqr012stu345678901vwxy"
```

```bash
# Now these work without --board, --table flags:
copera rows list --json
copera tables list --json
```

Set `default_profile` to avoid `--profile` when you have multiple profiles:

```toml
default_profile = "work"
```

---

## Rate Limits

| Operation | Limit |
|-----------|-------|
| Board reads | 50 req/min |
| Row creation | 30 req/min |
| Send message | 100 req/min |
| Docs | not published |

The CLI retries automatically on 429 (up to 2 times with exponential backoff). If it still fails, exit code 6 is returned.

---

## All IDs Are 24-Character Hex Strings

```
66abc123def456789012abcd   <- valid ObjectId
abc123                     <- invalid, returns exit code 2 (usage error)
```

---

## Quick Reference

```bash
# What boards exist?
copera boards list --json

# What tables are in a board?
copera tables list --board $BOARD_ID --json

# What columns does a table have?
copera tables get $TABLE_ID --board $BOARD_ID --json

# Get a specific row with resolved column labels
copera rows get $ROW_ID --board $BOARD_ID --table $TABLE_ID --json

# Search docs
copera docs search "keyword" --json

# Get doc content
copera docs content $DOC_ID

# Check auth
copera auth status --json

# Check version
copera version --json
```

---

## Hard Limits

- Never commit `.copera.local.toml` — it may contain tokens.
- Never store tokens in `--data` column values.
- Never run more than 30 `rows create` calls per minute.
- Docs `update` is async — the server returns 202 immediately, content processes in the background.

---

## Introspection

```bash
# Get full help for any command
copera <command> --help

# Get version info
copera version --json
```
