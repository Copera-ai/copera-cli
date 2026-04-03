# Copera CLI

`copera` is the official command-line interface for [Copera](https://copera.ai). Manage boards, tables, rows, docs, drive files, and messaging from your terminal — or integrate it into LLM agent pipelines.

## Installation

### macOS / Linux

```bash
curl -fsSL https://cli.copera.ai/install.sh | bash
```

Or with a specific version:

```bash
VERSION=0.1.0 curl -fsSL https://cli.copera.ai/install.sh | bash
```

### Windows

Download the latest `.zip` from [cli.copera.ai](https://cli.copera.ai/version.json), extract `copera.exe`, and add it to your `PATH`:

```powershell
# PowerShell
$version = (Invoke-RestMethod https://cli.copera.ai/version.json).latest
Invoke-WebRequest "https://cli.copera.ai/v$version/copera-$version-windows-amd64.zip" -OutFile copera.zip
Expand-Archive copera.zip -DestinationPath .
Move-Item copera.exe "$env:LOCALAPPDATA\Microsoft\WindowsApps\copera.exe"
```

### From source

```bash
go install github.com/copera/copera-cli/cmd/copera@latest
```

### Update

```bash
copera update
```

---

## Quick Start

```bash
# First time: set up auth
copera auth login

# Or use an environment variable (recommended for CI/CD and agents)
export COPERA_CLI_AUTH_TOKEN="your_api_token"

# List your boards
copera boards list

# Browse your docs
copera docs tree
```

---

## Authentication

The CLI resolves your API token in this order (highest priority first):

| Source | Example |
|--------|---------|
| Environment variable | `COPERA_CLI_AUTH_TOKEN=cp_pat_xxx` |
| `--token` flag | `copera boards list --token cp_pat_xxx` |
| `.copera.local.toml` (current dir, git-ignored) | Project-local token |
| `.copera.toml` (current dir) | Committed project config |
| `~/.copera.toml` | Home directory fallback |

**Token types:**
- **Personal Access Token** (`cp_pat_...`) — required for docs commands, works for everything
- **Integration API Key** (`cp_key_...`) — for boards and channels only

Get a token at [developers.copera.ai/guides/authentication](https://developers.copera.ai/guides/authentication).

---

## Commands

### Auth

```bash
copera auth login              # Interactive auth setup
copera auth status             # Show current token and source
copera auth logout             # Remove stored credential
```

### Boards (alias: `bases`)

```bash
copera boards list             # List all accessible boards
copera boards list --json      # JSON output (for scripting)
copera boards get <id>         # Get board details
copera bases list              # Same as boards list
```

### Tables

```bash
copera tables list                        # List tables (uses default board if set)
copera tables list --board <board-id>     # List tables in specific board
copera tables get <table-id>              # Get table schema and columns
copera tables get <id> --board <board-id>
```

### Rows

```bash
copera rows list                                      # List rows (uses defaults)
copera rows list --board <id> --table <id>            # Explicit IDs
copera rows get <row-id>                              # Get row with resolved column labels
copera rows create --data '{"columns":[...]}'         # Create a row
echo '{"columns":[...]}' | copera rows create        # Create from stdin
```

### Docs

Docs commands require a **Personal Access Token** (`cp_pat_...`).

```bash
copera docs tree                          # Tree view of workspace docs
copera docs tree --parent <id>            # Subtree under a specific doc
copera docs search "keyword"              # Full-text search
copera docs get <id>                      # Get doc metadata
copera docs content <id>                  # Get markdown content (cached)
copera docs content <id> --no-cache       # Bypass cache
copera docs create --title "New Doc"      # Create a doc
copera docs update <id> --content "..."   # Update content (replace by default)
copera docs update <id> --operation append  # Append content
cat content.md | copera docs update <id>  # Update from stdin
copera docs delete <id> --force           # Delete a doc
```

### Drive

Drive commands require a **Personal Access Token** (`cp_pat_...`) with the `ACCESS_DRIVE` scope.

```bash
copera drive tree                             # Tree view of drive files and folders
copera drive tree --parent <id>               # Subtree under a folder
copera drive tree --depth 5                   # Control nesting depth (1-10)
copera drive search "quarterly report"        # Full-text search
copera drive get <file-id>                    # Get file/folder metadata
copera drive download <file-id>               # Download a file
copera drive download <file-id> -o report.pdf # Download to specific path
copera drive upload ./report.pdf              # Upload a single file
copera drive upload ./project/ --parent <id>  # Upload directory recursively
copera drive mkdir "New Folder"               # Create a folder
copera drive mkdir "Sub" --parent <id>        # Create nested folder
```

**Upload features:**
- Multipart chunked upload via S3 presigned URLs (handles large files)
- Concurrent part uploads (`--concurrency`, default 4)
- Configurable chunk size (`--chunk-size`, default 10 MB)
- curl/wget-style progress bar when running in a TTY
- Recursive directory upload with automatic folder creation

### Channels

```bash
copera channels message send <channel-id> --body "Hello"  # Send a message
```

### Cache

```bash
copera cache status            # Show cache location and size
copera cache clean             # Remove all cached data
```

### Update

```bash
copera update                  # Update to the latest version
copera update --version 1.2.0  # Pin to a specific version
copera update --force          # Skip confirmation
```

### Utilities

```bash
copera version                 # Show version
copera version --json          # Version as JSON
copera completion bash         # Shell completion for bash
copera completion zsh          # Shell completion for zsh
copera completion fish         # Shell completion for fish
```

---

## Configuration

Profiles group a token and default resource IDs together. Create `~/.copera.toml`:

```toml
# Set the default profile (avoids --profile flag)
default_profile = "work"

[profiles.default]
token      = "cp_pat_abc123..."
board_id   = "66abc123def456789012abcd"

[profiles.work]
token      = "cp_key_xyz789..."
board_id   = "66ghi789jkl012345678mnop"
table_id   = "66pqr012stu345678901vwxy"

# Global settings (apply to all profiles)
[output]
format = "auto"   # auto | json | table | plain

[cache]
ttl = "1h"
```

Switch profiles with `--profile` or `COPERA_PROFILE`:

```bash
copera boards list                        # uses default_profile
copera boards list --profile work         # uses [profiles.work]
COPERA_PROFILE=work copera boards list    # same, via env var
```

For project-level shared defaults (no tokens — safe to commit):

```toml
# .copera.toml
[profiles.default]
board_id   = "66abc123def456789012abcd"
```

For project-local token override (add to `.gitignore`):

```toml
# .copera.local.toml
[profiles.default]
token = "your_personal_token_here"
```

---

## Machine-Readable Output

The CLI is designed to work with LLM agents and scripts:

- **Auto-JSON when piped:** When stdout is not a TTY, output defaults to JSON.
- **`--json` flag:** Forces JSON output regardless of TTY.
- **`--output` flag:** `auto | json | table | plain`
- **`--quiet` / `-q`:** Suppress informational messages; only emit result data.
- **Structured errors on stderr:**
  ```json
  {"error":"resource_not_found","message":"Board 'abc123' not found","suggestion":"Run 'copera boards list' to see accessible boards","transient":false}
  ```
- **Exit codes:** `0=ok, 1=error, 2=usage, 3=not found, 4=auth, 5=conflict, 6=rate limit`
- **Stdin support:** Pipe content directly for `rows create` and `docs update`.
- **No interactive prompts** when `--no-input` is set or `CI=true`.

---

## Environment Variables

| Variable | Description |
|----------|-------------|
| `COPERA_CLI_AUTH_TOKEN` | API token (overrides all config) |
| `COPERA_PROFILE` | Active config profile name (default: `"default"`) |
| `COPERA_NO_UPDATE_CHECK` | Set to `1` to disable background version checks |
| `COPERA_SANDBOX` | Set to `1` to use the dev API (`api-dev.copera.ai`) |
| `CI` | When set to `true`, disables interactive prompts and update checks |
| `NO_COLOR` | Disable ANSI color output |

---

## Shell Completion

```bash
# Bash
copera completion bash >> ~/.bashrc

# Zsh
copera completion zsh >> ~/.zshrc

# Fish
copera completion fish > ~/.config/fish/completions/copera.fish
```

---

## Related

- [Copera Developer Docs](https://developers.copera.ai/)
- [AGENTS.md](./AGENTS.md) — Instructions for LLM agents using this CLI
