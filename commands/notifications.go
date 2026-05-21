package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newNotificationsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "notifications",
		Short: "List, mark, and delete notifications",
	}
	cmd.AddCommand(
		newNotificationsListCmd(cli),
		newNotificationsReadCmd(cli),
		newNotificationsUnreadCmd(cli),
		newNotificationsDeleteCmd(cli),
	)
	return cmd
}

// ── notifications list ──────────────────────────────────────────────────────

func newNotificationsListCmd(cli *CLI) *cobra.Command {
	var flagAfter, flagBefore string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List notifications owned by the current PAT user",
		Long: `List notifications owned by the user behind the current PAT.

Pagination uses notification IDs as cursors:
  --after <id>   fetch notifications older than <id>
  --before <id>  fetch notifications newer than <id>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.NotificationList(context.Background(), flagAfter, flagBefore)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.Notifications) == 0 {
				cli.Printer.Info("No notifications.")
				return nil
			}

			for i, n := range page.Notifications {
				if i > 0 {
					cli.Printer.PrintLine("")
				}
				cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", n.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", n.Type))
				cli.Printer.PrintLine(fmt.Sprintf("Status:  %s", n.Status))
				if n.Sender != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Sender:  %s", n.Sender))
				}
				if !n.CreatedAt.IsZero() {
					cli.Printer.PrintLine(fmt.Sprintf("Created: %s", n.CreatedAt.Format("2006-01-02 15:04")))
				}
				if n.GroupCount > 1 {
					cli.Printer.PrintLine(fmt.Sprintf("Group:   %d", n.GroupCount))
				}
				if summary := summarizeNotificationData(n.Data); summary != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Data:    %s", summary))
				}
			}

			cli.Printer.Info("\n%d unread / %d total.", page.UnreadCount, page.Count)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagAfter, "after", "", "Cursor — fetch notifications older than this ID")
	cmd.Flags().StringVar(&flagBefore, "before", "", "Cursor — fetch notifications newer than this ID")
	return cmd
}

// ── notifications read ──────────────────────────────────────────────────────

func newNotificationsReadCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "read <notification-id>",
		Short: "Mark a notification as read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateNotificationStatus(cli, args[0], "read")
		},
	}
}

// ── notifications unread ────────────────────────────────────────────────────

func newNotificationsUnreadCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "unread <notification-id>",
		Short: "Mark a notification as unread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdateNotificationStatus(cli, args[0], "unread")
		},
	}
}

func runUpdateNotificationStatus(cli *CLI, id, status string) error {
	client, _, err := requireAPIClient(cli)
	if err != nil {
		return err
	}

	n, err := client.NotificationUpdateStatus(context.Background(), id, status)
	if err != nil {
		return apiError(cli, err)
	}

	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(n)
	}

	cli.Printer.Info("Notification %s marked as %s.", n.ID, n.Status)
	return nil
}

// ── notifications delete ────────────────────────────────────────────────────

func newNotificationsDeleteCmd(cli *CLI) *cobra.Command {
	var flagForce bool

	cmd := &cobra.Command{
		Use:   "delete <notification-id>",
		Short: "Delete a notification",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			if !flagForce && !cli.IsNonInteractive() {
				fmt.Fprintf(cli.Printer.Out, "Delete notification %s? [y/N]: ", args[0])
				var ans string
				fmt.Fscanln(cli.Stdin, &ans)
				if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(ans)), "y") {
					cli.Printer.Info("Aborted.")
					return nil
				}
			}

			if err := client.NotificationDelete(context.Background(), args[0]); err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"deleted":        true,
					"notificationId": args[0],
				})
			}

			cli.Printer.Info("Notification deleted.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagForce, "force", false, "Skip confirmation prompt")
	return cmd
}

// summarizeNotificationData picks a handful of well-known data fields and
// renders them as a compact key=value line. Falls back to listing keys when
// no preferred field is present.
func summarizeNotificationData(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	preferred := []string{"title", "name", "message", "content", "preview", "rowId", "docId", "channelId", "url"}
	parts := []string{}
	for _, k := range preferred {
		if v, ok := data[k]; ok {
			s := fmt.Sprintf("%v", v)
			parts = append(parts, fmt.Sprintf("%s=%s", k, truncate(s, 80)))
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "  ")
	}
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 4 {
		keys = keys[:4]
		keys = append(keys, "…")
	}
	return strings.Join(keys, ", ")
}
