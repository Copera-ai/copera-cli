package commands

import (
	"context"
	"fmt"

	"github.com/copera/copera-cli/internal/api"
	"github.com/spf13/cobra"
)

// ── bookings ──────────────────────────────────────────────────────────────────

func newBookingsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bookings",
		Short: "List and inspect bookings",
	}
	cmd.AddCommand(
		newBookingsListCmd(cli),
		newBookingsGetCmd(cli),
	)
	return cmd
}

func newBookingsListCmd(cli *CLI) *cobra.Command {
	var flagStatus, flagType, flagFrom, flagTo, flagCursor string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List bookings, newest first",
		Long: `List the workspace's bookings, newest first.

Filter by status, booking type, and start-date range. Pagination is keyset-based:
pass the previous response's nextCursor value back via --cursor.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.BookingList(context.Background(), api.BookingListOpts{
				Status:        flagStatus,
				BookingTypeID: flagType,
				From:          flagFrom,
				To:            flagTo,
				Limit:         flagLimit,
				Cursor:        flagCursor,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.Bookings) == 0 {
				cli.Printer.Info("No bookings found.")
				return nil
			}

			for i, b := range page.Bookings {
				if i > 0 {
					cli.Printer.PrintLine("")
				}
				printBooking(cli, &b)
			}

			if page.HasMore && page.NextCursor != nil {
				cli.Printer.Info("\nMore results available. Use --cursor %s", *page.NextCursor)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagStatus, "status", "", "Filter by status (PENDING, CONFIRMED, DECLINED, CANCELLED, COMPLETED)")
	cmd.Flags().StringVar(&flagType, "type", "", "Filter by booking type id")
	cmd.Flags().StringVar(&flagFrom, "from", "", "Only bookings starting at/after this ISO-8601 instant")
	cmd.Flags().StringVar(&flagTo, "to", "", "Only bookings starting at/before this ISO-8601 instant")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1-100)")
	cmd.Flags().StringVar(&flagCursor, "cursor", "", "Keyset cursor from a previous response's nextCursor")
	return cmd
}

func newBookingsGetCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "get <booking-id>",
		Short: "Get a single booking by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			b, err := client.BookingGet(context.Background(), args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(b)
			}

			printBooking(cli, b)
			return nil
		},
	}
}

func printBooking(cli *CLI, b *api.Booking) {
	cli.Printer.PrintLine(fmt.Sprintf("ID:        %s", b.ID))
	cli.Printer.PrintLine(fmt.Sprintf("Status:    %s", b.Status))
	title := b.BookingTypeTitle
	if title == "" {
		title = b.BookingTypeID
	}
	cli.Printer.PrintLine(fmt.Sprintf("Type:      %s", title))
	cli.Printer.PrintLine(fmt.Sprintf("Start:     %s", b.Start.Format("2006-01-02 15:04")))
	cli.Printer.PrintLine(fmt.Sprintf("End:       %s", b.End.Format("2006-01-02 15:04")))
	cli.Printer.PrintLine(fmt.Sprintf("Duration:  %d min", b.DurationMinutes))
	cli.Printer.PrintLine(fmt.Sprintf("Timezone:  %s", b.TimezoneAtBooking))
	cli.Printer.PrintLine(fmt.Sprintf("Booker:    %s <%s>", b.Booker.Name, b.Booker.Email))
	if b.Location.Type != "" {
		loc := b.Location.Type
		if b.Location.Label != "" {
			loc = fmt.Sprintf("%s (%s)", b.Location.Label, b.Location.Type)
		}
		cli.Printer.PrintLine(fmt.Sprintf("Location:  %s", loc))
	}
	if len(b.Hosts) > 0 {
		cli.Printer.PrintLine(fmt.Sprintf("Hosts:     %d", len(b.Hosts)))
	}
	if len(b.Guests) > 0 {
		cli.Printer.PrintLine(fmt.Sprintf("Guests:    %d", len(b.Guests)))
	}
}

// ── booking-types ─────────────────────────────────────────────────────────────

func newBookingTypesCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "booking-types",
		Short: "List booking types (event types)",
	}
	cmd.AddCommand(
		newBookingTypesListCmd(cli),
	)
	return cmd
}

func newBookingTypesListCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the workspace's booking types",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.BookingTypeList(context.Background())
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.BookingTypes) == 0 {
				cli.Printer.Info("No booking types found.")
				return nil
			}

			for i, t := range page.BookingTypes {
				if i > 0 {
					cli.Printer.PrintLine("")
				}
				cli.Printer.PrintLine(fmt.Sprintf("ID:        %s", t.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Title:     %s", t.Title))
				if t.Slug != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Slug:      %s", t.Slug))
				}
				cli.Printer.PrintLine(fmt.Sprintf("Kind:      %s", t.Kind))
				cli.Printer.PrintLine(fmt.Sprintf("Duration:  %d min", t.DurationMinutes))
				cli.Printer.PrintLine(fmt.Sprintf("Hidden:    %t", t.Hidden))
				cli.Printer.PrintLine(fmt.Sprintf("Active:    %t", t.Active))
			}
			return nil
		},
	}
}
