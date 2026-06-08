# Copera CLI

`copera` is the official command-line interface for [Copera](https://copera.ai).
Use it to work with boards, tables, rows, docs, drive files, channels, and
workspace data from your terminal or from scripts.

## Install

### macOS / Linux

Run this in your terminal:

```bash
curl -fsSL https://cli.copera.ai/install.sh | bash
```

To install a specific version:

```bash
VERSION=0.1.0 curl -fsSL https://cli.copera.ai/install.sh | bash
```

By default, the installer writes to `/usr/local/bin/copera`. If that directory
requires elevated permissions, the script will ask for `sudo`.

### Windows

Open **PowerShell** and run this command. You do not need to run PowerShell as
Administrator; this installs `copera.exe` for your current Windows user.

```powershell
irm https://cli.copera.ai/install.ps1 | iex
```

Close and reopen PowerShell, then verify the install:

```powershell
copera version
```

To install a specific version:

```powershell
$env:VERSION = "0.1.0"; irm https://cli.copera.ai/install.ps1 | iex
```

### Manual Windows Install

If you prefer to download the archive yourself:

1. Download the Windows AMD64 zip for the version you want from the Copera CLI CDN.
2. Extract `copera.exe`.
3. Move it to a directory on your `PATH`, such as `%LOCALAPPDATA%\Microsoft\WindowsApps`.
4. Open a new PowerShell window and run `copera version`.

### From Source

```bash
go install github.com/copera/copera-cli/cmd/copera@latest
```

## Update

```bash
copera update
```

Use `copera update --version 1.2.0` to install a specific version, or
`copera update --force` to skip the confirmation prompt.

## First Setup

For most people, the easiest authentication flow is:

```bash
copera auth login
```

The CLI prints a Copera URL, opens your browser when possible, then asks you to
paste the generated token back into the terminal. The printed URL is always
available, so the same flow works from WSL, SSH, and other terminals where the
browser may not open automatically.

If you already have a token:

```bash
copera auth login --token=cp_pat_xxx
```

For CI, scripts, and LLM agents, prefer an environment variable:

```bash
export COPERA_CLI_AUTH_TOKEN="cp_pat_xxx"
```

On Windows PowerShell:

```powershell
$env:COPERA_CLI_AUTH_TOKEN = "cp_pat_xxx"
```

## Try It

```bash
copera auth status
copera boards list
copera docs tree
copera search "onboarding"
```

Use `--json` whenever you want machine-readable output:

```bash
copera boards list --json
```

## Authentication Details

### Token Types

- Personal Access Token (`cp_pat_...`): required for docs, drive,
  notifications, and workspace-user commands; works for all supported commands.
- Integration API Key (`cp_key_...`): works for boards and channels only.

Docs and drive operations require a Personal Access Token with the appropriate
workspace permissions.

### Login Modes

| Command | Use it when |
|---|---|
| `copera auth login` | You want the guided browser flow. |
| `copera auth login --token=<value>` | You already have a token and want to save it directly. |
| `copera auth login --token` | You already have a token and want a masked paste prompt without opening a browser. |

### Token Resolution Order

The CLI loads credentials in this order:

| Priority | Source | Example |
|---|---|---|
| 1 | Environment variable | `COPERA_CLI_AUTH_TOKEN=cp_pat_xxx` |
| 2 | `--token` flag | `copera boards list --token cp_pat_xxx` |
| 3 | `.copera.local.toml` in the current directory | Project-local, git-ignored token |
| 4 | `.copera.toml` in the current directory | Shared project defaults |
| 5 | `~/.copera.toml` | User-level fallback |

More details are available in the
[Copera authentication guide](https://developers.copera.ai/guides/authentication).

## Common Commands

Run `copera <command> --help` for the full list of flags and examples.

### Auth

```bash
copera auth login
copera auth status
copera auth whoami
copera auth logout
```

### Boards and Tables

```bash
copera boards list
copera boards list --query "roadmap"
copera boards get <board-id>

copera tables list --board <board-id>
copera tables list --board <board-id> --query "tasks"
copera tables get <table-id> --board <board-id>
copera tables export <table-id> --board <board-id> --view <view-id> --format CSV -o out.csv
```

`copera bases` is an alias for `copera boards`.

### Rows

```bash
copera rows list --board <board-id> --table <table-id>
copera rows list --board <board-id> --table <table-id> --query "oauth"
copera rows get <row-id> --board <board-id> --table <table-id>
copera rows create --board <board-id> --table <table-id> --data '{"columns":[{"columnId":"<column-id>","value":"Hello"}]}'
copera rows update <row-id> --board <board-id> --table <table-id> --data '{"columns":[{"columnId":"<column-id>","value":"Updated"}]}'
copera rows delete <row-id> --board <board-id> --table <table-id> --force

# Fixed legacy row description, shown by rows get as "Description (legacy)"
copera rows description <row-id> --board <board-id> --table <table-id>
copera rows update-description <row-id> --board <board-id> --table <table-id> --content "New description"

# RICH TEXT / DESCRIPTION table column cells (modern long-text columns)
copera rows column-content <row-id> --board <board-id> --table <table-id> --column <column-id>
copera rows update-column-content <row-id> --board <board-id> --table <table-id> --column <column-id> --content "# Notes"

copera rows comment <row-id> --board <board-id> --table <table-id> --content "Looks good"
copera rows comments <row-id> --board <board-id> --table <table-id>
```

You can also pipe JSON or text into commands that accept stdin:

```bash
echo '{"columns":[{"columnId":"<column-id>","value":"Hello"}]}' | copera rows create --board <board-id> --table <table-id>
echo "Looks good" | copera rows comment <row-id> --board <board-id> --table <table-id>
echo "# Notes" | copera rows update-column-content <row-id> --board <board-id> --table <table-id> --column <column-id>
```

Rows have two separate long-text surfaces:

- Fixed legacy row description: use `rows description` and `rows update-description`.
- RICH TEXT / DESCRIPTION table column cells: use `rows column-content --column <column-id>` and `rows update-column-content --column <column-id>`.

Do not use `rows update-description` for a table column named `Description`.
Run `copera tables get <table-id> --board <board-id> --json` to find the
column ID, then use `rows update-column-content --column <column-id>`.

### Docs

Docs commands require a Personal Access Token.

```bash
copera docs tree
copera docs tree --parent <doc-id>
copera docs search "keyword"
copera docs get <doc-id>
copera docs content <doc-id>
copera docs create --title "New Doc" --content "Initial content"
copera docs update <doc-id> --content "Replacement content"
copera docs update <doc-id> --operation append --content "More content"
copera docs metadata <doc-id> --title "New title"
copera docs delete <doc-id> --force
```

### Drive

Drive commands require a Personal Access Token with drive access.

```bash
copera drive tree
copera drive tree --parent <folder-id>
copera drive search "quarterly report"
copera drive get <file-id>
copera drive download <file-id> -o report.pdf
copera drive upload ./report.pdf --parent <folder-id>
copera drive upload ./project/ --parent <folder-id>
copera drive mkdir "New Folder"
```

Uploads support multipart transfer, configurable chunk size, concurrent part
uploads, progress output in interactive terminals, and recursive directory
upload.

### Channels

```bash
copera channels list
copera channels list --query "deploy"
copera channels list --kind dm --participant <user-id>

copera channels message send "Hello" --channel <channel-id>
copera channels message send "Hello" --user <user-id>
echo "Deploy done" | copera channels message send --channel <channel-id>
```

### Workspace, Search, and Notifications

```bash
copera workspace info
copera workspace members
copera workspace teams

copera search "contract"
copera search "contract" --type document --type driveContent

copera notifications list
copera notifications read <notification-id>
copera notifications unread <notification-id>
copera notifications delete <notification-id> --force
```

### Cache and Utilities

```bash
copera cache status
copera cache clean

copera version
copera version --json
copera completion bash
copera completion zsh
copera completion fish
```

## Configuration

Profiles group a token and default resource IDs together. Create
`~/.copera.toml` when you want to avoid repeating flags such as `--board` and
`--table`.

```toml
default_profile = "work"

[profiles.default]
token = "cp_pat_abc123..."
board_id = "66abc123def456789012abcd"

[profiles.work]
token = "cp_key_xyz789..."
board_id = "66ghi789jkl012345678mnop"
table_id = "66pqr012stu345678901vwxy"

[output]
format = "auto" # auto | json | table | plain

[cache]
ttl = "1h"
```

Switch profiles with `--profile` or `COPERA_PROFILE`:

```bash
copera boards list --profile work
COPERA_PROFILE=work copera boards list
```

For shared project defaults, commit `.copera.toml` without tokens:

```toml
[profiles.default]
board_id = "66abc123def456789012abcd"
table_id = "66pqr012stu345678901vwxy"
```

For local secrets, use `.copera.local.toml` and keep it out of git:

```toml
[profiles.default]
token = "cp_pat_xxx"
```

## Machine-Readable Output

The CLI is designed to work well in scripts and agent workflows:

- When stdout is not a TTY, output defaults to JSON.
- `--json` forces JSON output.
- `--output` accepts `auto`, `json`, `table`, or `plain`.
- `--quiet` / `-q` suppresses informational messages.
- Errors are emitted on stderr as structured JSON.
- Exit codes are stable: `0=ok`, `1=error`, `2=usage`, `3=not found`,
  `4=auth`, `5=conflict`, `6=rate limited`.
- `--no-input` and `CI=true` disable interactive prompts.

Example structured error:

```json
{"error":"resource_not_found","message":"Board 'abc123' not found","suggestion":"Run 'copera boards list' to see accessible boards","transient":false}
```

## Environment Variables

| Variable | Description |
|---|---|
| `COPERA_CLI_AUTH_TOKEN` | API token; overrides config files. |
| `COPERA_PROFILE` | Active config profile name. |
| `COPERA_NO_UPDATE_CHECK` | Set to `1` to disable background version checks. |
| `COPERA_SANDBOX` | Set to `1` to use the dev API. |
| `CI` | Set to `true` to disable prompts and update checks. |
| `NO_COLOR` | Disable ANSI color output. |

## Related

- [Copera Developer Docs](https://developers.copera.ai/)
- [AGENTS.md](./AGENTS.md) for LLM agents using this CLI
