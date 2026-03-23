# ADR-001: Use Cobra + Viper as CLI Framework

**Date:** 2026-03-23
**Status:** Accepted

---

## Context

We are building a new CLI (`copera`). The new CLI needs:

- ~20+ subcommands organized in a noun-verb hierarchy
- Consistent help text and `--help` for every command and subcommand
- Shell completion (bash, zsh, fish) for both humans and agent tooling
- Global flags (`--json`, `--output`, `--token`, `--profile`, `--quiet`) inherited by all subcommands
- Configuration from multiple sources (files, env vars, flags)

---

## Options Considered

### Option A: Raw `flag` package (like metrics-agent)

**Pros:** Zero dependencies, simple, what the existing codebase uses.
**Cons:** No native subcommand support; implementing noun-verb hierarchy manually would require hundreds of lines of routing code; no shell completion generation; global flag inheritance requires manual wiring.

### Option B: Cobra (CLI) + Viper (config)

**Pros:** De-facto standard for Go CLIs. Used by Docker, Kubernetes (`kubectl`), GitHub CLI (`gh`), Hugo, and most major Go CLIs. Native subcommand tree, automatic `--help`, shell completion generation, persistent flags, Viper integration for multi-source config.
**Cons:** External dependency (~2 packages). Slightly more boilerplate per command.

### Option C: `urfave/cli`

**Pros:** Simpler API than Cobra for small CLIs.
**Cons:** Less adoption in the Go ecosystem for complex CLIs. No built-in Viper integration. Cobra has significantly more tooling around it.

---

## Decision

Use **Cobra** for the CLI framework and **Viper** for configuration management.

This matches the tooling used by every major Go CLI the team would reference, maximizes familiarity, and provides complete features for the planned command set.

---

## Consequences

- Commands live in `commands/` package; each file registers subcommands via `init()` or explicit registration in `root.go`.
- Global flags are defined on the root command as persistent flags.
- Viper is initialized once in `commands/root.go` and config values are accessed via `internal/config.Config` (Viper is never called directly in command files — always go through the typed config struct).
- Shell completion is available via `copera completion bash|zsh|fish|powershell`.

