package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newDesignsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "designs",
		Aliases: []string{"artifacts"},
		Short:   "Work with Copera Designs",
	}
	cmd.AddCommand(
		newArtifactsListCmd(cli),
		newArtifactsGetCmd(cli),
		newArtifactsVersionsCmd(cli),
		newArtifactsSourceCmd(cli),
		newArtifactsDesignCmd(cli),
		newArtifactsStartersCmd(cli),
		newArtifactsHandoffCmd(cli),
		newDesignOpenCmd(cli),
		newDesignLogCmd(cli),
		newDesignGuideCmd(cli),
		newDesignInitCmd(cli),
		newDesignPullCmd(cli),
		newDesignStatusCmd(cli),
		newDesignDiffCmd(cli),
		newDesignCheckCmd(cli),
		newDesignPushCmd(cli),
		newDesignScreenshotCmd(cli),
		newDesignAssetsCmd(cli),
		newDesignExportCmd(cli),
		newDesignImportCmd(cli),
		newDesignRenameCmd(cli),
		newDesignTrashCmd(cli),
		newDesignRestoreCmd(cli),
		newDesignForkCmd(cli),
		newDesignCommentsCmd(cli),
		newDesignWatchCmd(cli),
		newDesignAskCmd(cli),
	)
	return cmd
}

// artifactAPIError is apiError plus a hint for the most common artifacts
// failure: a 403 from a token without Drive access or a workspace without
// Artifacts enabled.
func artifactAPIError(cli *CLI, err error) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		cli.Printer.PrintError("api_error", apiErr.Error(),
			"Artifacts need a personal access token with Drive access, and Artifacts enabled in this workspace", false)
		return exitcodes.New(apiErr.ExitCode(), err)
	}
	return apiError(cli, err)
}

// resolveArtifactVersion returns the version to read: the --version flag when
// set, otherwise the artifact's current version.
func resolveArtifactVersion(ctx context.Context, client *api.Client, artifactID, versionID string) (*api.ArtifactVersion, error) {
	if versionID != "" {
		return client.ArtifactVersionGet(ctx, artifactID, versionID)
	}
	artifact, err := client.ArtifactGet(ctx, artifactID)
	if err != nil {
		return nil, err
	}
	return &artifact.CurrentVersion, nil
}

func validatePageLimit(cli *CLI, limit int) error {
	if limit != 0 && (limit < 1 || limit > 50) {
		cli.Printer.PrintError("invalid_flag", "limit must be between 1 and 50", "", false)
		return exitcodes.Newf(exitcodes.Usage, "limit must be between 1 and 50")
	}
	return nil
}

// ── artifacts list ───────────────────────────────────────────────────────────

