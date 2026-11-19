package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newBoardsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "boards",
		Aliases: []string{"bases"},
		Short:   "Manage boards",
	}
	cmd.AddCommand(
		newBoardsListCmd(cli),
		newBoardsGetCmd(cli),
		newBoardsCreateCmd(cli),
		newBoardsUpdateCmd(cli),
		newBoardsDeleteCmd(cli),
		newBoardsParticipantsCmd(cli),
	)
	return cmd
}

// ── boards list ──────────────────────────────────────────────────────────────

func newBoardsListCmd(cli *CLI) *cobra.Command {
	var flagQuery string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all boards",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boards, err := client.BoardList(context.Background(), &api.BoardListOptions{
				Query: flagQuery,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(boards)
			}

			if len(boards) == 0 {
				cli.Printer.Info("No boards found.")
				return nil
			}

			for i, b := range boards {
				cli.Printer.PrintLine(fmt.Sprintf("ID:          %s", b.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Name:        %s", b.Name))
				cli.Printer.PrintLine(fmt.Sprintf("Description: %s", b.Description))
				cli.Printer.PrintLine(fmt.Sprintf("Updated:     %s", b.UpdatedAt.Format("2006-01-02 15:04")))
				if i < len(boards)-1 {
					cli.Printer.PrintLine("")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagQuery, "query", "", "Search by board name or description")
	return cmd
}

// ── boards get ───────────────────────────────────────────────────────────────

func newBoardsGetCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get board details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			board, err := client.BoardGet(context.Background(), args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(board)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:          %s", board.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:        %s", board.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Description: %s", board.Description))
			cli.Printer.PrintLine(fmt.Sprintf("Created:     %s", board.CreatedAt.Format("2006-01-02 15:04")))
			cli.Printer.PrintLine(fmt.Sprintf("Updated:     %s", board.UpdatedAt.Format("2006-01-02 15:04")))

			return nil
		},
	}
}

// ── tables list ──────────────────────────────────────────────────────────────

func newTablesCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tables",
		Short: "Manage tables within a board",
	}
	cmd.AddCommand(
		newTablesListCmd(cli),
		newTablesGetCmd(cli),
		newTablesExportCmd(cli),
		newTablesCreateCmd(cli),
		newTablesUpdateCmd(cli),
		newTablesDeleteCmd(cli),
		newTablesColumnsCmd(cli),
	)
	return cmd
}

func newTablesListCmd(cli *CLI) *cobra.Command {
	var flagBoard string
	var flagQuery string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tables in a board",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			tables, err := client.TableList(context.Background(), boardID, &api.TableListOptions{
				Query: flagQuery,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(tables)
			}

			if len(tables) == 0 {
				cli.Printer.Info("No tables found in board %s.", boardID)
				return nil
			}

			for i, tb := range tables {
				cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", tb.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Name:    %s", tb.Name))
				cli.Printer.PrintLine(fmt.Sprintf("Columns: %d", len(tb.Columns)))
				cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", tb.UpdatedAt.Format("2006-01-02 15:04")))
				if i < len(tables)-1 {
					cli.Printer.PrintLine("")
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagQuery, "query", "", "Search by table name")
	return cmd
}

// ── tables get ───────────────────────────────────────────────────────────────

func newTablesGetCmd(cli *CLI) *cobra.Command {
	var flagBoard string

	cmd := &cobra.Command{
		Use:   "get <table-id>",
		Short: "Get table details with column definitions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			table, err := client.TableGet(context.Background(), boardID, args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(table)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", table.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:    %s", table.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Board:   %s", table.Board))
			cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", table.UpdatedAt.Format("2006-01-02 15:04")))
			cli.Printer.PrintLine("")
			cli.Printer.PrintLine("Columns:")
			for _, col := range table.Columns {
				line := fmt.Sprintf("  %s  %-12s  %s", col.ColumnID, col.Type, col.Label)
				if len(col.Options) > 0 {
					opts := make([]string, len(col.Options))
					for i, o := range col.Options {
						opts[i] = o.Label
					}
					line += fmt.Sprintf("  [%s]", joinMax(opts, 5))
				}
				cli.Printer.PrintLine(line)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	return cmd
}

