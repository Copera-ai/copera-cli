package commands

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/config"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newDesignCheckCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "check [dir]",
		Short: "Build and render a local Design without saving a version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, state, files, err := openDesignForBuild(cli, args)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			digest, err := designCheckDigest(cfg, state, files)
			if err != nil {
				return designError(cli, err)
			}
			report, err := checkDesign(cmd.Context(), cli, client, root, state, files, false)
			if err != nil {
				return designError(cli, err)
			}
			if err := recordDesignCheck(root, cfg, files, digest, report.State == "ready"); err != nil {
				cli.Printer.Info("Warning: save Design check result: %s", err)
			}
			designVendorHint(cli, files, state.DesignSystemFiles)(report)
			return printDesignBuild(cli, report, "")
		},
	}
}

type designCheckReceipt struct {
	Digest    string `json:"digest"`
	CheckedAt string `json:"checkedAt"`
}

func (receipt *designCheckReceipt) recent() bool {
	if receipt == nil {
		return false
	}
	checkedAt, err := time.Parse(time.RFC3339Nano, receipt.CheckedAt)
	age := time.Since(checkedAt)
	return err == nil && age >= 0 && age < 30*time.Minute
}

func designCheckDigest(cfg *config.Config, state *designState, files map[string]designFile) (string, error) {
	type fileFingerprint struct {
		Checksum  string
		MediaType string
		Binary    bool
	}
	fingerprints := make(map[string]fileFingerprint, len(files))
	snapshot := *state
	snapshot.LastCheck = nil
	snapshot.BuildState = ""
	snapshot.PendingAssets = nil
	for name, file := range files {
		checksum := file.checksum()
		fingerprints[name] = fileFingerprint{checksum, file.MediaType, file.Binary}
		if file.Binary {
			canonical := designCanonicalAssetPath(name, state.AssetPaths)
			if pending := state.PendingAssets[canonical]; pending.matches(checksum) {
				if snapshot.PendingAssets == nil {
					snapshot.PendingAssets = make(map[string]pendingDesignAsset)
				}
				snapshot.PendingAssets[canonical] = pending
			}
		}
	}
	// Scope the cache to the checked project settings, pins, base and API
	// credentials. Only the resulting digest is persisted.
	return designPushKey("design-check-v1", struct {
		Server string
		Token  string
		Design designState
		Files  map[string]fileFingerprint
	}{strings.TrimRight(cfg.API.BaseURL, "/"), cfg.Token, snapshot, fingerprints})
}

