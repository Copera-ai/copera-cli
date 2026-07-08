package commands

import (
	"bufio"
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

	participantsCmd := &cobra.Command{
		Use:   "participants",
		Short: "Manage channel participants",
	}
	participantsCmd.AddCommand(
		newChannelParticipantsAddCmd(cli),
		newChannelParticipantsRemoveCmd(cli),
	)

	dmCmd := &cobra.Command{
		Use:   "dm",
		Short: "Direct message operations",
	}
	dmCmd.AddCommand(newChannelDMSendCmd(cli))

	cmd.AddCommand(
		newChannelsListCmd(cli),
		newChannelsCreateCmd(cli),
		newChannelArchiveCmd(cli),
		newChannelUnarchiveCmd(cli),
		messageCmd,
		participantsCmd,
		dmCmd,
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

func newChannelsCreateCmd(cli *CLI) *cobra.Command {
	var flagName, flagDescription, flagType string
	var flagParticipants []string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new text group channel",
		Long: `Create a new text group channel. The token user becomes the creator and a participant.

The channel name can be provided as an argument or via --name.

Examples:
  copera channels create "launch-room"
  copera channels create --name "launch-room" --description "Q3 launch coordination"
  copera channels create "voice-room" --type voice --participant user1 --participant user2`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			name := flagName
			if len(args) > 0 {
				name = strings.TrimSpace(args[0])
			}
			if name == "" {
				cli.Printer.PrintError("input_error", "channel name is required",
					"Provide the name as an argument or via --name", false)
				return exitcodes.New(exitcodes.Usage, fmt.Errorf("missing channel name"))
			}

			input := &api.ChannelCreateInput{
				Name:               name,
				Description:        flagDescription,
				ParticipantUserIDs: flagParticipants,
				Type:               flagType,
			}

			created, err := client.ChannelCreate(context.Background(), input)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(created)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:           %s", created.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:         %s", created.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Participants: %s", strings.Join(created.ParticipantUserIDs, ", ")))
			cli.Printer.Info("Channel created.")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "Channel name (1-200 characters)")
	cmd.Flags().StringVar(&flagDescription, "description", "", "Channel description (optional)")
	cmd.Flags().StringVar(&flagType, "type", "", "Channel type: text or voice (default text)")
	cmd.Flags().StringArrayVar(&flagParticipants, "participant", nil, "Workspace user ID to add as participant (repeatable)")
	return cmd
}

func newChannelParticipantsAddCmd(cli *CLI) *cobra.Command {
	var flagChannel string
	var flagParticipants []string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add participants to a channel",
		Long: `Add workspace users (including hired AI agents) to a channel. Requires channel admin.

Examples:
  copera channels participants add --channel <id> --participant user1 --participant user2
  copera channels participants add --participant user1   # uses config channel_id`,
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

			if len(flagParticipants) == 0 {
				cli.Printer.PrintError("input_error", "at least one participant is required",
					"Provide one or more --participant <user-id> flags", false)
				return exitcodes.New(exitcodes.Usage, fmt.Errorf("no participants"))
			}

			result, err := client.ChannelAddParticipants(context.Background(), channelID, &api.ChannelParticipantsInput{
				ParticipantUserIDs: flagParticipants,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}

			cli.Printer.PrintLine(fmt.Sprintf("Channel:      %s", result.ChannelID))
			cli.Printer.PrintLine(fmt.Sprintf("Participants: %s", strings.Join(result.ParticipantUserIDs, ", ")))
			cli.Printer.Info("Participants added.")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagChannel, "channel", "", "Channel ID")
	cmd.Flags().StringArrayVar(&flagParticipants, "participant", nil, "Workspace user ID to add (repeatable, at least one)")
	return cmd
}

func newChannelParticipantsRemoveCmd(cli *CLI) *cobra.Command {
	var flagChannel string
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "remove <participant-user-id>",
		Short: "Remove a participant from a channel",
		Long: `Remove one participant from a channel. Requires channel admin.

Examples:
  copera channels participants remove user1 --channel <id>
  copera channels participants remove user1 --force`,
		Args: cobra.ExactArgs(1),
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

			participantID := args[0]

			if !flagForce && !cli.IsNonInteractive() {
				r := bufio.NewReader(cli.Stdin)
				fmt.Fprintf(cli.Printer.Out, "Remove participant %s from channel %s? [y/N]: ", participantID, channelID)
				ans, _ := r.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
					cli.Printer.Info("Aborted.")
					return nil
				}
			} else if !flagForce {
				cli.Printer.PrintError("confirmation_required", "use --force to remove without confirmation", "", false)
				return exitcodes.Newf(exitcodes.Usage, "use --force to remove without confirmation")
			}

			if _, err := client.ChannelRemoveParticipant(context.Background(), channelID, participantID); err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"removed":     true,
					"channel":     channelID,
					"participant": participantID,
				})
			}

			cli.Printer.Info("Participant %s removed from channel %s.", participantID, channelID)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagChannel, "channel", "", "Channel ID")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