// joinMax joins up to max items with ", " and appends "+N more" if truncated.
func joinMax(items []string, max int) string {
	if len(items) <= max {
		return joinStr(items, ", ")
	}
	return joinStr(items[:max], ", ") + fmt.Sprintf(" +%d more", len(items)-max)
}

func joinStr(items []string, sep string) string {
	result := ""
	for i, s := range items {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// ── tables export ───────────────────────────────────────────────────────────

func newTablesExportCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagView, flagFormat, flagOutput string
	var flagColumns, flagRows []string
	var flagSaveToDrive, flagForceAsync, flagIncludeHidden, flagIncludeSystem bool

	cmd := &cobra.Command{
		Use:   "export <table-id>",
		Short: "Export a table view to a downloadable format",
		Long: `Export a table view (CSV / XLSX / JSON / MARKDOWN / HTML / PDF / ZIP / ICS).

Text/spreadsheet formats are returned inline — the payload is written to --output
(or stdout if no path is given). PDF / ZIP and large renders are queued through
the async pipeline and the command prints the job descriptor instead.

Required:
  --view <view-id>   the view to render (boards can have multiple views)

Examples:
  copera tables export <id> --view <view-id> --format CSV --output report.csv
  copera tables export <id> --view <view-id> --format JSON --output - > rows.json
  copera tables export <id> --view <view-id> --format PDF --save-to-drive`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if flagView == "" {
				cli.Printer.PrintError("missing_flag", "--view is required",
					"Use --view <view-id>", false)
				return exitcodes.Newf(exitcodes.Usage, "--view is required")
			}

			if flagFormat != "" && !api.IsValidExportFormat(flagFormat) {
				cli.Printer.PrintError("input_error",
					fmt.Sprintf("invalid format %q", flagFormat),
					"Use one of CSV|XLSX|JSON|MARKDOWN|HTML|PDF|ZIP|ICS", false)
				return exitcodes.Newf(exitcodes.Usage, "invalid format")
			}

			input := &api.ExportTableInput{
				BoardID:              boardID,
				ViewID:               flagView,
				Format:               api.ExportFormat(flagFormat),
				ColumnIDs:            flagColumns,
				RowIDs:               flagRows,
				IncludeHidden:        flagIncludeHidden,
				IncludeSystemColumns: flagIncludeSystem,
				ForceAsync:           flagForceAsync,
				SaveToDrive:          flagSaveToDrive,
			}

			resp, err := client.TableExport(context.Background(), boardID, args[0], input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(resp)
			}

			if resp.IsAsync() {
				j := resp.AsyncJob
				cli.Printer.PrintLine(fmt.Sprintf("Job:    %s", j.JobID))
				cli.Printer.PrintLine(fmt.Sprintf("Status: %s", j.Status))
				cli.Printer.PrintLine(fmt.Sprintf("Format: %s", j.Format))
				if j.ExpiresAt != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Expires: %s", j.ExpiresAt))
				}
				if j.DownloadURL != nil {
					cli.Printer.PrintLine(fmt.Sprintf("URL:    %s", *j.DownloadURL))
				}
				cli.Printer.Info("Export queued. Poll the workspace UI or async API for completion.")
				return nil
			}

			if err := writeExportPayload(cli, flagOutput, resp.Payload); err != nil {
				return exitcodes.New(exitcodes.Error, err)
			}
			cli.Printer.Info("Export complete (%d rows, %s).", resp.RowCount, resp.FileName)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagView, "view", "", "View ID (required)")
	cmd.Flags().StringVar(&flagFormat, "format", "", "Export format: CSV|XLSX|JSON|MARKDOWN|HTML|PDF|ZIP|ICS")
	cmd.Flags().StringVarP(&flagOutput, "output-file", "o", "", "Write payload to this file (default: stdout; '-' = stdout)")
	cmd.Flags().StringSliceVar(&flagColumns, "column", nil, "Restrict to these column IDs (repeatable)")
	cmd.Flags().StringSliceVar(&flagRows, "row", nil, "Restrict to these row IDs (repeatable)")
	cmd.Flags().BoolVar(&flagIncludeHidden, "include-hidden", false, "Include hidden columns in the export")
	cmd.Flags().BoolVar(&flagIncludeSystem, "include-system", false, "Include system columns")
	cmd.Flags().BoolVar(&flagSaveToDrive, "save-to-drive", false, "Save the rendered file to drive instead of returning inline")
	cmd.Flags().BoolVar(&flagForceAsync, "force-async", false, "Force the server to use the async pipeline")
	return cmd
}

