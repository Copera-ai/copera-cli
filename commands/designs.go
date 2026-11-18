package commands

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/auth"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func designError(cli *CLI, err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		cli.Printer.PrintError("api_error", apiErr.Error(),
			"Check that your personal access token has Designs permission and that Designs are enabled in this workspace", false)
		return exitcodes.New(apiErr.ExitCode(), err)
	}
	return apiError(cli, err)
}

func designID(value string) (string, error) {
	id := value
	if u, err := url.Parse(value); err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
		id = path.Base(u.Path)
	}
	if len(id) == 24 {
		if _, err := hex.DecodeString(id); err == nil {
			return strings.ToLower(id), nil
		}
	}
	return "", fmt.Errorf("expected a Design ID or a Copera URL ending in a Design ID")
}

func designTool() string {
	if _, ok := os.LookupEnv("CLAUDECODE"); ok {
		return "claude_code"
	}
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "CODEX_") {
			return "codex"
		}
	}
	return "copera_cli"
}

func designVersion(ctx context.Context, client *api.Client, artifact *api.Artifact, number int) (*api.ArtifactVersion, error) {
	if number == 0 || artifact.CurrentVersion.Version == number {
		return &artifact.CurrentVersion, nil
	}
	var after string
	for {
		page, err := client.ArtifactVersions(ctx, artifact.ID, api.ArtifactPageOpts{After: after, Limit: 50})
		if err != nil {
			return nil, err
		}
		for _, edge := range page.Edges {
			if edge.Node.Version == number {
				return client.ArtifactVersionGet(ctx, artifact.ID, edge.Node.ID)
			}
		}
		if !page.HasNextPage {
			return nil, fmt.Errorf("Design %s has no version %d", artifact.ID, number)
		}
		if page.EndCursor == "" || page.EndCursor == after {
			return nil, fmt.Errorf("version pagination did not advance")
		}
		after = page.EndCursor
	}
}

func newDesignOpenCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "open <id|url>",
		Short: "Open a Design in Copera",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			pack, err := client.ArtifactHandoff(cmd.Context(), id, api.ArtifactHandoffOpts{})
			if err != nil {
				return designError(cli, err)
			}
			link := pack.Artifact.URL
			u, err := url.Parse(link)
			if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return designError(cli, fmt.Errorf("the API did not return a valid Design URL"))
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(struct {
					URL string `json:"url"`
				}{URL: link})
			}
			cli.Printer.PrintLine(link)
			if cli.Printer.IsTable() && !cli.IsNonInteractive() {
				if err := auth.OpenURL(link); err != nil {
					cli.Printer.Info("Could not open the browser: %s", err)
				}
			}
			return nil
		},
	}
}

type designLogEntry struct {
	Version       int       `json:"version"`
	Date          time.Time `json:"date"`
	Origin        *string   `json:"origin"`
	Tool          *string   `json:"tool"`
	ChangeSummary *string   `json:"changeSummary"`
}

func newDesignLogCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "log <id|url>",
		Short: "Show a Design's version history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			entries := []designLogEntry{}
			var after string
			for {
				result, err := client.ArtifactVersions(cmd.Context(), id, api.ArtifactPageOpts{After: after, Limit: 50})
				if err != nil {
					return designError(cli, err)
				}
				for _, edge := range result.Edges {
					v := edge.Node
					// The version list does not currently expose origin or change summary.
					entries = append(entries, designLogEntry{Version: v.Version, Date: v.CreatedAt, Tool: v.Tool})
				}
				if !result.HasNextPage {
					break
				}
				if result.EndCursor == "" || result.EndCursor == after {
					return designError(cli, fmt.Errorf("version pagination did not advance"))
				}
				after = result.EndCursor
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(entries)
			}
			for i, entry := range entries {
				if i > 0 {
					cli.Printer.PrintLine("")
				}
				tool := "unavailable"
				if entry.Tool != nil {
					tool = *entry.Tool
				}
				cli.Printer.PrintLine(fmt.Sprintf("Version: %d", entry.Version))
				cli.Printer.PrintLine(fmt.Sprintf("Date: %s", entry.Date.Format(time.RFC3339)))
				cli.Printer.PrintLine("Origin: unavailable")
				cli.Printer.PrintLine(fmt.Sprintf("Tool: %s", tool))
				cli.Printer.PrintLine("Change summary: unavailable")
			}
			return nil
		},
	}
}

func newDesignGuideCmd(cli *CLI) *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "guide",
		Short: "Print the Copera Design authoring guide",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			guide, err := client.ArtifactGuide(cmd.Context(), kind, "")
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(guide)
			}
			if _, err := io.WriteString(cli.Printer.Out, guide.Markdown); err != nil {
				return exitcodes.New(exitcodes.Error, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Design kind")
	return cmd
}
