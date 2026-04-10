# ADR-006: Browser-Assisted PAT Creation for `auth login`

**Date:** 2026-04-09
**Status:** Accepted
**Supersedes:** Parts of ADR-004 (specifically the "Option A: OAuth + browser flow" rejection and the token-validation step of `auth login`)

---

## Context

ADR-004 settled on a token-paste login flow (`copera auth login` prompts for
a masked PAT, stores it) and explicitly rejected a browser-based OAuth
flow because "LLM agents cannot complete browser flows" and the Copera API
didn't support OAuth for third-party CLIs.

Eighteen months later the ergonomics of that decision are showing:

1. First-time human users have to leave the terminal, find the PAT
   settings page (nested under Workspace Settings → User → Personal
   Tokens), click `+ Create Token`, fill in fields, copy the token, and
   paste it back — with no in-CLI guidance.
2. PATs are workspace-scoped, so the user has to remember which workspace
   URL to visit BEFORE creating the token — a pitfall the CLI cannot
   help with, since it has no workspace state at login time.
3. The LLM-agent concern that justified rejecting a browser flow is real,
   but it only applies to agents — humans running the CLI interactively
   are the majority, and a browser flow is significantly better UX for
   them.

The web team is building a new top-level route at `/oauth/cli` (outside
the `:workspaceSlug` prefix) that walks the user through workspace
selection → PAT form → token display. The CLI should open this page by
default, while keeping escape hatches for agents and headless
environments.

---

## Decision

`copera auth login` gains three sub-flows, selected by the `--token` flag:

### 1. Browser mode (default)

`copera auth login`

- Prints the URL (`<web_url>/oauth/cli`) to stdout.
- Attempts to launch the URL in the default browser via stdlib
  `os/exec` (`open`/`xdg-open`/`rundll32`), using `cmd.Start()` so the
  CLI does not block.
- The URL is printed **before** the browser launch, so WSL, SSH, and
  headless environments that silently fail to open a browser still give
  the user something to copy.
- Falls through to the normal profile-name → masked-paste → save-location
  prompts used in ADR-004.

### 2. Direct mode — `--token=<value>`

`copera auth login --token=<value>`

- Saves `<value>` directly with no browser launch and no paste prompt.
- Still asks for profile name and save location in interactive
  terminals; uses `default` profile + `~/.copera.toml` in non-interactive
  mode.
- Intended for scripts, CI, LLM agents, and anyone who already has a
  token and wants to skip the UI entirely.

### 3. Paste-only mode — `--token` (no value)

`copera auth login --token`

- Skips the browser but goes through the masked paste prompt.
- Cobra `pflag` implements this via `NoOptDefVal` — the flag returns a
  sentinel (`__paste_mode__`) when invoked without a value, which the
  RunE maps to the paste-only branch.
- Useful in WSL/SSH sessions where the user already has a token from
  another device and doesn't want us to try launching `xdg-open`.

### Non-interactive fallback (unchanged from ADR-004)

`COPERA_CLI_AUTH_TOKEN=<token>` is still the blessed way to set a token
without ever running `auth login` — agents should prefer this.

### Token validation at login time (DEFERRED)

ADR-004 called for validating the token with `GET /board/list-boards`
after paste. That validation was never implemented, and adding it now
has a subtle footgun: PATs can be scoped to a subset of permissions
(`access_docs` only, `access_drive` only, etc.), and a
`BoardList` call would return 403 Forbidden for any token lacking
`access_boards`. We'd have to distinguish 401 (invalid token) from 403
(valid token, wrong scope) and only treat 401 as failure.

This is worth doing eventually, but out of scope for this ADR. The
current code still falls through to `WriteProfile` without validation,
matching ADR-004's as-shipped behavior.

**Follow-up:** add a dedicated `/auth/validate` or `/me` endpoint to
`api.copera.ai/public/v1` that works for any PAT regardless of scope,
then wire token validation into all three sub-flows.

---

## Options Considered

### Option A: OAuth loopback (localhost callback)

Spin up a local HTTP server on a random port, embed its URL as a `?redirect`
parameter, have the web app POST the generated token back to the loopback.
**Pros:** Fully automated — no copy-paste. Standard for IDE tooling.
**Cons:** Doesn't work over SSH without port forwarding. Adds a
non-trivial `net/http.Server` to the CLI. Race conditions around the
loopback port in shared environments. Complicates LLM-agent use cases
(agents can't navigate browser flows). User explicitly requested the
simpler copy-paste UX.

### Option B: Browser + copy-paste (chosen)

Open the browser, print the URL as a fallback, user copies token back.
**Pros:** Works over SSH, WSL, headless, and with screen readers. No new
network infrastructure in the CLI. Matches `gh auth login` in its
token-paste mode. Keeps the escape hatches for agents.
**Cons:** One manual copy-paste. Acceptable.

### Option C: New separate command (`copera auth login-web`)

Leave `auth login` as-is, add a new `login-web` subcommand for the browser
flow.
**Pros:** Fully additive, zero risk of regressing existing scripts.
**Cons:** Users who run `auth login` for the first time still hit the
bad UX. Two commands for the same intent is confusing.

### Option D: Keep `auth login` paste-only, document the web URL

No CLI changes. Update README to point users at the web URL.
**Pros:** Zero code changes.
**Cons:** Does not fix the problem — the issue is that a human running
`copera auth login` for the first time has no idea the URL exists.

---

## Security Considerations

- Browser launch is best-effort and never exits the CLI with an error —
  a failure would print the URL anyway, so the CLI's flow is unchanged.
- The token is still entered via `term.ReadPassword` in browser and
  paste-only modes — masked input, not echoed.
- `--token=<value>` exposes the token in shell history. Documented in
  README and AGENTS.md; users who care should prefer the env var.
- The sentinel value `__paste_mode__` is impossible to collide with a
  real PAT because real tokens use the `cp_pat_` or `cp_key_` prefix.
- No telemetry, no callback to a third-party service, no loopback port.

---

## Consequences

- `internal/auth/openbrowser.go` is a new ~25-line file with stdlib-only
  `exec.Command` dispatch per GOOS. No new dependencies.
- `internal/config/config.go` gains a `WebURL` field on `APIConfig` with
  sandbox swap logic mirroring the existing `BaseURL` pattern.
- `commands/auth.go` splits `runAuthLogin` into three private functions:
  `runAuthLoginBrowser`, `runAuthLoginPasteOnly`, `runAuthLoginDirect`.
- A local `--token` flag on the `auth login` command shadows the root's
  persistent `--token` flag — this is safe per cobra's
  `mergePersistentFlags` semantics (local wins; the root binding is
  simply bypassed for this command).
- `COPERA_CLI_AUTH_TOKEN` env var continues to work unchanged in all
  three modes.
- LLM agents should prefer `--token=<value>` or the env var; the bare
  `copera auth login` command explicitly errors out in non-interactive
  terminals (`cli.IsNonInteractive()`), pointing users at the escape
  hatches.
- ADR-004 remains authoritative for token storage, resolution order,
  `auth status`, and `auth logout` — this ADR only changes the login
  UX.