// writeExportPayload writes payload to a file (or stdout when path is "" or "-").
func writeExportPayload(cli *CLI, path, payload string) error {
	if path == "" || path == "-" {
		cli.Printer.PrintLine(payload)
		return nil
	}
	return os.WriteFile(path, []byte(payload), 0o644)
}

// confirmDestructive gates a destructive action. With --force it proceeds
// immediately. Otherwise, in non-interactive mode (--no-input / CI) it refuses
// and requires --force; in interactive mode it prompts the user.
// Returns proceed=true to continue. A non-nil error is a usage error the caller
// should return directly.
func confirmDestructive(cli *CLI, force bool, prompt string) (bool, error) {
	if force {
		return true, nil
	}
	if cli.IsNonInteractive() {
		cli.Printer.PrintError("confirmation_required",
			"refusing to perform a destructive action without confirmation",
			"Re-run with --force to proceed non-interactively", false)
		return false, exitcodes.Newf(exitcodes.Usage, "confirmation required")
	}
	fmt.Fprint(cli.Printer.Out, prompt)
	var ans string
	fmt.Fscanln(cli.Stdin, &ans)
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
		cli.Printer.Info("Aborted.")
		return false, nil
	}
	return true, nil
}

// parseColumnOptions turns repeatable "Label" or "Label:color" flag values into
// column option inputs.
func parseColumnOptions(raw []string) []api.ColumnOptionInput {
	if len(raw) == 0 {
		return nil
	}
	opts := make([]api.ColumnOptionInput, 0, len(raw))
	for _, r := range raw {
		label, color, found := strings.Cut(r, ":")
		o := api.ColumnOptionInput{Label: strings.TrimSpace(label)}
		if found {
			o.Color = strings.TrimSpace(color)
		}
		opts = append(opts, o)
	}
	return opts
}

// ── boards create ────────────────────────────────────────────────────────────

