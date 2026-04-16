package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/cache"
	"github.com/copera/copera-cli/internal/config"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/output"
	"github.com/spf13/cobra"
)

func newRowsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rows",
		Short: "Manage rows within a table",
	}
	cmd.AddCommand(
		newRowsListCmd(cli),
		newRowsGetCmd(cli),
		newRowsCreateCmd(cli),
		newRowsUpdateCmd(cli),
		newRowsUpdateDescriptionCmd(cli),
		newRowsDeleteCmd(cli),
		newRowsAuthenticateCmd(cli),
	)
	return cmd
}

// ── rows list ────────────────────────────────────────────────────────────────

func newRowsListCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rows in a table",
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

			rows, err := client.RowList(context.Background(), boardID, tableID)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(rows)
			}

			if len(rows) == 0 {
				cli.Printer.Info("No rows found.")
				return nil
			}

			t := output.NewTable(cli.Printer)
			t.Header("ID", "Row#", "Owner", "Columns", "Updated")
			for _, r := range rows {
				t.Row(r.ID, r.RowID, r.Owner, len(r.Columns), r.UpdatedAt.Format("2006-01-02"))
			}
			t.Render()
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	return cmd
}

// ── rows get ─────────────────────────────────────────────────────────────────

func newRowsGetCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable string

	cmd := &cobra.Command{
		Use:   "get <row-id>",
		Short: "Get a single row",
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

			tableID, err := resolveID(nil, flagTable, cfg.TableID, "table ID (--board or config table_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --table <id> or set table_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			row, err := client.RowGet(context.Background(), boardID, tableID, args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(row)
			}

			// Fetch table schema (cached) to resolve column/option labels
			td := fetchTableData(cli, client, cfg, boardID, tableID)

			cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", row.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Row#:    %d", row.RowID))
			cli.Printer.PrintLine(fmt.Sprintf("Owner:   %s", row.Owner))
			cli.Printer.PrintLine(fmt.Sprintf("Board:   %s", row.Board))
			cli.Printer.PrintLine(fmt.Sprintf("Table:   %s", row.Table))
			cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", row.UpdatedAt.Format("2006-01-02 15:04")))
			if row.Description != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Description: %s", row.Description))
			}
			cli.Printer.PrintLine("")
			cli.Printer.PrintLine("Columns:")
			for _, col := range row.Columns {
				label := col.ColumnID
				value := formatColumnValue(td, col)
				if td != nil {
					label = td.ResolveColumnLabel(col.ColumnID)
				}
				cli.Printer.PrintLine(fmt.Sprintf("  %s: %s", label, value))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	return cmd
}

// ── rows create ──────────────────────────────────────────────────────────────

func newRowsCreateCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable, flagData string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new row",
		Long: `Create a row in a table. Provide column data via --data (JSON) or stdin.

Example:
  copera rows create --board <id> --table <id> --data '{"columns":[{"columnId":"abc","value":"test"}]}'
  echo '{"columns":[...]}' | copera rows create --board <id> --table <id>`,
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

			data := flagData
			if data == "" {
				raw, readErr := io.ReadAll(cli.Stdin)
				if readErr != nil || len(raw) == 0 {
					cli.Printer.PrintError("input_error", "no row data provided",
						"Use --data '{...}' or pipe JSON via stdin", false)
					return exitcodes.New(exitcodes.Usage, fmt.Errorf("no row data"))
				}
				data = string(raw)
			}

			var input api.CreateRowInput
			if err := json.Unmarshal([]byte(data), &input); err != nil {
				cli.Printer.PrintError("input_error", fmt.Sprintf("invalid JSON: %s", err),
					"Provide valid JSON with a 'columns' array", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			row, err := client.RowCreate(context.Background(), boardID, tableID, &input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(row)
			}

			cli.Printer.PrintLine(fmt.Sprintf("Created row %s (row# %d)", row.ID, row.RowID))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().StringVar(&flagData, "data", "", "Row data as JSON")
	return cmd
}

// ── rows update ─────────────────────────────────────────────────────────────

func newRowsUpdateCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable, flagData string

	cmd := &cobra.Command{
		Use:   "update <row-id>",
		Short: "Update a row's column values",
		Long: `Update column values of an existing row. Provide column data via --data (JSON) or stdin.

Example:
  copera rows update <id> --board <id> --table <id> --data '{"columns":[{"columnId":"abc","value":"new"}]}'
  echo '{"columns":[...]}' | copera rows update <id> --board <id> --table <id>`,
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

			data := flagData
			if data == "" {
				raw, readErr := io.ReadAll(cli.Stdin)
				if readErr != nil || len(raw) == 0 {
					cli.Printer.PrintError("input_error", "no row data provided",
						"Use --data '{...}' or pipe JSON via stdin", false)
					return exitcodes.New(exitcodes.Usage, fmt.Errorf("no row data"))
				}
				data = string(raw)
			}

			var input api.UpdateRowInput
			if err := json.Unmarshal([]byte(data), &input); err != nil {
				cli.Printer.PrintError("input_error", fmt.Sprintf("invalid JSON: %s", err),
					"Provide valid JSON with a 'columns' array", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			row, err := client.RowUpdate(context.Background(), boardID, tableID, args[0], &input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(row)
			}

			cli.Printer.PrintLine(fmt.Sprintf("Updated row %s (row# %d)", row.ID, row.RowID))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().StringVar(&flagData, "data", "", "Row data as JSON")
	return cmd
}

// ── rows update-description ─────────────────────────────────────────────────

func newRowsUpdateDescriptionCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable string
	var flagOperation, flagContent string

	cmd := &cobra.Command{
		Use:   "update-description <row-id>",
		Short: "Update a row's description content",
		Long: `Update a row's markdown description content.

Content is read from --content or stdin. The update is processed asynchronously
(the server returns 202 Accepted immediately).

Operations:
  replace  — replace entire description (default)
  append   — add to the end
  prepend  — add to the beginning`,
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

			content := flagContent
			if content == "" {
				content, err = readStdinContent(cli)
				if err != nil {
					cli.Printer.PrintError("input_error", err.Error(), "Pipe content via stdin or use --content", false)
					return exitcodes.New(exitcodes.Usage, err)
				}
			}

			if err := client.RowUpdateDescription(context.Background(), boardID, tableID, args[0], flagOperation, content); err != nil {
				return apiError(cli, err)
			}

			cli.Printer.Info("Description update queued (operation: %s). Processing is async.", flagOperation)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().StringVar(&flagOperation, "operation", "replace", "Update operation: replace|append|prepend")
	cmd.Flags().StringVar(&flagContent, "content", "", "Content text (reads stdin if not set)")
	return cmd
}

// ── rows delete ─────────────────────────────────────────────────────────────

func newRowsDeleteCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable string
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "delete <row-id>",
		Short: "Delete a row",
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

			tableID, err := resolveID(nil, flagTable, cfg.TableID, "table ID (--table or config table_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --table <id> or set table_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if !flagForce && !cli.IsNonInteractive() {
				fmt.Fprintf(cli.Printer.Out, "Delete row %s? [y/N]: ", args[0])
				var ans string
				fmt.Fscanln(cli.Stdin, &ans)
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
					cli.Printer.Info("Aborted.")
					return nil
				}
			}

			if err := client.RowDelete(context.Background(), boardID, tableID, args[0]); err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"deleted": true,
					"rowId":   args[0],
				})
			}

			cli.Printer.Info("Row deleted.")
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