func newChannelArchiveCmd(cli *CLI) *cobra.Command {
	var flagChannel string
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "archive [channel-id]",
		Short: "Archive a channel",
		Long: `Archive a channel. Requires channel owner or admin. Archived channels can be restored.

The channel ID can be provided as an argument, via --channel, or from config channel_id.

Examples:
  copera channels archive <id>
  copera channels archive --channel <id> --force`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			channelID, err := resolveID(args, flagChannel, cfg.ChannelID, "channel ID (argument, --channel, or config channel_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Provide the channel ID as an argument, --channel <id>, or set channel_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if !flagForce && !cli.IsNonInteractive() {
				r := bufio.NewReader(cli.Stdin)
				fmt.Fprintf(cli.Printer.Out, "Archive channel %s? [y/N]: ", channelID)
				ans, _ := r.ReadString('\n')
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
					cli.Printer.Info("Aborted.")
					return nil
				}
			} else if !flagForce {
				cli.Printer.PrintError("confirmation_required", "use --force to archive without confirmation", "", false)
				return exitcodes.Newf(exitcodes.Usage, "use --force to archive without confirmation")
			}

			if _, err := client.ChannelArchive(context.Background(), channelID); err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"archived": true,
					"channel":  channelID,
				})
			}

			cli.Printer.Info("Channel %s archived.", channelID)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagChannel, "channel", "", "Channel ID")
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

func newChannelUnarchiveCmd(cli *CLI) *cobra.Command {
	var flagChannel string

	cmd := &cobra.Command{
		Use:   "unarchive [channel-id]",
		Short: "Restore an archived channel",
		Long: `Restore an archived channel. Requires channel owner or admin.

The channel ID can be provided as an argument, via --channel, or from config channel_id.

Examples:
  copera channels unarchive <id>
  copera channels unarchive --channel <id>`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			channelID, err := resolveID(args, flagChannel, cfg.ChannelID, "channel ID (argument, --channel, or config channel_id)")
			if err != nil {
				cli.Printer.PrintError("missing_id", err.Error(),
					"Provide the channel ID as an argument, --channel <id>, or set channel_id in your profile config", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			if _, err := client.ChannelUnarchive(context.Background(), channelID); err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"unarchived": true,
					"channel":    channelID,
				})
			}

			cli.Printer.Info("Channel %s unarchived.", channelID)
			return nil
		},
	}

	cmd.Flags().StringVar(&flagChannel, "channel", "", "Channel ID")
	return cmd
}

func newChannelDMSendCmd(cli *CLI) *cobra.Command {
	var flagUser string

	cmd := &cobra.Command{
		Use:   "send [text]",
		Short: "Send a direct message to a workspace user",
		Long: `Send a text direct message to a workspace user by user ID. Creates or reuses the DM channel.

Message content can be provided as an argument or piped from stdin.

Examples:
  copera channels dm send "Hello there" --user <user-id>
  echo "Deploy done" | copera channels dm send --user <user-id>`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			if flagUser == "" {
				cli.Printer.PrintError("input_error", "--user is required",
					"Provide the recipient's workspace user ID via --user", false)
				return exitcodes.New(exitcodes.Usage, fmt.Errorf("missing user"))
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
		},
	}

	cmd.Flags().StringVar(&flagUser, "user", "", "Workspace user ID (required)")
	return cmd
}
