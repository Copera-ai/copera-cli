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
copera boards create --name "Q3 Roadmap"
copera boards create --name "CRM" \
  --column '{"label":"Contact","type":"CONTACT"}' \
  --column '{"label":"Follow up","type":"DATE","format":"DATE_ISO"}'
copera boards update <board-id> --name "Q4 Roadmap"
copera boards delete <board-id> --force
copera boards participants add --board <board-id> --participant <user-id>
copera boards participants remove <participant-id> --board <board-id>

copera tables list --board <board-id>
copera tables list --board <board-id> --query "tasks"
copera tables get <table-id> --board <board-id>
copera tables export <table-id> --board <board-id> --view <view-id> --format CSV -o out.csv
copera tables create --board <board-id> --name "Tasks"
copera tables update <table-id> --board <board-id> --name "Backlog"
copera tables delete <table-id> --board <board-id> --force
copera tables columns add --board <board-id> --table <table-id> --label "Status" --type STATUS --option "Open" --option "Done"
copera tables columns update <column-id> --board <board-id> --table <table-id> --label "State"
```

`copera bases` is an alias for `copera boards`. Run `copera <command> --help`
for the exact flags each write command accepts.

### Rows

```bash
copera rows list --board <board-id> --table <table-id>
copera rows list --board <board-id> --table <table-id> --query "oauth"
copera rows list --board <board-id> --table <table-id> --offset 0 --limit 100
copera rows query --data @query.json
copera rows aggregate <board-id> <table-id> --data @aggregation.json
copera rows bulk <board-id> <table-id> --data @operations.json
copera rows views <board-id> <table-id>
copera rows get <row-id> --board <board-id> --table <table-id>
copera rows get --row 42 --board <board-id> --table <table-id>   # look up by visible Row#
copera rows create --board <board-id> --table <table-id> --data '{"columns":[{"columnId":"<column-id>","value":"Hello"}]}'
copera rows update <row-id> --board <board-id> --table <table-id> --data '{"columns":[{"columnId":"<column-id>","value":"Updated"}]}'
copera rows delete <row-id> --board <board-id> --table <table-id> --force

# Fixed legacy row description, shown by rows get as "Description (legacy)"
copera rows description <row-id> --board <board-id> --table <table-id>

# RICH TEXT / DESCRIPTION table column cells (modern long-text columns)
copera rows column-content <row-id> --board <board-id> --table <table-id> --column <column-id>
copera rows update-column-content <row-id> --board <board-id> --table <table-id> --column <column-id> --content "# Notes"

copera rows comment <row-id> --board <board-id> --table <table-id> --content "Looks good"
copera rows comments <row-id> --board <board-id> --table <table-id>

# Manage attachments on FILE columns, and download from comments
copera rows attachments download <row-id> --board <board-id> --table <table-id> --column <column-id> --file <file-id> -o ./contract.pdf
copera rows attachments upload <row-id> ./contract.pdf --board <board-id> --table <table-id> --column <column-id>
copera rows attachments delete <row-id> --board <board-id> --table <table-id> --column <column-id> --file <file-id> --force
copera rows comments attachments download <row-id> --board <board-id> --table <table-id> --comment <comment-id> --file <file-id> -o ./contract.pdf

# Authenticate a row using identifier and password columns
copera rows authenticate --board <board-id> --table <table-id> \
  --identifier-column <column-id> --identifier-value "user@example.com" \
  --password-column <column-id> --password-value "secret"
```

You can also pipe JSON or text into commands that accept stdin:

```bash
echo '{"columns":[{"columnId":"<column-id>","value":"Hello"}]}' | copera rows create --board <board-id> --table <table-id>
echo "Looks good" | copera rows comment <row-id> --board <board-id> --table <table-id>
echo "# Notes" | copera rows update-column-content <row-id> --board <board-id> --table <table-id> --column <column-id>
```

Rows have two separate long-text surfaces:

- Fixed legacy row description: use `rows description` to read it. Updating it is no longer supported by the CLI.
- RICH TEXT / DESCRIPTION table column cells: tables can have multiple DESCRIPTION columns, so use `rows column-content --column <column-id>` and `rows update-column-content --column <column-id>`.

For a table column named `Description`, run
`copera tables get <table-id> --board <board-id> --json` to find the column ID,
then use `rows update-column-content --column <column-id>`.

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
copera docs generate --data @document.json
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
copera channels create "deploys" --description "Deploy notifications" --participant <user-id>
copera channels archive <channel-id> --force
copera channels unarchive <channel-id>

copera channels message send "Hello" --channel <channel-id>
copera channels message send "Hello" --user <user-id>
echo "Deploy done" | copera channels message send --channel <channel-id>

copera channels participants add --channel <channel-id> --participant <user-id>
copera channels participants remove <participant-user-id> --channel <channel-id> --force

copera channels dm send "Hi there" --user <user-id>
```

### Workspace

```bash
copera workspace info
copera workspace me                       # the authenticated user's identity
copera workspace members
copera workspace members --query "jane"
copera workspace teams
copera workspace team create --name "Design" --participant <user-id>
```

### Tempo, Agenda, Inbox, Automations, and Bookings

```bash
copera tempo calendars list                 # `time` is an alias
copera tempo calendars create --field name="Focus"
copera tempo shelf create --data '{"title":"Prepare review"}'
copera tempo shelf schedule <item-id> --data @schedule.json

copera agenda events list --query-param from=2026-09-01T00:00:00Z
copera agenda events create --data @event.json
copera agenda events respond <event-id> --field status=ACCEPTED

copera inbox list
copera inbox threads list <inbox-id> --query-param page=1
copera inbox send --data @message.json
copera inbox attachments upload <inbox-id> --file ./contract.pdf
copera inbox delete <inbox-id> --data @email-ids.json --force

copera omni channels list
copera omni conversations list <channel-id> --query-param filter=MINE
copera omni conversations get <conversation-id>
copera omni messages list <conversation-id>
copera omni reply <conversation-id> --field text="On it!"
copera omni whatsapp send <channel-id> --field phoneNumber=+5511999990000 --field text="Hello"
copera omni whatsapp start <channel-id> --field phoneNumber=+5511999990000

copera automations list
copera automations run <automation-id> --data @trigger.json
copera automations retry <run-id>

copera bookings list
copera bookings list --status CONFIRMED --from 2026-01-01T00:00:00Z
copera bookings get <booking-id>
copera bookings cancel <booking-id> --field reason="Customer request" --force
copera booking-types list
copera booking-types create --data @booking-type.json
copera booking-host profile get
copera booking-host schedules create --data @schedule.json
copera booking-host links create <booking-type-id> --field maxUses=1

copera meeting-notes list
copera meeting-notes list --query "standup"
copera meeting-notes get <note-id>
copera meeting-notes get <note-id> --transcript
```

Newer structured commands accept `--data '{...}'`, `--data @file.json`, or
`--data -` for stdin. Repeatable `--field key=value` flags override top-level
JSON fields, and `--query-param key=value` adds URL query parameters. Destructive
commands require interactive confirmation or `--force`.

Inbox requires a Personal Access Token with `access_inbox`, and Omni requires
`access_omni`; both are deliberately available through REST and the CLI, not
through hosted MCP. `copera omni whatsapp send` finds the open conversation for
the number (reopening a closed one, or starting a new outbound thread) before
delivering the text.

### Exports

```bash
copera exports job <job-id>
copera exports result-set --data '{"resultSetId":"<id>","format":"CSV"}'
```

### Search and Notifications

```bash
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