func newBoardsCreateCmd(cli *CLI) *cobra.Command {
	var flagName, flagDescription, flagTableName string
	var flagData string
	var flagColumns []string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new board",
		Long: `Create a new board. The board is created with an initial table.

Example:
  copera boards create --name "Q3 Roadmap"
  copera boards create --name "CRM" --description "Sales pipeline" --table-name "Leads"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			payload, err := readObjectInput(cli, flagData, false)
			if err != nil {
				return usageError(cli, err)
			}
			if payload == nil {
				payload = map[string]any{}
			}
			if cmd.Flags().Changed("name") {
				payload["name"] = flagName
			}
			if cmd.Flags().Changed("description") {
				payload["description"] = flagDescription
			}
			if cmd.Flags().Changed("table-name") {
				payload["tableName"] = flagTableName
			}
			if len(flagColumns) > 0 {
				columns := make([]any, 0, len(flagColumns))
				for _, raw := range flagColumns {
					var column map[string]any
					if err := json.Unmarshal([]byte(raw), &column); err != nil {
						return usageError(cli, fmt.Errorf("invalid --column JSON: %w", err))
					}
					columns = append(columns, column)
				}
				payload["columns"] = columns
			}
			name, _ := payload["name"].(string)
			if name == "" {
				cli.Printer.PrintError("missing_input", "board name is required",
					"Use --name <board-name> or provide name in --data", false)
				return exitcodes.Newf(exitcodes.Usage, "board name is required")
			}

			var board api.CreatedBoard
			err = client.DoJSON(context.Background(), "POST", "/board", payload, &board)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(&board)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:              %s", board.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:            %s", board.Name))
			if board.Description != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Description:     %s", board.Description))
			}
			if board.DefaultTableID != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Default table:   %s", board.DefaultTableID))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagName, "name", "", "Board name (required)")
	cmd.Flags().StringVar(&flagDescription, "description", "", "Board description")
	cmd.Flags().StringVar(&flagTableName, "table-name", "", "Name for the initial table (default: Table)")
	cmd.Flags().StringVar(&flagData, "data", "", "Full board JSON inline, @file, or - for stdin")
	cmd.Flags().StringArrayVar(&flagColumns, "column", nil, `Initial column JSON, e.g. {"label":"Due","type":"DATE","format":"DATE_ISO"} (repeatable)`)
	return cmd
}

// ── boards update ────────────────────────────────────────────────────────────

func newBoardsUpdateCmd(cli *CLI) *cobra.Command {
	var flagName, flagDescription, flagIcon string

	cmd := &cobra.Command{
		Use:   "update <board-id>",
		Short: "Update a board's name, description, or icon",
		Long: `Update board properties. Only the flags you pass are changed.

Example:
  copera boards update <board-id> --name "New name"
  copera boards update <board-id> --description "Updated" --icon "🚀"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			input := &api.UpdateBoardInput{}
			if cmd.Flags().Changed("name") {
				input.Name = &flagName
			}
			if cmd.Flags().Changed("description") {
				input.Description = &flagDescription
			}
			if cmd.Flags().Changed("icon") {
				input.Icon = &flagIcon
			}
			if input.Name == nil && input.Description == nil && input.Icon == nil {
				cli.Printer.PrintError("missing_input", "nothing to update",
					"Pass at least one of --name, --description, --icon", false)
				return exitcodes.Newf(exitcodes.Usage, "nothing to update")
			}

			board, err := client.BoardUpdate(context.Background(), args[0], input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(board)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:          %s", board.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:        %s", board.Name))
			if board.Description != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Description: %s", board.Description))
			}
			cli.Printer.Info("Board updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagName, "name", "", "New board name")
	cmd.Flags().StringVar(&flagDescription, "description", "", "New board description")
	cmd.Flags().StringVar(&flagIcon, "icon", "", "New board icon")
	return cmd
}

// ── boards delete ────────────────────────────────────────────────────────────

func newBoardsDeleteCmd(cli *CLI) *cobra.Command {
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "delete <board-id>",
		Short: "Delete a board (moves it to trash)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			proceed, err := confirmDestructive(cli, flagForce,
				fmt.Sprintf("Delete board %s? [y/N]: ", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			resp, err := client.BoardDelete(context.Background(), args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{"deleted": resp.Success, "boardId": args[0]})
			}
			cli.Printer.Info("Board deleted.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

// ── boards participants ──────────────────────────────────────────────────────

func newBoardsParticipantsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "participants",
		Short: "Manage board participants",
	}
	cmd.AddCommand(
		newBoardsParticipantsAddCmd(cli),
		newBoardsParticipantsRemoveCmd(cli),
	)
	return cmd
}

