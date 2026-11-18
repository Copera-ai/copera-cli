package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newDesignCheckCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "check [dir]",
		Short: "Build and render a local Design without saving a version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, state, files, err := openDesignForBuild(args)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			report, err := checkDesign(cmd.Context(), client, root, state, files, false)
			if err != nil {
				return designError(cli, err)
			}
			return printDesignBuild(cli, report, "")
		},
	}
}

func openDesignForBuild(args []string) (*os.Root, *designState, map[string]designFile, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	root, err := openDesignDir(dir, false)
	if err != nil {
		return nil, nil, nil, err
	}
	state, err := loadDesignState(root)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, err
	}
	files, err := scanDesignFiles(root)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, err
	}
	return root, state, files, nil
}

func designProject(state *designState) api.ArtifactProjectSettings {
	return api.ArtifactProjectSettings{
		Kind:         state.Kind,
		Runtime:      state.Runtime,
		Entrypoint:   state.Entrypoint,
		Dependencies: state.Dependencies,
		Presentation: state.Presentation,
		Capabilities: state.Capabilities,
	}
}

func designTextFiles(files map[string]designFile, base map[string]string) []api.ArtifactContentFile {
	text := make([]api.ArtifactContentFile, 0)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := files[name]
		if !file.Binary && base[name] != sha256Hex(file.Data) {
			text = append(text, api.ArtifactContentFile{Path: name, MediaType: file.MediaType, Content: string(file.Data)})
		}
	}
	return text
}

func designCanonicalAssetPath(name string, paths map[string]string) string {
	if canonical := paths[name]; canonical != "" {
		return canonical
	}
	return name
}

func designManifestForFiles(state *designState, files map[string]designFile) api.ArtifactManifest {
	manifest := state.Manifest
	manifest.Assets = []api.ArtifactAsset{}
	for _, asset := range state.Manifest.Assets {
		if file, ok := files[designLocalAssetPath(asset.Path, state.AssetPaths)]; ok {
			if !file.Binary {
				asset.MediaType = file.MediaType
				asset.ContentDigest = sha256Hex(file.Data)
			}
			manifest.Assets = append(manifest.Assets, asset)
		}
	}
	manifest.Documents = slices.DeleteFunc(slices.Clone(manifest.Documents), func(doc api.ArtifactDocument) bool {
		_, exists := files[doc.Path]
		return !exists
	})
	return manifest
}

func designLocalAssetPath(canonical string, paths map[string]string) string {
	for name, stored := range paths {
		if stored == canonical {
			return name
		}
	}
	return canonical
}

func checkDesign(ctx context.Context, client *api.Client, root *os.Root, state *designState, files map[string]designFile, force bool) (*designBuildReport, error) {
	input := api.ArtifactCheckInput{
		ArtifactID: state.ID, BaseVersionID: state.BaseVersionID,
		IncludeScreenshots: true, Files: designTextFiles(files, nil),
	}
	if state.ID == "" || force {
		settings := designProject(state)
		input.Project = &settings
	}
	manifest := designManifestForFiles(state, files)
	pinned := make(map[string]bool, len(manifest.Assets))
	manifest.Assets = slices.DeleteFunc(manifest.Assets, func(asset api.ArtifactAsset) bool {
		name := designLocalAssetPath(asset.Path, state.AssetPaths)
		file := files[name]
		if !file.Binary {
			return false
		}
		// An updated file must replace the old pin in the dry run.
		if asset.ContentDigest != sha256Hex(file.Data) {
			return true
		}
		pinned[name] = true
		return false
	})
	manifestChanged := force || len(manifest.Assets) != len(state.Manifest.Assets) || len(manifest.Documents) != len(state.Manifest.Documents)
	var binaries []api.ArtifactCheckFile
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := files[name]
		if !file.Binary || pinned[name] {
			continue
		}
		if asset := state.PendingAssets[name]; state.ID != "" && asset.matches(sha256Hex(file.Data)) {
			manifest.Assets = append(manifest.Assets, asset.ArtifactAsset)
			manifestChanged = true
			continue
		}
		binaries = append(binaries, api.ArtifactCheckFile{Path: name, MediaType: file.MediaType, Data: file.Data})
	}
	if state.ID != "" && manifestChanged {
		// Unlike push, check treats source files outside the folder as removed.
		shared := &manifest.SharedDesign
		for _, name := range []*string{&shared.GuidancePath, &shared.TokensPath, &shared.ComponentsPath, &shared.AssetsPath} {
			if _, exists := files[*name]; !exists {
				*name = ""
			}
		}
		input.AuthoringManifest = &manifest
	}

	result, err := client.ArtifactCheck(ctx, input, binaries...)
	if err != nil {
		return nil, err
	}
	report := &designBuildReport{
		State: "ready", ArtifactBuildFindings: result.ArtifactBuildFindings, Warnings: result.Warnings,
	}
	if !result.OK {
		report.State = "failed"
	}
	for _, shot := range result.Screenshots {
		name, err := saveDesignShot(root, shot.PageID, shot.Width, shot.Data)
		if err != nil {
			return nil, err
		}
		report.Screenshots = append(report.Screenshots, name)
	}
	return report, nil
}

