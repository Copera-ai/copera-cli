package commands

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/spf13/cobra"
)

func newDesignLinkCmd(cli *CLI) *cobra.Command {
	var from, to int64
	var messages []string
	cmd := &cobra.Command{
		Use:   "link <design-id> <row|meeting|chat|doc|drive-file> <source-id> [table-id row-id]",
		Short: "Link selected work context for Design authors",
		Args:  publicCommandArgs(3, 5),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			target, err := sourceLinkTarget(cmd, args[1:], from, to, messages)
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			response, err := client.ArtifactLinkSource(cmd.Context(), id, target)
			if err != nil {
				return printPublicAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(response)
			}
			printDesignSource(cli, response.Source)
			return nil
		},
	}
	cmd.Flags().Int64Var(&from, "from-ms", 0, "Meeting excerpt start in milliseconds, inclusive")
	cmd.Flags().Int64Var(&to, "to-ms", 0, "Meeting excerpt end in milliseconds, exclusive")
	cmd.Flags().StringArrayVar(&messages, "message-id", nil, "Selected channel message ID; repeat to select more")
	return cmd
}

func sourceLinkTarget(cmd *cobra.Command, args []string, from, to int64, messages []string) (api.ArtifactSourceTarget, error) {
	kind := args[0]
	hasFrom, hasTo := cmd.Flags().Changed("from-ms"), cmd.Flags().Changed("to-ms")
	if (kind != "meeting" && (hasFrom || hasTo)) || (kind != "chat" && len(messages) != 0) {
		return api.ArtifactSourceTarget{}, errors.New("use --from-ms and --to-ms only for meetings and --message-id only for chat")
	}
	expect := 2
	if kind == "row" {
		expect = 4
	}
	if len(args) != expect {
		return api.ArtifactSourceTarget{}, fmt.Errorf("%s requires %d source ID(s)", kind, expect-1)
	}
	ids := make([]string, 0, expect-1)
	for _, arg := range args[1:] {
		id, err := parsePublicObjectID(arg, "source")
		if err != nil {
			return api.ArtifactSourceTarget{}, err
		}
		ids = append(ids, id)
	}
	target := api.ArtifactSourceTarget{Kind: kind}
	switch kind {
	case "row":
		target.BoardID, target.TableID, target.RowID = ids[0], ids[1], ids[2]
	case "meeting":
		target.NoteID = ids[0]
		if hasFrom != hasTo || (hasFrom && (from < 0 || to <= from)) {
			return api.ArtifactSourceTarget{}, errors.New("provide both --from-ms and --to-ms with 0 <= start < end")
		}
		if hasFrom {
			target.Selection = &api.ArtifactSourceSelection{StartMs: &from, EndMs: &to}
		}
	case "chat":
		target.ChannelID = ids[0]
		if len(messages) == 0 {
			break
		}
		messageIDs := make([]string, 0, len(messages))
		for _, message := range messages {
			id, err := parsePublicObjectID(message, "message")
			if err != nil {
				return api.ArtifactSourceTarget{}, err
			}
			if !slices.Contains(messageIDs, id) {
				messageIDs = append(messageIDs, id)
			}
		}
		if len(messageIDs) > 20 {
			return api.ArtifactSourceTarget{}, errors.New("select at most 20 messages")
		}
		target.Selection = &api.ArtifactSourceSelection{MessageIDs: messageIDs}
	case "doc":
		target.DocID = ids[0]
	case "drive-file":
		target.Kind, target.FileID = "drive_file", ids[0]
	default:
		return api.ArtifactSourceTarget{}, errors.New("source must be row, meeting, chat, doc, or drive-file")
	}
	return target, nil
}

func newDesignSourcesCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "sources <design-id>",
		Short: "List Design source links you can read",
		Args:  publicCommandArgs(1, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			response, err := client.ArtifactListSources(cmd.Context(), id)
			if err != nil {
				return printPublicAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(response)
			}
			for _, link := range response.Sources {
				printDesignSource(cli, link)
			}
			return nil
		},
	}
}

func printDesignSource(cli *CLI, link api.ArtifactSourceLink) {
	label := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, link.Label)
	cli.Printer.PrintLine(fmt.Sprintf("%s [%s] %s (%s)", link.ID, link.Target.Kind, label, link.Href))
}

func newDesignUnlinkCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <design-id> <source-link-id>",
		Short: "Remove a source link from a Design",
		Args:  publicCommandArgs(2, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			linkID, err := parsePublicObjectID(args[1], "source link")
			if err != nil {
				return publicInputError(cli, err.Error())
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			if err := client.ArtifactUnlinkSource(cmd.Context(), id, linkID); err != nil {
				return printPublicAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(struct {
					Removed bool `json:"removed"`
				}{Removed: true})
			}
			cli.Printer.PrintLine("Source link removed.")
			return nil
		},
	}
}
