package commands

import (
	"context"
	"fmt"
	"os"

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
