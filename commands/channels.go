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

	cmd.AddCommand(messageCmd)
	return cmd
}

func newChannelMessageSendCmd(cli *CLI) *cobra.Command {
	var flagChannel string
	var flagName string

	cmd := &cobra.Command{
		Use:   "send [text]",
		Short: "Send a message to a channel",
		Long: `Send a text message to a channel. Markdown is supported.

Message content can be provided as an argument or piped from stdin.

Examples:
  copera channels message send "Hello world" --channel <id>
  echo "Deploy done" | copera channels message send --channel <id>
  copera channels message send --channel <id> < report.md`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			channelID, err := resolveID(nil, flagChannel, cfg.ChannelID, "channel ID (--channel or config channel_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Use --channel <id> or set channel_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
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
	cmd.Flags().StringVar(&flagName, "name", "", "Display name for the sender (optional)")
	return cmd
}
