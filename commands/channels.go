package commands

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newChannelsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Manage channels and messaging",
	}

	messageCmd := &cobra.Command{
		Use:   "message",
		Short: "Channel message operations",
	}
	messageCmd.AddCommand(newChannelMessageSendCmd(cli))

	cmd.AddCommand(
		newChannelsListCmd(cli),
		messageCmd,
	)
	return cmd
}

func newChannelsListCmd(cli *CLI) *cobra.Command {
	var flagQuery, flagType, flagKind, flagParticipant string
	var flagLimit, flagOffset int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List channels visible to the token user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.ChannelList(context.Background(), &api.ChannelListOptions{
				Query:         flagQuery,
				Type:          flagType,
				Kind:          flagKind,
				ParticipantID: flagParticipant,
				Limit:         flagLimit,
				Offset:        flagOffset,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.Channels) == 0 {
				cli.Printer.Info("No channels found.")
				return nil
			}

			for i, ch := range page.Channels {
				cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", ch.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Name:    %s", ch.Name))
				cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", ch.Type))
				cli.Printer.PrintLine(fmt.Sprintf("Kind:    %s", ch.Kind))
				cli.Printer.PrintLine(fmt.Sprintf("Updated: %s", ch.UpdatedAt.Format("2006-01-02 15:04")))
				if i < len(page.Channels)-1 {
					cli.Printer.PrintLine("")
				}
			}
			if page.Total > len(page.Channels) {
				cli.Printer.Info("\nShowing %d of %d channels.", len(page.Channels), page.Total)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&flagQuery, "query", "", "Search by channel name, description, or participant")
	cmd.Flags().StringVar(&flagType, "type", "", "Filter by channel type")
	cmd.Flags().StringVar(&flagKind, "kind", "", "Filter by channel kind: group or dm")
	cmd.Flags().StringVar(&flagParticipant, "participant", "", "Filter by participant user or team ID")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1-200)")
	cmd.Flags().IntVar(&flagOffset, "offset", 0, "Pagination offset")
	return cmd
}

func newChannelMessageSendCmd(cli *CLI) *cobra.Command {
	var flagChannel string
	var flagName string
	var flagUser string

	cmd := &cobra.Command{
		Use:   "send [text]",
		Short: "Send a message to a channel",
		Long: `Send a text message to a channel or direct message a workspace user. Markdown is supported.

Message content can be provided as an argument or piped from stdin.

Examples:
  copera channels message send "Hello world" --channel <id>
  copera channels message send "Hello world" --user <user-id>
  echo "Deploy done" | copera channels message send --channel <id>
  copera channels message send --channel <id> < report.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			// Resolve message: args > stdin
			var message string
			if len(args) > 0 {
				message = strings.Join(args, " ")
			} else {
				raw, readErr := io.ReadAll(cli.Stdin)
				if readErr != nil || len(raw) == 0 {
					cli.Printer.PrintError("input_error", "no message provided",
						"Provide message as argument or pipe via stdin", false)
					return exitcodes.New(exitcodes.Usage, fmt.Errorf("no message"))
				}
				message = strings.TrimSpace(string(raw))
			}

			if message == "" {
				cli.Printer.PrintError("input_error", "message cannot be empty",
					"Provide a non-empty message", false)
				return exitcodes.New(exitcodes.Usage, fmt.Errorf("empty message"))
			}
			if len(message) > 10000 {
				cli.Printer.PrintError("input_error", "message cannot exceed 10000 characters",
					"Shorten the message and try again", false)
				return exitcodes.New(exitcodes.Usage, fmt.Errorf("message too long"))
			}

			if flagUser != "" {
				if flagChannel != "" {
					err := fmt.Errorf("--user and --channel cannot be used together")
					cli.Printer.PrintError("input_error", err.Error(),
						"Use --user for a direct message or --channel for a channel message", false)
					return exitcodes.New(exitcodes.Usage, err)
				}
				if flagName != "" {
					err := fmt.Errorf("--name is only supported with --channel")
					cli.Printer.PrintError("input_error", err.Error(),
						"Remove --name when sending a direct message", false)
					return exitcodes.New(exitcodes.Usage, err)
				}

				result, err := client.SendDirectMessage(context.Background(), &api.DirectMessageInput{
					UserID:  flagUser,
					Message: message,
				})
				if err != nil {
					return apiError(cli, err)
				}

				if cli.Printer.IsJSON() {
					return cli.Printer.PrintJSON(map[string]any{
						"sent":    true,
						"user":    flagUser,
						"channel": result.ChannelID,
						"queued":  result.Queued,
					})
				}

				cli.Printer.Info("Direct message queued for user %s.", flagUser)
				return nil
			}

			channelID, err := resolveID(nil, flagChannel, cfg.ChannelID, "channel ID (--channel or config channel_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --channel <id>, --user <user-id>, or set channel_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			input := &api.SendMessageInput{
				Message: message,
				Name:    flagName,
			}

			if err := client.SendMessage(context.Background(), channelID, input); err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"sent":    true,
					"channel": channelID,
				})
			}

			cli.Printer.Info("Message sent to channel %s.", channelID)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagChannel, "channel", "", "Channel ID")
	cmd.Flags().StringVar(&flagUser, "user", "", "Workspace user ID for direct message")
	cmd.Flags().StringVar(&flagName, "name", "", "Display name for the sender (optional)")
	return cmd
}