// ── rows authenticate ───────────────────────────────────────────────────────

func newRowsAuthenticateCmd(cli *CLI) *cobra.Command {
	var flagBoard, flagTable string
	var flagIdentCol, flagIdentVal, flagPassCol, flagPassVal string

	cmd := &cobra.Command{
		Use:   "authenticate",
		Short: "Authenticate a row using identifier and password columns",
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

			if flagIdentCol == "" || flagIdentVal == "" || flagPassCol == "" || flagPassVal == "" {
				cli.Printer.PrintError("missing_input", "all authentication flags are required",
					"Use --identifier-column, --identifier-value, --password-column, --password-value", false)
				return exitcodes.Newf(exitcodes.Usage, "missing required authentication flags")
			}

			row, err := client.RowAuthenticate(context.Background(), boardID, tableID, &api.AuthenticateRowInput{
				IdentifierColumnID:    flagIdentCol,
				IdentifierColumnValue: flagIdentVal,
				PasswordColumnID:      flagPassCol,
				PasswordColumnValue:   flagPassVal,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(row)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", row.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Row#:    %d", row.RowID))
			cli.Printer.PrintLine(fmt.Sprintf("Owner:   %s", row.Owner))
			cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", row.UpdatedAt.Format("2006-01-02 15:04")))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagBoard, "board", "", "Board ID")
	cmd.Flags().StringVar(&flagTable, "table", "", "Table ID")
	cmd.Flags().StringVar(&flagIdentCol, "identifier-column", "", "Identifier column ID")
	cmd.Flags().StringVar(&flagIdentVal, "identifier-value", "", "Identifier column value")
	cmd.Flags().StringVar(&flagPassCol, "password-column", "", "Password column ID")
	cmd.Flags().StringVar(&flagPassVal, "password-value", "", "Password column value")
	return cmd
}

// formatColumnValue returns a display string for a row column value.
// For LINK columns it uses linkValue (the resolved display names from the linked table).
// For option-based columns it resolves option IDs to labels via the table cache.
func formatColumnValue(td *cache.TableData, col api.RowColumn) string {
	// LINK columns: linkValue holds the display strings
	if td != nil {
		if tc, ok := td.Columns[col.ColumnID]; ok && tc.Type == "LINK" {
			return formatLinkValue(col.LinkValue)
		}
	}
	// For option-based columns, resolve via cache
	if td != nil {
		return td.ResolveOptionLabel(col.ColumnID, col.Value)
	}
	return fmt.Sprintf("%v", col.Value)
}

func formatLinkValue(linkValue any) string {
	switch v := linkValue.(type) {
	case []any:
		if len(v) == 0 {
			return "(empty)"
		}
		items := make([]string, 0, len(v))
		for _, item := range v {
			items = append(items, fmt.Sprintf("%v", item))
		}
		return strings.Join(items, ", ")
	case nil:
		return "(empty)"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// fetchTableData loads table schema from cache or API. Returns nil on failure (non-fatal).
func fetchTableData(cli *CLI, client *api.Client, cfg *config.Config, boardID, tableID string) *cache.TableData {
	tc := newTableCache(cli, cfg)

	if td, ok := tc.Get(tableID); ok {
		return td
	}

	table, err := client.TableGet(context.Background(), boardID, tableID)
	if err != nil {
		return nil
	}

	td := &cache.TableData{
		TableID: tableID,
		Name:    table.Name,
		Columns: make(map[string]cache.TableColumn),
	}
	for _, col := range table.Columns {
		colEntry := cache.TableColumn{
			Label:   col.Label,
			Type:    col.Type,
			Options: make(map[string]string),
		}
		for _, opt := range col.Options {
			colEntry.Options[opt.OptionID] = opt.Label
		}
		td.Columns[col.ColumnID] = colEntry
	}

	tc.Set(td)
	return td
}
