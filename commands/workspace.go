package commands

import (
	"context"
	"fmt"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newWorkspaceCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Workspace info, members, and teams",
	}
	cmd.AddCommand(
		newWorkspaceInfoCmd(cli),
		newWorkspaceMeCmd(cli),
		newWorkspaceMembersCmd(cli),
		newWorkspaceTeamsCmd(cli),
		newWorkspaceTeamCmd(cli),
	)
	return cmd
}

// ── workspace team ────────────────────────────────────────────────────────────

func newWorkspaceTeamCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Manage individual workspace teams",
	}
	cmd.AddCommand(newWorkspaceTeamCreateCmd(cli))
	return cmd
}

func newWorkspaceTeamCreateCmd(cli *CLI) *cobra.Command {
	var flagName string
	var flagParticipants []string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new team",
		Long: `Create a new workspace team.

The team name must be unique in the workspace. Optionally seed the team with
participant user IDs (24-char hex).

Example:
  copera workspace team create --name "Engineering"
  copera workspace team create --name "Design" --participant u001 --participant u002`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			if flagName == "" {
				cli.Printer.PrintError("missing_input", "team name is required",
					"Use --name <team-name>", false)
				return exitcodes.Newf(exitcodes.Usage, "team name is required")
			}

			team, err := client.WorkspaceTeamCreate(context.Background(), &api.CreateTeamInput{
				Name:               flagName,
				ParticipantUserIDs: flagParticipants,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(team)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", team.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:     %s", team.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Members:  %d", len(team.Participants)))
			if team.Main {
				cli.Printer.PrintLine("Main:     true")
			}
			cli.Printer.PrintLine(fmt.Sprintf("Created:  %s", team.CreatedAt.Format("2006-01-02 15:04")))
			return nil
		},
	}
	cmd.Flags().StringVar(&flagName, "name", "", "Team name (required)")
	cmd.Flags().StringSliceVar(&flagParticipants, "participant", nil, "Participant user ID (repeatable)")
	return cmd
}

// ── workspace me ──────────────────────────────────────────────────────────────

func newWorkspaceMeCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show the authenticated user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			me, err := client.Me(context.Background())
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(me)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", me.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:    %s", me.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Email:   %s", me.Email))
			if me.Title != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Title:   %s", me.Title))
			}
			cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", me.Type))
			cli.Printer.PrintLine(fmt.Sprintf("Status:  %s", me.Status))

			return nil
		},
	}
}

// ── workspace info ──────────────────────────────────────────────────────────

func newWorkspaceInfoCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show workspace details",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			ws, err := client.WorkspaceInfo(context.Background())
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(ws)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:          %s", ws.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:        %s", ws.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Slug:        %s", ws.Slug))
			if ws.Description != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Description: %s", ws.Description))
			}
			cli.Printer.PrintLine(fmt.Sprintf("Seats:       %d", ws.Seats))
			cli.Printer.PrintLine(fmt.Sprintf("Created:     %s", ws.CreatedAt.Format("2006-01-02 15:04")))
			cli.Printer.PrintLine(fmt.Sprintf("Updated:     %s", ws.UpdatedAt.Format("2006-01-02 15:04")))

			return nil
		},
	}
}

// ── workspace members ───────────────────────────────────────────────────────

func newWorkspaceMembersCmd(cli *CLI) *cobra.Command {
	var flagQuery string
	var flagLimit, flagOffset int

	cmd := &cobra.Command{
		Use:   "members",
		Short: "List workspace members",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.WorkspaceMembers(context.Background(), api.MemberListOpts{
				Query:  flagQuery,
				Limit:  flagLimit,
				Offset: flagOffset,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.Members) == 0 {
				cli.Printer.Info("No members found.")
				return nil
			}

			for i, m := range page.Members {
				cli.Printer.PrintLine(fmt.Sprintf("ID:      %s", m.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Name:    %s", m.Name))
				cli.Printer.PrintLine(fmt.Sprintf("Email:   %s", m.Email))
				if m.Title != "" {
					cli.Printer.PrintLine(fmt.Sprintf("Title:   %s", m.Title))
				}
				cli.Printer.PrintLine(fmt.Sprintf("Type:    %s", m.Type))
				cli.Printer.PrintLine(fmt.Sprintf("Status:  %s", m.Status))
				if !m.Active {
					cli.Printer.PrintLine("Active:  false")
				}
				cli.Printer.PrintLine(fmt.Sprintf("Joined:  %s", m.CreatedAt.Format("2006-01-02 15:04")))
				if i < len(page.Members)-1 {
					cli.Printer.PrintLine("")
				}
			}

			if page.Total > len(page.Members) {
				cli.Printer.Info("\nShowing %d of %d members.", len(page.Members), page.Total)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagQuery, "query", "", "Filter by name or email")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1-500)")
	cmd.Flags().IntVar(&flagOffset, "offset", 0, "Pagination offset")
	return cmd
}

// ── workspace teams ─────────────────────────────────────────────────────────

func newWorkspaceTeamsCmd(cli *CLI) *cobra.Command {
	var flagQuery string
	var flagLimit, flagOffset int

	cmd := &cobra.Command{
		Use:   "teams",
		Short: "List workspace teams",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			page, err := client.WorkspaceTeams(context.Background(), api.TeamListOpts{
				Query:  flagQuery,
				Limit:  flagLimit,
				Offset: flagOffset,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(page)
			}

			if len(page.Teams) == 0 {
				cli.Printer.Info("No teams found.")
				return nil
			}

			for i, t := range page.Teams {
				cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", t.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Name:     %s", t.Name))
				cli.Printer.PrintLine(fmt.Sprintf("Members:  %d", len(t.Participants)))
				if t.Main {
					cli.Printer.PrintLine("Main:     true")
				}
				cli.Printer.PrintLine(fmt.Sprintf("Created:  %s", t.CreatedAt.Format("2006-01-02 15:04")))
				cli.Printer.PrintLine(fmt.Sprintf("Updated:  %s", t.UpdatedAt.Format("2006-01-02 15:04")))
				if i < len(page.Teams)-1 {
					cli.Printer.PrintLine("")
				}
			}

			if page.Total > len(page.Teams) {
				cli.Printer.Info("\nShowing %d of %d teams.", len(page.Teams), page.Total)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagQuery, "query", "", "Filter by team name")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1-200)")
	cmd.Flags().IntVar(&flagOffset, "offset", 0, "Pagination offset")
	return cmd
}
