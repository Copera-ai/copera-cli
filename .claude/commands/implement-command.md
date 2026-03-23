# Implement a Copera CLI Command

Implement the next planned command or feature for the `copera` CLI following the project's established patterns.

## Pre-flight

1. **Read `PLAN.md`** to find the next unchecked item.
2. **Read `CLAUDE.md`** to confirm patterns, conventions, and API facts.
3. **Read the relevant ADR** in `docs/decisions/` for the area you're changing.
4. **Read existing code** in the same package before writing new code.

## Implementation checklist

### API layer (`internal/api/`)
- [ ] Add request/response types as Go structs with `json` tags
- [ ] Add method(s) to `*Client` (e.g. `client.BoardList`, `client.RowCreate`)
- [ ] Use `_id` for ID fields (API returns `_id`, not `id`)
- [ ] Handle API quirks: epoch strings for timestamps, `[]any` for polymorphic fields

### Command (`commands/`)
- [ ] Create `commands/<name>.go` with `newXxxCmd(cli *CLI) *cobra.Command`
- [ ] Register in `commands/root.go` via `cmd.AddCommand(newXxxCmd(cli))`
- [ ] Use `requireAPIClient(cli)` to get `(*api.Client, *config.Config, error)`
- [ ] Use `resolveID(args, flagValue, cfg.XxxID, "description")` for ID resolution
- [ ] Handle `cli.Printer.IsJSON()` branch: call `cli.Printer.PrintJSON(data)` and return
- [ ] Human output: use metadata list format (`cli.Printer.PrintLine(fmt.Sprintf("Label: %s", value))`) — not tables, unless the data is dense tabular rows
- [ ] For errors: use `apiError(cli, err)` for API errors, `exitcodes.New(code, err)` for others
- [ ] For missing required IDs: `cli.Printer.PrintError(...)` then `return exitcodes.New(exitcodes.Usage, err)`
- [ ] For stdin input: read from `cli.Stdin` when no flag provides the data
- [ ] For destructive ops: check `cli.IsNonInteractive()` and require `--force`

### Caching (if applicable)
- [ ] Use `newDocCache(cli, cfg)` or `newTableCache(cli, cfg)` — never construct caches directly
- [ ] Cache is non-fatal: if cache read/write fails, fall back gracefully
- [ ] Invalidate cache after mutations (e.g. `docs update` invalidates doc content cache)

### Tests (`commands/<name>_test.go`)
- [ ] Use `testutil.RunCommand(t, args, stdin)` — injects `MemStore`, no disk I/O
- [ ] Use `testutil.RunCommandWithStore(t, args, stdin, store)` when testing cache behavior across calls
- [ ] Mock API with `testutil.NewMockServer(t, testutil.MockRoutes{...}.Handler())`
- [ ] Set up config with `testutil.WriteTempConfigAt(t, path, toml)` + `testutil.SetEnv(t, "HOME", dir)`
- [ ] Test cases:
  - JSON output (`--json` flag)
  - Human output (`--output table` to force non-JSON in test)
  - Flag overrides config defaults (e.g. `--board` overrides `board_id`)
  - Missing required ID returns exit code 2
  - Missing token returns exit code 4
  - API error returns appropriate exit code (3=not found, 4=auth, 6=rate limit)
  - Stdin used as body when no flag (for content-accepting commands)
- [ ] Never hit real API — always mock
- [ ] Never use real tokens — use `"tok_test_fake"`
- [ ] Use `require.NoError` for setup, `assert.*` for behavior checks

### Wrap up
- [ ] `go build ./...` succeeds
- [ ] `go test ./...` passes
- [ ] Update `PLAN.md` to check off the completed item

## Command to implement: $ARGUMENTS
