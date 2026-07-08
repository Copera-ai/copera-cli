package commands

import (
	"context"
	"fmt"

	"github.com/copera/copera-cli/internal/api"
	"github.com/spf13/cobra"
)

func newMeetingNotesCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "meeting-notes",
		Short: "List and read meeting notes",
	}
	cmd.AddCommand(
		newMeetingNotesListCmd(cli),
		newMeetingNotesGetCmd(cli),
	)
	return cmd
}

// ── meeting-notes list ────────────────────────────────────────────────────────

func newMeetingNotesListCmd(cli *CLI) *cobra.Command {
	var flagQuery, flagBefore string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List meeting notes, newest first",
		Long: `List meeting notes the token user owns or is a shared participant of, newest first.

Use --before with an ISO date to page back in time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.MeetingNoteList(context.Background(), api.MeetingNoteListOpts{
				Query:  flagQuery,
				Limit:  flagLimit,
				Before: flagBefore,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.Notes) == 0 {
				cli.Printer.Info("No meeting notes found.")
				return nil
			}

			for i, n := range page.Notes {
				if i > 0 {
					cli.Printer.PrintLine("")
				}
				cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", n.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Title:    %s", n.Title))
				cli.Printer.PrintLine(fmt.Sprintf("Status:   %s", n.Status))
				if n.StartedAt != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Started:  %s", n.StartedAt))
				}
				if n.EndedAt != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Ended:    %s", n.EndedAt))
				}
				cli.Printer.PrintLine(fmt.Sprintf("Summary:  %t", n.HasSummary))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagQuery, "query", "", "Search note titles and typed notes")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1-100)")
	cmd.Flags().StringVar(&flagBefore, "before", "", "Return notes started before this ISO date")
	return cmd
}

// ── meeting-notes get ─────────────────────────────────────────────────────────

func newMeetingNotesGetCmd(cli *CLI) *cobra.Command {
	var flagTranscript bool

	cmd := &cobra.Command{
		Use:   "get <note-id>",
		Short: "Get a meeting note's summary and optional transcript",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			note, err := client.MeetingNoteGet(context.Background(), args[0], flagTranscript)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(note)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", note.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Title:    %s", note.Title))
			cli.Printer.PrintLine(fmt.Sprintf("Status:   %s", note.Status))
			if note.StartedAt != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Started:  %s", note.StartedAt))
			}
			if note.EndedAt != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Ended:    %s", note.EndedAt))
			}
			if note.Summary != "" {
				cli.Printer.PrintLine("")
				cli.Printer.PrintLine("Summary:")
				cli.Printer.PrintLine(note.Summary)
			}
			if note.UserNotes != "" {
				cli.Printer.PrintLine("")
				cli.Printer.PrintLine("Notes:")
				cli.Printer.PrintLine(note.UserNotes)
			}
			if len(note.Transcript) > 0 {
				cli.Printer.PrintLine("")
				cli.Printer.PrintLine(fmt.Sprintf("Transcript (%d segments):", len(note.Transcript)))
				for _, seg := range note.Transcript {
					if seg.Speaker != "" {
						cli.Printer.PrintLine(fmt.Sprintf("[%s] %s", seg.Speaker, seg.Text))
					} else {
						cli.Printer.PrintLine(seg.Text)
					}
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&flagTranscript, "transcript", false, "Include the full transcript segments")
	return cmd
}
