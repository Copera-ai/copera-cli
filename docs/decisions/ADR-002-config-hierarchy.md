# ADR-002: Cosmic-Config–Style Multi-Level Configuration with TOML

**Date:** 2026-03-23
**Status:** Accepted

---

## Context

The CLI needs to be usable in multiple contexts:

1. **Individual developer** — personal API token, personal defaults, no project config
2. **Project team** — shared project-level config (default board ID) committed to source control
3. **Local overrides** — per-developer overrides on top of committed project config (gitignored)
4. **CI/CD / LLM agents** — token injected via environment variable, no file system interaction required

This is the same problem that tools like `npm`, `eslint`, `prettier`, and many Node.js tools solve with [cosmiconfig](https://github.com/cosmiconfig/cosmiconfig). We want the same behavior in Go.

---

## Decision

Implement a 6-level config resolution chain using **TOML** as the config file format, applied in this order (highest priority first):

| Level | Source | Notes |
|-------|--------|-------|
| 1 | `COPERA_CLI_AUTH_TOKEN` env var | Token only; always wins |
| 2 | `--token` CLI flag | Token only; per-invocation |
| 3 | `.copera.local.toml` (CWD) | Full config; should be in `.gitignore` |
| 4 | `.copera.toml` (CWD) | Full config; team-committed |
| 5 | `~/.config/copera/profiles/<name>/config.toml` | User profile config |
| 6 | `~/.copera.toml` | User home fallback (simple setup) |

The active profile name is resolved from `COPERA_PROFILE` env var, then `--profile` flag, defaulting to `"default"`.

---

## Options Considered

### Option A: Single config file
**Pros:** Simple. One place to look.
**Cons:** Can't support project-level defaults without committing tokens. Can't support per-developer overrides. Bad for teams.

### Option B: XDG Base Directory only
**Pros:** Follows Linux standard.
**Cons:** No project-level config. Users can't commit shared defaults. Common pattern in CLIs (gh, stripe, vercel) is to support both project and global config.

### Option C: Cosmic-config–style layered config (chosen)
**Pros:** Matches `gh`, `stripe`, `vercel`, `fly` CLI patterns. Supports all use cases. `.local` variant is a well-understood convention (Next.js, dotenv). Env var override is the clear escape hatch for automation.
**Cons:** Slightly more complex to explain; mitigated by good `auth login` wizard UX.

### YAML vs TOML for config format

TOML was chosen over YAML for the following reasons:

| Concern | YAML | TOML |
|---------|------|------|
| Readability for simple configs | Good | Excellent — table sections map naturally to Go structs |
| Profile sections | Requires nested maps | Native `[profile.name]` table syntax |
| Indentation sensitivity | Yes (source of bugs) | No |
| Comments | Supported | Supported |
| Viper support | Native | Native (`github.com/pelletier/go-toml/v2`, bundled with Viper) |
| Familiarity | Very common | Growing (Rust's Cargo.toml, Hugo, etc.) |

TOML's `[profiles.name]` table syntax makes multi-profile configs natural to read and edit manually:

```toml
[profiles.work]
token      = "tok_work_abc"
board_id   = "66abc123def456789012abcd"
channel_id = "66def456abc123789012efgh"

[profiles.personal]
token    = "tok_personal_xyz"
board_id = "66ghi789jkl012345678mnop"
```

No dependency is added — Viper bundles `go-toml/v2` already.

---

## Profile-Based Config Design

Each **named profile** groups a token and its default resource IDs together. Switching context is a single flag or env var, not editing multiple keys.

### Active profile resolution

1. `COPERA_PROFILE` env var
2. `--profile` CLI flag
3. Falls back to `"default"`

### Profile structure

Each profile lives under `[profiles.<name>]` and can hold:

```toml
[profiles.default]
token      = ""      # NEVER commit a real token here
board_id   = ""
table_id   = ""
row_id     = ""
channel_id = ""
```

Global (non-profile) keys apply to all profiles unless the active profile overrides them:

```toml
[output]
format = "auto"     # auto | json | table | plain
color  = "auto"     # auto | always | never

[cache]
dir = ""            # default: system temp dir
ttl = "1h"

[api]
base_url = "https://api.copera.ai/public/v1"
timeout  = "30s"
```

### Full example (`~/.copera.toml`)

```toml
[profiles.default]
token      = "tok_personal_abc123"
board_id   = "66abc123def456789012abcd"
channel_id = "66def456abc123789012efgh"

[profiles.work]
token      = "tok_work_xyz789"
board_id   = "66ghi789jkl012345678mnop"
table_id   = "66pqr012stu345678901vwxy"
channel_id = "66zab345cde678901234fghi"

[output]
format = "auto"
color  = "auto"

[cache]
ttl = "1h"
```

Usage:
```bash
copera boards list                        # uses [profiles.default]
copera boards list --profile work         # uses [profiles.work]
COPERA_PROFILE=work copera boards list    # same, via env var
```

---

## `auth login` Wizard Behavior

When the user runs `copera auth login`, the wizard:

1. Asks: "Profile name:" (default: `"default"`, lists existing profiles)
2. Asks: "Token:" (masked input)
3. Optionally asks for default board, table, channel IDs
4. Asks: "Where should this be saved?"
   - `~/.copera.toml` — personal (default)
   - `.copera.local.toml` — this project only (git-ignored)
   - `.copera.toml` — this project (committed — use for non-secret defaults only)
5. Validates token against API before saving.
6. If `.copera.local.toml` chosen: offers to add it to `.gitignore`.

---

## Consequences

- `internal/config` package owns all config loading. Commands never call `viper.Get` directly.
- The active profile is resolved once at startup and stored in the `Config` struct — commands read `config.Token`, `config.BoardID`, etc. without knowing which profile is active.
- Tokens in committed files (`.copera.toml`) must be documented as unsafe — only use for project-shared non-secret defaults.
- `config.example.toml` shows multiple profiles as the primary example.
- The `auth login` wizard should offer to add `.copera.local.toml` to `.gitignore` automatically.
- No additional Go dependency needed — Viper bundles `go-toml/v2`.