type designBuildReport struct {
	State       string                `json:"state"`
	FailureCode string                `json:"failureCode,omitempty"`
	Warnings    []api.ArtifactWarning `json:"warnings,omitempty"`
	api.ArtifactBuildFindings
	Screenshots []string `json:"screenshots"`
}

func saveDesignShot(root *os.Root, page string, width int, data []byte) (string, error) {
	if page == "" || page == "." || page == ".." || strings.ContainsAny(page, "/\\") || width <= 0 {
		return "", fmt.Errorf("invalid screenshot page or width")
	}
	name := fmt.Sprintf(".copera/shots/%s-%d.webp", page, width)
	if err := writeDesignFile(root, name, data); err != nil {
		return "", err
	}
	return name, nil
}

func printDesignBuild(cli *CLI, report *designBuildReport, failureSuggestion string) error {
	if cli.Printer.IsJSON() {
		if err := cli.Printer.PrintJSON(report); err != nil {
			return err
		}
	} else {
		cli.Printer.PrintLine("Build: " + report.State)
		for _, diagnostic := range report.Diagnostics {
			message := fmt.Sprintf("%s: %s", diagnostic.Severity, diagnostic.Message)
			if diagnostic.FilePath != "" {
				message = designSourceLocation(diagnostic.FilePath, diagnostic.Line, diagnostic.Column) + ": " + message
			}
			cli.Printer.PrintLine(message)
		}
		for _, entry := range report.RuntimeConsole.Entries {
			message := fmt.Sprintf("Runtime %s: %s", entry.Level, entry.Text)
			if entry.Source != nil {
				message = designSourceLocation(entry.Source.File, entry.Source.Line, entry.Source.Column) + ": " + message
			}
			cli.Printer.PrintLine(message)
		}
		for _, name := range report.Screenshots {
			cli.Printer.PrintLine(name)
		}
	}
	if report.State != "ready" {
		message := "Design build " + report.State
		if report.FailureCode != "" {
			message += " (" + report.FailureCode + ")"
		}
		cli.Printer.PrintError("build_failed", message, failureSuggestion, false)
		return exitcodes.New(exitcodes.Error, errors.New(message))
	}
	return nil
}

func designSourceLocation(name string, line, column int) string {
	if line > 0 {
		name += ":" + strconv.Itoa(line)
		if column > 0 {
			name += ":" + strconv.Itoa(column)
		}
	}
	return name
}

func newDesignScreenshotCmd(cli *CLI) *cobra.Command {
	var page, out string
	var width int
	cmd := &cobra.Command{
		Use:   "screenshot [dir|id]",
		Short: "Download a screenshot from a Design build",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if width < 0 || (width == 0 && cmd.Flags().Changed("viewport")) {
				return designError(cli, fmt.Errorf("viewport width must be positive"))
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			id, versionID, defaultPage, err := designScreenshotVersion(ctx, client, args)
			if err != nil {
				return designError(cli, err)
			}
			build, err := client.ArtifactBuild(ctx, id, versionID, 0)
			if err != nil {
				return designError(cli, err)
			}
			if page == "" {
				page = defaultPage
			}
			selected, largest := "", 0
			for _, shot := range build.Screenshots {
				if (page != "" && page != shot.PageID) || (width != 0 && width != shot.Width) {
					continue
				}
				if shot.Width > largest {
					selected, largest = shot.URL, shot.Width
				}
			}
			if selected == "" {
				return designError(cli, fmt.Errorf("no matching screenshot in the %s build", build.State))
			}
			data, err := downloadHandoffAsset(ctx, client, selected)
			if err != nil {
				return designError(cli, fmt.Errorf("download Design screenshot: %w", err))
			}
			if err := os.WriteFile(out, data, 0o644); err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]string{"path": out})
			}
			cli.Printer.PrintLine(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&page, "page", "", "Page ID (defaults to the Design's default page)")
	cmd.Flags().IntVar(&width, "viewport", 0, "Viewport width in CSS pixels (defaults to the widest available)")
	cmd.Flags().StringVarP(&out, "out", "o", "", "Output image file")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

func designScreenshotVersion(ctx context.Context, client *api.Client, args []string) (id, version, page string, err error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
		if id, err := designID(dir); err == nil {
			artifact, err := client.ArtifactGet(ctx, id)
			if err != nil {
				return "", "", "", err
			}
			current := artifact.CurrentVersion
			return id, current.ID, current.AuthoringManifest.DefaultPageID, nil
		}
	}
	root, err := openDesignDir(dir, false)
	if err != nil {
		return "", "", "", err
	}
	defer root.Close()
	state, err := loadDesignState(root)
	if err != nil {
		return "", "", "", err
	}
	if state.ID == "" || state.BaseVersionID == "" {
		return "", "", "", fmt.Errorf("this folder has no saved Design version")
	}
	return state.ID, state.BaseVersionID, state.Manifest.DefaultPageID, nil
}