func newArtifactsListCmd(cli *CLI) *cobra.Command {
	var flagScope, flagSearch, flagAfter string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts you can read",
		Long: `List artifacts you own (--scope mine, the default) or that are shared with
you (--scope shared). Results are paginated: pass the printed cursor to --after
for the next page.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagScope != "" && flagScope != "mine" && flagScope != "shared" {
				cli.Printer.PrintError("invalid_flag", "scope must be mine or shared", "", false)
				return exitcodes.Newf(exitcodes.Usage, "scope must be mine or shared")
			}
			if err := validatePageLimit(cli, flagLimit); err != nil {
				return err
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			result, err := client.ArtifactsList(context.Background(), api.ArtifactListOpts{
				Scope:  flagScope,
				Search: flagSearch,
				After:  flagAfter,
				Limit:  flagLimit,
			})
			if err != nil {
				return artifactAPIError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			if len(result.Edges) == 0 {
				cli.Printer.Info("No artifacts found.")
				return nil
			}
			for i, edge := range result.Edges {
				a := edge.Node
				cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", a.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Title:    %s", a.Title))
				cli.Printer.PrintLine(fmt.Sprintf("Version:  %d (%s)", a.CurrentVersion.Version, a.CurrentVersion.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Pages:    %d", len(a.CurrentVersion.AuthoringManifest.Pages)))
				cli.Printer.PrintLine(fmt.Sprintf("Updated:  %s", a.CurrentVersion.CreatedAt.Format("2006-01-02 15:04:05")))
				if i < len(result.Edges)-1 {
					cli.Printer.PrintLine("")
				}
			}
			if result.HasNextPage && result.EndCursor != "" {
				cli.Printer.Info("\nMore results: --after %s", result.EndCursor)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagScope, "scope", "", "Which artifacts: mine|shared (default: mine)")
	cmd.Flags().StringVar(&flagSearch, "search", "", "Filter by title")
	cmd.Flags().StringVar(&flagAfter, "after", "", "Cursor from the previous page")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results, 1-50 (default: server default 20)")
	return cmd
}

// ── artifacts get ────────────────────────────────────────────────────────────

func newArtifactsGetCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "get <artifactId>",
		Short: "Get artifact metadata and its current source inventory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			a, err := client.ArtifactGet(context.Background(), args[0])
			if err != nil {
				return artifactAPIError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(a)
			}
			v := a.CurrentVersion
			cli.Printer.PrintLine(fmt.Sprintf("ID:          %s", a.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Title:       %s", a.Title))
			if a.Description != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Description: %s", a.Description))
			}
			cli.Printer.PrintLine(fmt.Sprintf("Version:     %d (%s)", v.Version, v.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Runtime:     %s", v.Runtime))
			cli.Printer.PrintLine(fmt.Sprintf("Entrypoint:  %s", v.Entrypoint))
			cli.Printer.PrintLine(fmt.Sprintf("Can edit:    %t", a.CanEdit))
			cli.Printer.PrintLine(fmt.Sprintf("Created:     %s", v.CreatedAt.Format("2006-01-02 15:04:05")))
			if a.Preview.URL != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Preview:     %s", a.Preview.URL))
			}
			if pages := v.AuthoringManifest.Pages; len(pages) > 0 {
				cli.Printer.PrintLine("Pages:")
				for _, p := range pages {
					cli.Printer.PrintLine(fmt.Sprintf("  %s  %s  (%s, %s)", p.ID, p.Title, p.Surface, p.Entrypoint))
				}
			}
			if len(v.Files) > 0 {
				cli.Printer.PrintLine("Files:")
				for _, f := range v.Files {
					cli.Printer.PrintLine(fmt.Sprintf("  %s  (%s)", f.Path, humanSize(f.UTF8Bytes)))
				}
			}
			return nil
		},
	}
}

// ── artifacts versions ───────────────────────────────────────────────────────

func newArtifactsVersionsCmd(cli *CLI) *cobra.Command {
	var flagAfter string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "versions <artifactId>",
		Short: "List an artifact's immutable versions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePageLimit(cli, flagLimit); err != nil {
				return err
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactVersions(context.Background(), args[0], api.ArtifactPageOpts{
				After: flagAfter,
				Limit: flagLimit,
			})
			if err != nil {
				return artifactAPIError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			if len(result.Edges) == 0 {
				cli.Printer.Info("No versions found.")
				return nil
			}
			for i, edge := range result.Edges {
				v := edge.Node
				cli.Printer.PrintLine(fmt.Sprintf("Version:  %d", v.Version))
				cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", v.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Created:  %s", v.CreatedAt.Format("2006-01-02 15:04:05")))
				cli.Printer.PrintLine(fmt.Sprintf("Pages:    %d", len(v.AuthoringManifest.Pages)))
				cli.Printer.PrintLine(fmt.Sprintf("Files:    %d", len(v.Files)))
				if i < len(result.Edges)-1 {
					cli.Printer.PrintLine("")
				}
			}
			if result.HasNextPage && result.EndCursor != "" {
				cli.Printer.Info("\nMore results: --after %s", result.EndCursor)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagAfter, "after", "", "Cursor from the previous page")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results, 1-50 (default: server default 20)")
	return cmd
}

// ── artifacts source ─────────────────────────────────────────────────────────

// artifactSourceOutput is the JSON shape of a whole-file source read.
type artifactSourceOutput struct {
	ArtifactID    string `json:"artifactId"`
	VersionID     string `json:"versionId"`
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ContentDigest string `json:"contentDigest"`
	ByteLength    int    `json:"byteLength"`
	Text          string `json:"text"`
}

func newArtifactsSourceCmd(cli *CLI) *cobra.Command {
	var flagVersion, flagPath string

	cmd := &cobra.Command{
		Use:   "source <artifactId>",
		Short: "Print one source file of a version",
		Long: `Print a whole source file of an artifact version. The file is read in
bounded pages and followed until the end, so large files come back complete.

--path defaults to the version's entrypoint; --version defaults to the
artifact's current version. Human/plain output prints the file text exactly as
stored; JSON output wraps it with its path, media type and digest.

  copera artifacts source <id> --path src/App.tsx --output plain > App.tsx`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := context.Background()
			artifactID := args[0]

			versionID, filePath := flagVersion, flagPath
			if versionID == "" || filePath == "" {
				v, err := resolveArtifactVersion(ctx, client, artifactID, flagVersion)
				if err != nil {
					return artifactAPIError(cli, err)
				}
				versionID = v.ID
				if filePath == "" {
					filePath = v.Entrypoint
				}
			}

			page, text, err := client.ArtifactSourceFile(ctx, artifactID, versionID, filePath)
			if err != nil {
				return artifactAPIError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(artifactSourceOutput{
					ArtifactID:    page.ArtifactID,
					VersionID:     page.VersionID,
					Path:          page.Path,
					MediaType:     page.MediaType,
					ContentDigest: page.ContentDigest,
					ByteLength:    len(text),
					Text:          text,
				})
			}
			// Print the file byte-exact; only a terminal gets a closing newline.
			if text != "" && !strings.HasSuffix(text, "\n") && cli.Printer.IsTable() {
				text += "\n"
			}
			if _, err := io.WriteString(cli.Printer.Out, text); err != nil {
				return exitcodes.New(exitcodes.Error, fmt.Errorf("write output: %w", err))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagVersion, "version", "", "Version ID (default: the artifact's current version)")
	cmd.Flags().StringVar(&flagPath, "path", "", "File path inside the version (default: the entrypoint)")
	return cmd
}

// ── artifacts design ─────────────────────────────────────────────────────────

func newArtifactsDesignCmd(cli *CLI) *cobra.Command {
	var flagVersion string

	cmd := &cobra.Command{
		Use:   "design <artifactId>",
		Short: "Export a version's shared design",
		Long: `Export the shared design (guidance, tokens, components, dependencies and
asset metadata) pinned to a version. --version defaults to the artifact's
current version. Use --json to get the design files' full content.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := context.Background()
			versionID := flagVersion
			if versionID == "" {
				v, err := resolveArtifactVersion(ctx, client, args[0], "")
				if err != nil {
					return artifactAPIError(cli, err)
				}
				versionID = v.ID
			}

			d, err := client.ArtifactDesign(ctx, args[0], versionID)
			if err != nil {
				return artifactAPIError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(d)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Version:  %s", d.SourceVersionID))
			cli.Printer.PrintLine(fmt.Sprintf("Digest:   %s", d.DesignDigest))
			cli.Printer.PrintLine(fmt.Sprintf("Runtime:  %s", d.Runtime))
			printDesignPaths(cli, d.SharedDesign)
			if len(d.Files) > 0 {
				cli.Printer.PrintLine("Files:")
				for _, f := range d.Files {
					cli.Printer.PrintLine(fmt.Sprintf("  %s  (%s)", f.Path, humanSize(int64(len(f.Content)))))
				}
			}
			if len(d.Dependencies) > 0 {
				cli.Printer.PrintLine("Dependencies:")
				for _, dep := range d.Dependencies {
					cli.Printer.PrintLine(fmt.Sprintf("  %s@%s", dep.Name, dep.Version))
				}
			}
			if len(d.Assets) > 0 {
				cli.Printer.PrintLine("Assets:")
				for _, a := range d.Assets {
					cli.Printer.PrintLine(fmt.Sprintf("  %s  (%s, %s)", a.Path, a.Usage, a.MediaType))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagVersion, "version", "", "Version ID (default: the artifact's current version)")
	return cmd
}

func printDesignPaths(cli *CLI, sd api.ArtifactSharedDesign) {
	for _, row := range [][2]string{
		{"Guidance", sd.GuidancePath},
		{"Tokens", sd.TokensPath},
		{"Components", sd.ComponentsPath},
		{"Assets dir", sd.AssetsPath},
	} {
		if row[1] != "" {
			cli.Printer.PrintLine(fmt.Sprintf("%-10s %s", row[0]+":", row[1]))
		}
	}
}

// ── artifacts starters ───────────────────────────────────────────────────────

func newArtifactsStartersCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "starters",
		Short: "List curated design starters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			starters, err := client.ArtifactStarters(context.Background())
			if err != nil {
				return artifactAPIError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(struct {
					Starters []api.ArtifactStarter `json:"starters"`
				}{starters})
			}
			if len(starters) == 0 {
				cli.Printer.Info("No starters available.")
				return nil
			}
			for i, s := range starters {
				cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", s.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Title:    %s", s.Title))
				cli.Printer.PrintLine(fmt.Sprintf("Runtime:  %s", s.Runtime))
				cli.Printer.PrintLine(fmt.Sprintf("Files:    %d", len(s.Files)))
				if i < len(starters)-1 {
					cli.Printer.PrintLine("")
				}
			}
			return nil
		},
	}
}
