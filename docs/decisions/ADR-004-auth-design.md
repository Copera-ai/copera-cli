# ADR-004: Authentication Design

**Date:** 2026-03-23
**Status:** Accepted

---

## Context

The Copera API uses API keys (created in Workspace Settings → Integrations). All keys use `Authorization: Bearer <token>` — the API makes no distinction between token types. The CLI needs to:
- Allow secure storage at multiple config levels
- Work in headless/automated environments without interactive prompts
- Provide a guided first-time setup experience

---

## Decision

### Token resolution order (highest priority wins)

1. `COPERA_CLI_AUTH_TOKEN` environment variable
2. `--token` CLI flag (per-invocation, not stored)
3. `auth.token` from `.copera.local.toml` (CWD)
4. `auth.token` from `.copera.toml` (CWD)
5. `auth.token` from active profile config (`~/.config/copera/profiles/<name>/config.toml`)
6. `auth.token` from `~/.copera.toml`

### First-run behavior

If no token is found at any level:
1. If stdout is NOT a TTY (agent/CI mode): emit structured error and exit 4.
   ```json
   {
     "error": "auth_required",
     "message": "No API token found",
     "suggestion": "Set COPERA_CLI_AUTH_TOKEN env var or run 'copera auth login'",
     "transient": false
   }
   ```
2. If stdout IS a TTY: offer to run `auth login` interactively.

### `auth login` command

Interactive wizard that:
1. Prompts for token (masked input)
3. Validates token with a lightweight API call (`GET /board/list-boards`)
3. Prompts for config storage location (personal profile, `.copera.local.toml`, `.copera.toml`)
4. Writes the token to chosen location
5. If `.copera.local.toml` chosen: offers to add it to `.gitignore`

### `auth status` command

Shows:
- Current token source (which level it was resolved from)
- Token (masked: `cop_***...***abc`)
- Validation status (calls API to confirm token is valid)

### `auth logout` command

- Prompts which stored credential to remove (if multiple exist)
- Removes `auth.token` from chosen config file
- Does NOT remove other config values

---

## Options Considered

### Option A: OAuth + browser flow
**Pros:** Standard for user-facing apps. Secure token exchange.
**Cons:** LLM agents cannot complete browser flows. Copera API doesn't currently support OAuth for third-party CLI apps. Adds significant complexity.

### Option B: Token only (no wizard)
**Pros:** Simple. User sets `COPERA_CLI_AUTH_TOKEN` manually.
**Cons:** Poor first-time UX for humans. Easy to misconfigure.

### Option C: Token with interactive wizard + env var override (chosen)
**Pros:** Best of both worlds. Simple for automation (env var). Guided for humans (wizard). Matches how `gh auth login`, `stripe login`, and `vercel login` work for token-based auth.

---

## Security Considerations

- Tokens are never logged or emitted in structured output (masked as `tok_***...***xyz`).
- The `--token` flag value is not stored anywhere — it's per-invocation only.
- Config files containing tokens should have restrictive permissions (0600). The `auth login` wizard sets this automatically.
- `.copera.local.toml` is the recommended storage location for project-scoped tokens; it should never be committed.
- `COPERA_CLI_AUTH_TOKEN` is the recommended approach for CI/CD and agent use.

---

## Consequences

- `internal/auth.Resolve()` implements the full resolution chain.
- The auth package is the single source of truth for the token — commands never read config directly for auth.
- Future OAuth support can be added without breaking existing token-based auth.