func newBoardsParticipantsAddCmd(cli *CLI) *cobra.Command {
	var flagBoard string
	var flagParticipants []string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add participants to a board",
		Long: `Add one or more participants (workspace users, teams, or AI agents) to a board.

Example:
  copera boards participants add --board <board-id> --participant u001 --participant u002`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if len(flagParticipants) == 0 {
				cli.Printer.PrintError("missing_input", "at least one participant is required",
					"Use --participant <id> (repeatable)", false)
				return exitcodes.Newf(exitcodes.Usage, "no participants provided")
			}

			resp, err := client.BoardAddParticipants(context.Background(), boardID, &api.AddParticipantsInput{
				ParticipantIDs: flagParticipants,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(resp)
			}

			cli.Printer.PrintLine(fmt.Sprintf("Board:        %s", resp.BoardID))
			cli.Printer.PrintLine(fmt.Sprintf("Participants: %d", len(resp.ParticipantUserIDs)))
			cli.Printer.Info("Participants added.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringSliceVar(&flagParticipants, "participant", nil, "Participant ID to add (repeatable)")
	return cmd
}

func newBoardsParticipantsRemoveCmd(cli *CLI) *cobra.Command {
	var flagBoard string
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "remove <participant-id>",
		Short: "Remove a participant from a board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			proceed, err := confirmDestructive(cli, flagForce,
				fmt.Sprintf("Remove participant %s from board %s? [y/N]: ", args[0], boardID))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			resp, err := client.BoardRemoveParticipant(context.Background(), boardID, args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{"removed": resp.Success, "participantId": args[0]})
			}
			cli.Printer.Info("Participant removed.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

// ── tables create ────────────────────────────────────────────────────────────

func newTablesCreateCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagName string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new table in a board",
		Long: `Create a new table in a board.

Example:
  copera tables create --board <board-id> --name "Tasks"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if flagName == "" {
				cli.Printer.PrintError("missing_input", "table name is required",
					"Use --name <table-name>", false)
				return exitcodes.Newf(exitcodes.Usage, "table name is required")
			}

			table, err := client.TableCreate(context.Background(), boardID, &api.CreateTableInput{Name: flagName})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(table)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:    %s", table.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:  %s", table.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Board: %s", table.BoardID))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagName, "name", "", "Table name (required)")
	return cmd
}

// ── tables update ────────────────────────────────────────────────────────────

func newTablesUpdateCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagName string

	cmd := &cobra.Command{
		Use:   "update <table-id>",
		Short: "Rename a table",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if flagName == "" {
				cli.Printer.PrintError("missing_input", "table name is required",
					"Use --name <table-name>", false)
				return exitcodes.Newf(exitcodes.Usage, "table name is required")
			}

			table, err := client.TableUpdate(context.Background(), boardID, args[0], &api.UpdateTableInput{Name: flagName})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(table)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:    %s", table.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:  %s", table.Name))
			cli.Printer.Info("Table updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagName, "name", "", "New table name (required)")
	return cmd
}

// ── tables delete ────────────────────────────────────────────────────────────

func newTablesDeleteCmd(cli *CLI) *cobra.Command {
	var flagBoard string
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "delete <table-id>",
		Short: "Delete a table",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			proceed, err := confirmDestructive(cli, flagForce,
				fmt.Sprintf("Delete table %s? [y/N]: ", args[0]))
			if err != nil {
				return err
			}
			if !proceed {
				return nil
			}

			resp, err := client.TableDelete(context.Background(), boardID, args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{"deleted": resp.Success, "tableId": args[0]})
			}
			cli.Printer.Info("Table deleted.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

// ── tables columns ───────────────────────────────────────────────────────────

func newTablesColumnsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "columns",
		Short: "Manage table columns",
	}
	cmd.AddCommand(
		newTablesColumnsAddCmd(cli),
		newTablesColumnsUpdateCmd(cli),
	)
	return cmd
}

func newTablesColumnsAddCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable, flagLabel, flagType, flagConfig string
	var flagOptions, flagOptionJSON []string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a column to a table",
		Long: `Add a column to a table.

--type must be one of (UPPERCASE):
  TEXT PARAGRAPH NUMBER MONEY DATE EMAIL PHONE WEBSITE CHECKBOX
  STATUS DROPDOWN LABELS USERS CONTACT LOCATION DESCRIPTION FILE CHECKLIST
  LINK LOOKUP ROLLUP FORMULA CREATED_BY CREATED_AT LAST_MODIFIED_BY UPDATED_AT SLA

--config accepts type-specific JSON: format, numberOptions, defaultValue, formula,
tableRef, syncLink, linkMode, maxLinks, displayColumnId, restrictToViewId,
linkingColumnRef, columnRef, aggregator, filter, slaConfig.
SLA requires slaConfig; COUNTDOWN requires goalMs; COUNTUP requires startCondition.
STOPWATCH supports only MANUAL/NONE triggers.
Audit columns display read-only row metadata.

--option provides selectable values for STATUS, DROPDOWN, and LABELS columns
(repeatable). Format: "Label" or "Label:color".
--option-json accepts {"label":"Open","color":"green","order":0,"statusGroup":"TODO"}
(repeatable). statusGroup applies to STATUS and accepts TODO, IN_PROGRESS, or DONE.

Example:
  copera tables columns add --board <B> --table <T> --label "Priority" --type STATUS \
    --option "Low:green" --option "High:red"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			tableID, err := resolveID(nil, flagTable, cfg.TableID, "table ID (--table or config table_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --table <id> or set table_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if flagLabel == "" {
				cli.Printer.PrintError("missing_input", "column label is required",
					"Use --label <label>", false)
				return exitcodes.Newf(exitcodes.Usage, "column label is required")
			}

			flagType = strings.ToUpper(strings.TrimSpace(flagType))
			if !api.IsValidColumnType(flagType) {
				cli.Printer.PrintError("input_error",
					fmt.Sprintf("invalid column type %q", flagType),
					"Use one of: "+strings.Join(api.PublicCreatableColumnTypes, " "), false)
				return exitcodes.Newf(exitcodes.Usage, "invalid column type")
			}

			input := &api.AddColumnInput{Label: flagLabel, Type: flagType, Options: parseColumnOptions(flagOptions)}
			for _, raw := range flagOptionJSON {
				var option api.ColumnOptionInput
				decoder := json.NewDecoder(strings.NewReader(raw))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&option); err != nil {
					return usageError(cli, fmt.Errorf("invalid --option-json: %w", err))
				}
				if err := validateColumnOption(option.Label, option.StatusGroup); err != nil {
					return usageError(cli, err)
				}
				input.Options = append(input.Options, option)
			}
			if flagConfig != "" {
				decoder := json.NewDecoder(strings.NewReader(flagConfig))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&input.ColumnConfiguration); err != nil {
					return usageError(cli, fmt.Errorf("invalid --config JSON: %w", err))
				}
			}
			if input.Type == "SLA" && input.SLAConfig == nil {
				return usageError(cli, fmt.Errorf("SLA requires --config with slaConfig"))
			}
			if input.Type != "SLA" && input.SLAConfig != nil {
				return usageError(cli, fmt.Errorf("slaConfig requires SLA"))
			}
			if input.SLAConfig != nil {
				if err := validateSLAConfig(input.SLAConfig); err != nil {
					return usageError(cli, err)
				}
			}
			col, err := client.ColumnAdd(context.Background(), boardID, tableID, input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(col)
			}

			cli.Printer.PrintLine(fmt.Sprintf("Column:  %s", col.ColumnID))
			cli.Printer.PrintLine(fmt.Sprintf("Label:   %s", col.Label))
			cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", col.Type))
			if len(col.Options) > 0 {
				opts := make([]string, len(col.Options))
				for i, o := range col.Options {
					opts[i] = o.Label
				}
				cli.Printer.PrintLine(fmt.Sprintf("Options: %s", strings.Join(opts, ", ")))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().StringVar(&flagLabel, "label", "", "Column label (required)")
	cmd.Flags().StringVar(&flagType, "type", "", "Column type (required)")
	cmd.Flags().StringArrayVar(&flagOptions, "option", nil, `Option for STATUS/DROPDOWN/LABELS: "Label" or "Label:color" (repeatable)`)
	cmd.Flags().StringArrayVar(&flagOptionJSON, "option-json", nil, "Column option JSON with label, color, order, and statusGroup (repeatable)")
	cmd.Flags().StringVar(&flagConfig, "config", "", "Type-specific column configuration JSON (including slaConfig)")
	return cmd
}

func newTablesColumnsUpdateCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable, flagLabel, flagSLAConfig string
	var flagAddOptions, flagUpdateOptions, flagDeleteOptionIDs []string

	cmd := &cobra.Command{
		Use:   "update <column-id>",
		Short: "Update a column label, options, or SLA configuration",
		Long: `Update a column. Rename via --label, append options via --add-option,
edit existing options via --update-option JSON, remove named choices via
--delete-option, or replace an SLA column configuration via --sla-config JSON.
--update-option accepts {"optionId":"<id>","label":"Done","color":"green","order":2,"statusGroup":"DONE"}.

--add-option only applies to STATUS/DROPDOWN/LABELS columns and is append-only
(repeatable). Format: "Label" or "Label:color".

Example:
  copera tables columns update <column-id> --board <B> --table <T> --label "Stage"
  copera tables columns update <column-id> --board <B> --table <T> --add-option "Blocked:red"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boardID, err := resolveID(nil, flagBoard, cfg.BoardID, "board ID (--board or config board_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --board <id> or set board_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			tableID, err := resolveID(nil, flagTable, cfg.TableID, "table ID (--table or config table_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --table <id> or set table_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			input := &api.UpdateColumnInput{}
			if cmd.Flags().Changed("label") {
				input.Label = &flagLabel
			}
			input.AddOptions = parseColumnOptions(flagAddOptions)
			input.DeleteOptionIDs = flagDeleteOptionIDs
			for _, raw := range flagUpdateOptions {
				var option api.ColumnOptionUpdateInput
				decoder := json.NewDecoder(strings.NewReader(raw))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&option); err != nil {
					return usageError(cli, fmt.Errorf("invalid --update-option JSON: %w", err))
				}
				if option.OptionID == "" {
					return usageError(cli, fmt.Errorf("--update-option requires optionId"))
				}
				if option.StatusGroup != nil {
					if *option.StatusGroup == "" {
						return usageError(cli, fmt.Errorf("statusGroup must be TODO, IN_PROGRESS, or DONE"))
					}
					if err := validateColumnOption("option", *option.StatusGroup); err != nil {
						return usageError(cli, err)
					}
				}
				input.UpdateOptions = append(input.UpdateOptions, option)
			}
			if cmd.Flags().Changed("sla-config") {
				input.SLAConfig = &api.SLAConfig{}
				decoder := json.NewDecoder(strings.NewReader(flagSLAConfig))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(input.SLAConfig); err != nil {
					return usageError(cli, fmt.Errorf("invalid --sla-config JSON: %w", err))
				}
				if err := validateSLAConfig(input.SLAConfig); err != nil {
					return usageError(cli, err)
				}
			}
			if input.Label == nil && len(input.AddOptions) == 0 && len(input.UpdateOptions) == 0 && len(input.DeleteOptionIDs) == 0 && input.SLAConfig == nil {
				cli.Printer.PrintError("missing_input", "nothing to update",
					"Pass --label, --add-option, --update-option, --delete-option, or --sla-config", false)
				return exitcodes.Newf(exitcodes.Usage, "nothing to update")
			}

			col, err := client.ColumnUpdate(context.Background(), boardID, tableID, args[0], input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(col)
			}

			cli.Printer.PrintLine(fmt.Sprintf("Column:  %s", col.ColumnID))
			cli.Printer.PrintLine(fmt.Sprintf("Label:   %s", col.Label))
			cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", col.Type))
			if len(col.Options) > 0 {
				opts := make([]string, len(col.Options))
				for i, o := range col.Options {
					opts[i] = o.Label
				}
				cli.Printer.PrintLine(fmt.Sprintf("Options: %s", strings.Join(opts, ", ")))
			}
			cli.Printer.Info("Column updated.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().StringVar(&flagLabel, "label", "", "New column label")
	cmd.Flags().StringArrayVar(&flagAddOptions, "add-option", nil, `Option to append for STATUS/DROPDOWN/LABELS: "Label" or "Label:color" (repeatable)`)
	cmd.Flags().StringArrayVar(&flagUpdateOptions, "update-option", nil, "Existing option JSON with optionId, label, color, order, and statusGroup (repeatable)")
	cmd.Flags().StringArrayVar(&flagDeleteOptionIDs, "delete-option", nil, "Existing option ID to remove (repeatable)")
	cmd.Flags().StringVar(&flagSLAConfig, "sla-config", "", "Replacement SLA configuration JSON")
	return cmd
}

// validateSLAConfig checks structural rules locally; the API resolves workspace/table references.
func validateSLAConfig(config *api.SLAConfig) error {
	switch config.Mode {
	case "STOPWATCH", "COUNTDOWN", "COUNTUP":
	default:
		return fmt.Errorf("SLA mode must be STOPWATCH, COUNTDOWN, or COUNTUP")
	}
	if config.GoalMs != nil && *config.GoalMs <= 0 {
		return fmt.Errorf("SLA goalMs must be positive")
	}
	if config.Mode == "COUNTDOWN" && config.GoalMs == nil {
		return fmt.Errorf("COUNTDOWN requires goalMs")
	}
	if config.Mode == "COUNTUP" && config.StartCondition == nil {
		return fmt.Errorf("COUNTUP requires startCondition")
	}
	if thresholds := config.Thresholds; thresholds != nil {
		if thresholds.WarningPct == nil || thresholds.CriticalPct == nil || *thresholds.WarningPct < 0 || *thresholds.CriticalPct > 1 || *thresholds.WarningPct >= *thresholds.CriticalPct {
			return fmt.Errorf("SLA thresholds require 0 <= warningPct < criticalPct <= 1")
		}
	}
	for _, entry := range []struct {
		condition *api.SLACondition
		start     bool
	}{{config.StartCondition, true}, {config.PauseCondition, false}, {config.StopCondition, false}} {
		condition := entry.condition
		if condition == nil {
			continue
		}
		if config.Mode == "STOPWATCH" && condition.Type != "MANUAL" && condition.Type != "NONE" {
			return fmt.Errorf("STOPWATCH only supports MANUAL/NONE conditions")
		}
		switch condition.Type {
		case "ON_CREATE":
			if !entry.start {
				return fmt.Errorf("ON_CREATE is only valid for startCondition")
			}
		case "STATUS_CHANGE":
			if condition.StatusColumnID == "" || len(condition.StatusValues) == 0 || len(condition.StatusValues) > 100 {
				return fmt.Errorf("STATUS_CHANGE requires statusColumnId and 1-100 statusValues")
			}
			seen := make(map[string]bool, len(condition.StatusValues))
			for _, value := range condition.StatusValues {
				if seen[value] {
					return fmt.Errorf("statusValues must be unique")
				}
				seen[value] = true
			}
		case "MANUAL", "NONE":
		default:
			return fmt.Errorf("invalid SLA condition type")
		}
		if condition.Type != "STATUS_CHANGE" && (condition.StatusColumnID != "" || condition.StatusValues != nil) {
			return fmt.Errorf("status references require STATUS_CHANGE")
		}
	}
	return nil
}

func validateColumnOption(label, statusGroup string) error {
	if strings.TrimSpace(label) == "" {
		return fmt.Errorf("option label is required")
	}
	switch statusGroup {
	case "", "TODO", "IN_PROGRESS", "DONE":
		return nil
	default:
		return fmt.Errorf("statusGroup must be TODO, IN_PROGRESS, or DONE")
	}
}
