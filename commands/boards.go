package commands

import (
	"context"
	"fmt"

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
	return &cobra.Command{
		Use:   "list",
		Short: "List all boards",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			boards, err := client.BoardList(context.Background())
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
	)
	return cmd
}

func newTablesListCmd(cli *CLI) *cobra.Command {
	var flagBoard string

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

			tables, err := client.TableList(context.Background(), boardID)
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
