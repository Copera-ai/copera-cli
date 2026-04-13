package commands

import (
	"context"
	"fmt"

	"github.com/copera/copera-cli/internal/api"
	"github.com/spf13/cobra"
)

func newSearchCmd(cli *CLI) *cobra.Command {
	var flagTypes []string
	var flagSortBy, flagSortOrder string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search across the workspace",
		Long: `Search documents, channels, messages, drive files, todos, and more.

Use --type to filter by entity type (repeatable):
  document, channel, channelMessage, voiceTranscription,
  driveContent, todo, todoItem, aiChat, aiChatMessage`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			result, err := client.GlobalSearch(context.Background(), args[0], api.GlobalSearchOpts{
				Types:     flagTypes,
				SortBy:    flagSortBy,
				SortOrder: flagSortOrder,
				Limit:     flagLimit,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}

			if len(result.Hits) == 0 {
				cli.Printer.Info("No results for %q.", args[0])
				return nil
			}

			for i, h := range result.Hits {
				cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", h.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", h.EntityType))
				cli.Printer.PrintLine(fmt.Sprintf("Title:   %s", h.DisplayTitle()))
				updated := h.UpdatedAtTime()
				if !updated.IsZero() {
					cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", updated.Format("2006-01-02 15:04")))
				}
				if i < len(result.Hits)-1 {
					cli.Printer.PrintLine("")
				}
			}

			if result.TotalHits > len(result.Hits) {
				cli.Printer.Info("\nShowing %d of %d results.", len(result.Hits), result.TotalHits)
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVar(&flagTypes, "type", nil, "Filter by entity type (repeatable)")
	cmd.Flags().StringVar(&flagSortBy, "sort", "", "Sort field: createdAt|updatedAt")
	cmd.Flags().StringVar(&flagSortOrder, "order", "", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1-100)")
	return cmd
}