func recordDesignCheck(root *os.Root, cfg *config.Config, files map[string]designFile, digest string, passed bool) error {
	release, err := lockDesignState(root, false)
	if err != nil {
		return err
	}
	defer release()

	current, err := loadDesignState(root)
	if err != nil {
		return err
	}
	currentDigest, err := designCheckDigest(cfg, current, files)
	if err != nil || currentDigest != digest {
		return err
	}
	switch {
	case passed:
		current.LastCheck = &designCheckReceipt{Digest: digest, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	case current.LastCheck != nil && current.LastCheck.Digest == digest:
		current.LastCheck = nil
	default:
		return nil
	}
	return writeDesignStateLocked(root, current)
}

func openDesignForBuild(cli *CLI, args []string) (*os.Root, *designState, map[string]designFile, error) {
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
	files, err := scanDesignFiles(root, state)
	if err != nil {
		_ = root.Close()
		return nil, nil, nil, err
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		info, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			_ = root.Close()
			return nil, nil, nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if info.Size() == int64(len(designRules)) {
			content, err := readDesignFile(root, name)
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				_ = root.Close()
				return nil, nil, nil, err
			}
			if string(content) == designRules {
				continue
			}
		}
		cli.Printer.Info("%s is a local file of this folder and is not saved in the Design.", name)
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
		if !file.Binary && base[name] != file.checksum() {
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
				asset.ContentDigest = file.checksum()
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

func checkDesign(ctx context.Context, cli *CLI, client *api.Client, root *os.Root, state *designState, files map[string]designFile, force bool) (*designBuildReport, error) {
	input := api.ArtifactCheckInput{
		ArtifactID: state.ID, BaseVersionID: state.BaseVersionID,
		IncludeScreenshots: true, Files: designTextFiles(files, nil),
	}
	if state.ID == "" || force {
		settings := designProject(state)
		input.Project = &settings
	} else if state.Capabilities != nil {
		input.Project = &api.ArtifactProjectSettings{Capabilities: state.Capabilities}
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
		if asset.ContentDigest != file.checksum() {
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
		canonical := designCanonicalAssetPath(name, state.AssetPaths)
		if err := validateDesignContentPath(canonical); err != nil {
			return nil, err
		}
		if asset := state.PendingAssets[canonical]; state.ID != "" && asset.matches(file.checksum()) {
			manifest.Assets = append(manifest.Assets, asset.ArtifactAsset)
			manifestChanged = true
			continue
		}
		binaries = append(binaries, file.uploadPart(canonical))
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
		DarkScheme: result.DarkScheme, ScreenshotsOmittedPageIDs: result.ScreenshotsOmittedPageIDs,
	}
	if !result.OK {
		report.State = "failed"
	}
	for _, shot := range result.Screenshots {
		scheme := shot.Scheme
		if scheme == "" {
			scheme = "light"
		}
		name, err := saveDesignShot(root, shot.PageID, shot.Width, scheme, shot.Data)
		if err != nil {
			if !result.OK {
				return nil, err
			}
			cli.Printer.Info("Warning: save Design screenshot: %s", err)
			continue
		}
		report.addScreenshot(shot.PageID, shot.Route, name)
	}
	return report, nil
}

type designBuildReport struct {
	State       string                `json:"state"`
	FailureCode string                `json:"failureCode,omitempty"`
	Warnings    []api.ArtifactWarning `json:"warnings,omitempty"`
	Version     *api.ArtifactVersion  `json:"version,omitempty"`
	AppURL      string                `json:"appUrl,omitempty"`
	DarkScheme  *bool                 `json:"darkScheme,omitempty"`
	api.ArtifactBuildFindings
	Screenshots               []string               `json:"screenshots"`
	ScreenshotPages           []designScreenshotPage `json:"screenshotPages,omitempty"`
	ScreenshotsOmittedPageIDs []string               `json:"screenshotsOmittedPageIds,omitempty"`
}

type designScreenshotPage struct {
	PageID string   `json:"pageId"`
	Route  string   `json:"route,omitempty"`
	Paths  []string `json:"paths"`
}

func (report *designBuildReport) addScreenshot(pageID, route, name string) {
	if !slices.Contains(report.Screenshots, name) {
		report.Screenshots = append(report.Screenshots, name)
	}
	for i := range report.ScreenshotPages {
		page := &report.ScreenshotPages[i]
		if page.PageID != pageID {
			continue
		}
		if route != "" {
			page.Route = route
		}
		if !slices.Contains(page.Paths, name) {
			page.Paths = append(page.Paths, name)
		}
		return
	}
	report.ScreenshotPages = append(report.ScreenshotPages, designScreenshotPage{
		PageID: pageID, Route: route, Paths: []string{name},
	})
}

func saveDesignShot(root *os.Root, page string, width int, scheme string, data []byte) (string, error) {
	if page == "" || page == "." || page == ".." || strings.ContainsAny(page, "/\\") || width <= 0 {
		return "", fmt.Errorf("invalid screenshot page or width")
	}
	name := fmt.Sprintf(".copera/shots/%s-%d", page, width)
	switch scheme {
	case "light", "dark":
		name += "-" + scheme
	case "":
	default:
		return "", fmt.Errorf("invalid screenshot scheme %q", scheme)
	}
	name += ".webp"
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
		if report.Version != nil {
			cli.Printer.PrintLine(fmt.Sprintf("Version: %d (%s)", report.Version.Version, designsTerminalLine(report.Version.ID)))
		}
		cli.Printer.PrintLine("Build: " + designsTerminalLine(report.State))
		if report.AppURL != "" {
			cli.Printer.PrintLine("URL: " + designsTerminalLine(report.AppURL))
		}
		if report.DarkScheme != nil && !*report.DarkScheme {
			cli.Printer.PrintLine("darkScheme: false")
		}
		for _, diagnostic := range report.Diagnostics {
			message := fmt.Sprintf("%s: %s", diagnostic.Severity, diagnostic.Message)
			if diagnostic.FilePath != "" {
				message = designSourceLocation(diagnostic.FilePath, diagnostic.Line, diagnostic.Column) + ": " + message
			}
			cli.Printer.PrintLine(designsTerminalLine(message))
		}
		for _, entry := range report.RuntimeConsole.Entries {
			message := fmt.Sprintf("Runtime %s: %s", entry.Level, entry.Text)
			if entry.Source != nil {
				message = designSourceLocation(entry.Source.File, entry.Source.Line, entry.Source.Column) + ": " + message
			}
			cli.Printer.PrintLine(designsTerminalLine(message))
		}
		if report.Version != nil && len(report.Screenshots) > 0 {
			cli.Printer.PrintLine("Screenshots: .copera/shots/")
		}
		for _, page := range report.ScreenshotPages {
			label := "Page " + page.PageID
			if page.Route != "" {
				label += " (" + page.Route + ")"
			}
			cli.Printer.PrintLine(designsTerminalLine(label + ": " + strings.Join(page.Paths, ", ")))
		}
		for _, pageID := range report.ScreenshotsOmittedPageIDs {
			if !slices.ContainsFunc(report.ScreenshotPages, func(page designScreenshotPage) bool { return page.PageID == pageID }) {
				cli.Printer.PrintLine("Page " + designsTerminalLine(pageID) + ": screenshots omitted")
			}
		}
	}
	if report.State != "ready" {
		message := "Design build " + report.State
		if report.FailureCode != "" {
			message += " (" + report.FailureCode + ")"
		}
		message = designsTerminalLine(message)
		cli.Printer.PrintError("build_failed", message, designsTerminalLine(failureSuggestion), false)
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
