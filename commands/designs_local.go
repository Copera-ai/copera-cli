package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newDesignInitCmd(cli *CLI) *cobra.Command {
	var kind, runtime, system, kit, template, title, entry string
	var decline, adopt bool
	cmd := &cobra.Command{
		Use:   "init <dir>",
		Short: "Set up a local Design project",
		Args:  systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			title = strings.Join(strings.Fields(title), " ")
			if cmd.Flags().Changed("title") && title == "" {
				return designUsageError(cli, fmt.Errorf("--title must not be empty"))
			}
			if adopt {
				if runtime != "static" {
					return systemUsage(cli, fmt.Errorf("--adopt requires --runtime static"))
				}
				for _, flag := range []string{"template", "system", "kit"} {
					if cmd.Flags().Changed(flag) {
						return systemUsage(cli, fmt.Errorf("--adopt cannot be combined with --%s", flag))
					}
				}
			} else if cmd.Flags().Changed("entry") {
				return systemUsage(cli, fmt.Errorf("--entry requires --adopt"))
			}
			requestRuntime := runtime
			if template != "" && !cmd.Flags().Changed("runtime") {
				requestRuntime = ""
			}
			if err := validateDesignTemplate(kind, requestRuntime, template); err != nil {
				return designUsageError(cli, err)
			}
			if template == "" && strings.TrimSpace(kind) == "" {
				return systemUsage(cli, fmt.Errorf("kind must not be empty"))
			}
			if template == "" && runtime != "static" && runtime != "react" {
				return systemUsage(cli, fmt.Errorf("runtime must be static or react"))
			}
			if cmd.Flags().Changed("system") && (decline || strings.TrimSpace(system) == "") {
				return systemUsage(cli, fmt.Errorf("choose a system or decline it"))
			}
			if kit != "none" && (kit != "shadcn" || runtime != "react" || decline) {
				return systemUsage(cli, fmt.Errorf("kit must be none or shadcn; shadcn requires React and a Design system"))
			}
			dir := filepath.Clean(args[0])
			if adopt {
				return adoptDesign(cmd.Context(), cli, dir, kind, entry, title)
			}
			if entries, err := os.ReadDir(dir); err == nil {
				if len(entries) != 0 {
					err := fmt.Errorf("design folder %q is not empty; use --adopt to publish its existing files", dir)
					cli.Printer.PrintError("out_dir_not_empty", err.Error(), "", false)
					return exitcodes.New(exitcodes.Conflict, err)
				}
			} else if !errors.Is(err, fs.ErrNotExist) {
				return designError(cli, err)
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			guide, err := client.ArtifactGuide(ctx, kind, requestRuntime, template)
			if err != nil {
				return designError(cli, err)
			}
			state := &designState{Kind: guide.Kind, Runtime: guide.Runtime, Entrypoint: "index.html", Title: title, NoDesignSystem: decline}
			paths := make(designPaths)
			templateFiles := make(map[string]api.ArtifactContentFile)
			if template := guide.Template; template != nil {
				project := template.Project
				state.Kind, state.Runtime, state.Entrypoint = project.Kind, project.Runtime, project.Entrypoint
				state.Presentation, state.Capabilities = project.Presentation, project.Capabilities
				for _, file := range template.Files {
					if err := validateDesignContentPath(file.Path); err != nil {
						return designError(cli, err)
					}
					if _, exists := templateFiles[file.Path]; exists {
						return designError(cli, fmt.Errorf("repeated template file %q", file.Path))
					}
					if err := paths.add(file.Path, false); err != nil {
						return designError(cli, err)
					}
					templateFiles[file.Path] = file
				}
			}
			if state.Runtime == "vite_react" {
				if state.Dependencies, err = designRequiredPackages(guide.Facts); err != nil {
					return designError(cli, err)
				}
			}
			selected, err := resolveInitialDesignSystem(ctx, client, system, decline, cmd.Flags().Changed("kit"))
			if err != nil {
				var exitErr *exitcodes.ExitError
				if errors.As(err, &exitErr) && exitErr.Code == exitcodes.Usage {
					return systemUsage(cli, err)
				}
				return systemAPIError(cli, err)
			}
			var installed map[string]designFile
			if selected == nil && kit == "shadcn" {
				return systemUsage(cli, fmt.Errorf("shadcn needs a saved Design system"))
			}
			if selected != nil {
				installed, err = materializeInitialSystem(ctx, client, state, selected, kit)
				if err != nil {
					return systemAPIError(cli, err)
				}
				for name, file := range installed {
					if existing, found := templateFiles[name]; found && (existing.MediaType != file.MediaType || !bytes.Equal([]byte(existing.Content), file.Data)) {
						return systemConflict(cli, fmt.Sprintf("template collides with installed path %q", name))
					}
					if err := paths.add(name, false); err != nil {
						return systemConflict(cli, err.Error())
					}
				}
				if err := linkInitialBrand(state, templateFiles, guide); err != nil {
					return designError(cli, err)
				}
			}
			root, err := openDesignDir(dir, true)
			if err != nil {
				return designError(cli, err)
			}
			defer func() { _ = root.Close() }()
			entries, err := fs.ReadDir(root.FS(), ".")
			if err != nil {
				return designError(cli, err)
			}
			if len(entries) != 0 {
				return systemConflict(cli, fmt.Sprintf("design folder %q is not empty; use --adopt to publish its existing files", dir))
			}
			for _, file := range templateFiles {
				if err := writeDesignFile(root, file.Path, []byte(file.Content)); err != nil {
					return designError(cli, err)
				}
			}
			for name, file := range installed {
				if err := writeDesignFile(root, name, file.Data); err != nil {
					return designError(cli, err)
				}
			}
			if err := writeDesignGuide(root, guide); err != nil {
				return designError(cli, err)
			}
			if err := saveDesignState(root, state, nil); err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(state)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Design initialized in %s", dir))
			if state.DesignSystemRevision > 0 {
				cli.Printer.PrintLine(fmt.Sprintf("Design system %s revision %d (%s)", state.DesignSystemID, state.DesignSystemRevision, state.DesignSystemKit))
				if state.Runtime == "vite_react" {
					cli.Printer.PrintLine(brandImportGuidance(state.DesignSystemKit))
				}
				if state.BrandWarning != "" {
					cli.Printer.PrintLine(state.BrandWarning)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "Design kind")
	cmd.Flags().StringVar(&runtime, "runtime", "static", "Design runtime (static or react)")
	cmd.Flags().StringVar(&system, "system", "", "Saved design system name or ID")
	cmd.Flags().BoolVar(&decline, "no-system", false, "Start without a saved Design system")
	cmd.Flags().StringVar(&kit, "kit", "none", "Optional kit (none or shadcn)")
	cmd.Flags().StringVar(&template, "template", "", "Starter template (review)")
	cmd.Flags().BoolVar(&adopt, "adopt", false, "Use the existing static files in this folder")
	cmd.Flags().StringVar(&entry, "entry", "", "Entry HTML file when adopting a folder")
	cmd.Flags().StringVar(&title, "title", "", "Title for the first push")
	cmd.SetFlagErrorFunc(systemFlags(cli))
	return cmd
}

func designRequiredPackages(data json.RawMessage) ([]api.ArtifactDependency, error) {
	var facts struct {
		Toolchain struct {
			RequiredPackages []string `json:"requiredPackages"`
		} `json:"toolchain"`
		Packages []api.ArtifactDependency `json:"packages"`
	}
	if err := json.Unmarshal(data, &facts); err != nil {
		return nil, fmt.Errorf("read guide package facts: %w", err)
	}
	var required []api.ArtifactDependency
	for _, name := range facts.Toolchain.RequiredPackages {
		for _, dep := range facts.Packages {
			if dep.Name == name {
				required = append(required, dep)
				break
			}
		}
	}
	return required, nil
}

func checkDesignPullTargets(root *os.Root, remote map[string]designFile) error {
	check := func(name string) error {
		local := filepath.FromSlash(name)
		if err := checkDesignSymlinks(root, local); err != nil {
			return err
		}
		info, err := root.Lstat(local)
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("design path %q is not a regular file", name)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", ".copera/guide/README.md", ".copera/guide/facts.json"} {
		if err := check(name); err != nil {
			return err
		}
	}
	for _, file := range remote {
		if err := check(path.Join(designBaseDir, file.checksum())); err != nil {
			return err
		}
	}
	return nil
}

func writeDesignGuideFiles(root *os.Root, guide *api.ArtifactGuide) error {
	if err := writeDesignFile(root, ".copera/guide/README.md", []byte(guide.Markdown)); err != nil {
		return err
	}
	if len(guide.Facts) != 0 {
		if err := writeDesignFile(root, ".copera/guide/facts.json", append(guide.Facts, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func writeDesignGuide(root *os.Root, guide *api.ArtifactGuide) error {
	if err := writeDesignGuideFiles(root, guide); err != nil {
		return err
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := writeDesignFile(root, name, []byte(designRules)); err != nil {
			return err
		}
	}
	return nil
}

func validateRemoteDesignVersion(version *api.ArtifactVersion) error {
	paths := make(designPaths)
	files := make(map[string]bool)
	add := func(name string, file bool) error {
		if name == "" && !file {
			return nil
		}
		if err := validateDesignContentPath(name); err != nil {
			return err
		}
		if file {
			if files[name] {
				return fmt.Errorf("duplicate design path %q", name)
			}
			files[name] = true
		}
		return paths.add(name, false)
	}
	for _, file := range version.Files {
		if err := add(file.Path, true); err != nil {
			return err
		}
	}
	for _, asset := range version.AuthoringManifest.Assets {
		if err := add(asset.Path, true); err != nil {
			return err
		}
	}
	shared := version.AuthoringManifest.SharedDesign
	for _, name := range []string{version.Entrypoint, shared.GuidancePath, shared.TokensPath, shared.ComponentsPath, shared.AssetsPath} {
		if err := add(name, false); err != nil {
			return err
		}
	}
	for _, page := range version.AuthoringManifest.Pages {
		if err := add(page.Entrypoint, false); err != nil {
			return err
		}
	}
	for _, doc := range version.AuthoringManifest.Documents {
		if err := add(doc.Path, false); err != nil {
			return err
		}
	}
	return nil
}

func readRemoteDesignFiles(ctx context.Context, client *api.Client, id string, version *api.ArtifactVersion, staging string) (map[string]designFile, error) {
	if err := validateRemoteDesignVersion(version); err != nil {
		return nil, err
	}
	pack, err := client.ArtifactHandoff(ctx, id, api.ArtifactHandoffOpts{VersionID: version.ID, MaxInlineBytes: handoffMaxInlineBytes})
	if err != nil {
		return nil, err
	}
	inline := make(map[string]api.ArtifactHandoffFile, len(pack.Files))
	for _, file := range pack.Files {
		if err := validateDesignContentPath(file.Path); err != nil {
			return nil, err
		}
		inline[file.Path] = file
	}
	if pack.Spec != nil {
		if err := validateDesignContentPath(pack.Spec.Path); err != nil {
			return nil, err
		}
		inline[pack.Spec.Path] = api.ArtifactHandoffFile{Content: pack.Spec.Markdown, Omitted: pack.Spec.Omitted}
	}
	assets := make(map[string]api.ArtifactHandoffAsset, len(pack.Design.Assets))
	for _, asset := range pack.Design.Assets {
		if err := validateDesignContentPath(asset.Path); err != nil {
			return nil, err
		}
		assets[asset.Path] = asset
	}
	files := make(map[string]designFile, len(version.Files)+len(version.AuthoringManifest.Assets))
	for _, file := range version.Files {
		entry := inline[file.Path]
		if entry.Omitted == api.ArtifactOmittedSensitive {
			return nil, fmt.Errorf("the API refused the credential-shaped source file %q", file.Path)
		}
		var text string
		if entry.Content != nil {
			text = *entry.Content
		} else {
			_, text, err = client.ArtifactSourceFile(ctx, id, version.ID, file.Path)
			if err != nil {
				return nil, fmt.Errorf("read %q: %w", file.Path, err)
			}
		}
		data := []byte(text)
		if file.ContentDigest != "" && sha256Hex(data) != file.ContentDigest {
			return nil, fmt.Errorf("source %q does not match its checksum", file.Path)
		}
		files[file.Path] = designFile{Data: data, MediaType: file.MediaType}
	}
	var assetBytes int64
	for _, asset := range version.AuthoringManifest.Assets {
		signed := assets[asset.Path]
		if signed.URL == nil || *signed.URL == "" {
			return nil, fmt.Errorf("design asset %q has no download link", asset.Path)
		}
		temp, err := os.CreateTemp(staging, "asset-*")
		if err != nil {
			return nil, err
		}
		limit := min(designAssetLimit(asset.MediaType), (256<<20)-assetBytes)
		n, err := downloadDesignBytes(ctx, client, *signed.URL, temp, 0, limit, asset.ContentDigest)
		if err := errors.Join(err, temp.Close()); err != nil {
			return nil, fmt.Errorf("download design asset %q: %w", asset.Path, err)
		}
		assetBytes += n
		file := api.ArtifactUpload{
			ByteLength: n, ContentDigest: asset.ContentDigest,
			Open: func() (io.ReadCloser, error) { return os.Open(temp.Name()) },
		}
		files[asset.Path] = designFile{MediaType: asset.MediaType, Binary: true, Upload: &file}
	}
	return files, nil
}

func defaultDesignKind(manifest api.ArtifactManifest) string {
	for _, page := range manifest.Pages {
		if page.ID == manifest.DefaultPageID {
			return page.Kind
		}
	}
	return ""
}

func newDesignPullCmd(cli *CLI) *cobra.Command {
	var number int
	cmd := &cobra.Command{
		Use:   "pull <id|url> [dir]",
		Short: "Pull a Design into a local folder and merge local edits",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return designError(cli, err)
			}
			if number < 0 || (number == 0 && cmd.Flags().Changed("version")) {
				return designError(cli, fmt.Errorf("version must be a positive number"))
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			artifact, err := client.ArtifactGet(ctx, id)
			if err != nil {
				return designError(cli, err)
			}
			version, err := designVersion(ctx, client, &artifact.Artifact, number)
			if err != nil {
				return designError(cli, err)
			}
			dir := slugify(artifact.Title, "design-"+id)
			if len(args) == 2 {
				dir = filepath.Clean(args[1])
			}
			conflicts, err := pullDesignVersion(ctx, client, dir, id, version, artifact.HeadRevision, "")
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				result := struct {
					ID        string   `json:"id"`
					VersionID string   `json:"versionId"`
					Dir       string   `json:"dir"`
					Conflicts []string `json:"conflicts"`
				}{id, version.ID, dir, conflicts}
				if err := cli.Printer.PrintJSON(result); err != nil {
					return err
				}
			} else {
				cli.Printer.PrintLine(fmt.Sprintf("Pulled Design %s version %d into %s", id, version.Version, dir))
			}
			return printDesignPullConflicts(cli, conflicts)
		},
	}
	cmd.Flags().IntVar(&number, "version", 0, "Version number (default: current)")
	return cmd
}

func pullDesignVersion(ctx context.Context, client *api.Client, dir, id string, version *api.ArtifactVersion, headRevision int, workspaceID string) ([]string, error) {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	staging, err := os.MkdirTemp("", "copera-design-pull-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)
	remote, err := readRemoteDesignFiles(ctx, client, id, version, staging)
	if err != nil {
		return nil, err
	}
	if workspaceID == "" {
		workspace, err := client.WorkspaceInfo(ctx)
		if err != nil {
			return nil, err
		}
		workspaceID = workspace.ID
	}
	kind := defaultDesignKind(version.AuthoringManifest)
	guide, err := client.ArtifactGuide(ctx, kind, version.Runtime, "")
	if err != nil {
		return nil, err
	}
	if kind == "" {
		kind = guide.Kind
	}
	state := &designState{
		ID: id, Workspace: workspaceID, BaseVersionID: version.ID, HeadRevision: headRevision,
		Kind: kind, Runtime: version.Runtime, Entrypoint: version.Entrypoint, Manifest: version.AuthoringManifest,
		Capabilities: version.ProjectCapabilities,
	}
	root, err := openDesignDir(dir, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return applyDesignPull(root, state, remote, guide)
}

func printDesignPullConflicts(cli *CLI, conflicts []string) error {
	if len(conflicts) == 0 {
		return nil
	}
	err := fmt.Errorf("pull conflicts: %s", strings.Join(conflicts, ", "))
	cli.Printer.PrintError("pull_conflict", err.Error(), "Resolve the files left in the folder", false)
	return exitcodes.New(exitcodes.Conflict, err)
}

func applyDesignPull(root *os.Root, state *designState, remote map[string]designFile, guide *api.ArtifactGuide) ([]string, error) {
	previous, err := loadDesignState(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	base := map[string]designFile{}
	if previous != nil {
		if previous.ID != "" && previous.ID != state.ID {
			return nil, fmt.Errorf("the folder belongs to Design %s", previous.ID)
		}
		state.Adopted = previous.Adopted
		// Pull compares local files with the remote copy before writing them.
		previous.Adopted = false
		base, err = loadDesignBase(root, previous)
		if err != nil {
			return nil, err
		}
		if previous.BaseVersionID == state.BaseVersionID {
			state.BuildState = previous.BuildState
			state.Dependencies = previous.Dependencies
			if state.Capabilities == nil {
				state.Capabilities = previous.Capabilities
			}
		}
		state.AssetPaths = make(map[string]string)
		for name, canonical := range previous.AssetPaths {
			if name == canonical {
				continue
			}
			if err := validateDesignContentPath(name); err != nil {
				return nil, err
			}
			if asset, ok := remote[canonical]; ok && asset.Binary {
				if _, exists := remote[name]; exists {
					return nil, fmt.Errorf("asset path %q collides with remote source", name)
				}
				delete(remote, canonical)
				remote[name] = asset
				state.AssetPaths[name] = canonical
			}
		}
	}
	local, localPaths, err := scanDesignTree(root, previous)
	if err != nil {
		return nil, err
	}
	remotePaths := make(designPaths)
	for name := range remote {
		if err := remotePaths.add(name, false); err != nil {
			return nil, err
		}
	}
	for key, entry := range remotePaths {
		if local, exists := localPaths[key]; exists && local.name != entry.name {
			return nil, fmt.Errorf("design paths %q and %q collide", local.name, entry.name)
		}
	}
	merged, conflicts := mergeDesignFiles(base, local, remote)
	mergedPaths := make(designPaths)
	for name := range merged {
		if err := mergedPaths.add(name, false); err != nil {
			return nil, err
		}
	}
	if err := checkDesignPullTargets(root, remote); err != nil {
		return nil, err
	}
	var replacedDirs []string
	for _, entry := range localPaths {
		if !entry.dir {
			continue
		}
		for name := entry.name; name != "."; name = path.Dir(name) {
			if _, replacement := merged[name]; replacement {
				replacedDirs = append(replacedDirs, entry.name)
				break
			}
		}
	}
	names := designFileNames(local, merged)
	for _, name := range names {
		if _, exists := merged[name]; !exists {
			if err := root.Remove(filepath.FromSlash(name)); err != nil {
				return nil, err
			}
		}
	}
	// Remove old directories only where a merged file takes their place.
	slices.Sort(replacedDirs)
	slices.Reverse(replacedDirs)
	for _, dir := range replacedDirs {
		if err := root.Remove(filepath.FromSlash(dir)); err != nil {
			return nil, err
		}
	}
	for _, name := range names {
		next, exists := merged[name]
		before, existed := local[name]
		if !exists || (existed && sameDesignContent(before, next)) {
			continue
		}
		if err := writeDesignContent(root, name, next); err != nil {
			return nil, err
		}
	}
	if err := writeDesignGuide(root, guide); err != nil {
		return nil, err
	}
	return conflicts, saveDesignState(root, state, remote)
}

func designWorkDir(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return filepath.Clean(args[0])
}

func newDesignStatusCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "status [dir]",
		Short: "Show local changes and the remote Design build state",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := openDesignDir(designWorkDir(args), false)
			if err != nil {
				return designError(cli, err)
			}
			defer func() { _ = root.Close() }()
			state, err := loadDesignState(root)
			if err != nil {
				return designError(cli, err)
			}
			local, err := scanDesignFiles(root, state)
			if err != nil {
				return designError(cli, err)
			}
			status := struct {
				ID            string         `json:"id"`
				BaseVersionID string         `json:"baseVersionId"`
				RemoteAhead   bool           `json:"remoteAhead"`
				BuildState    string         `json:"buildState"`
				Changes       []designChange `json:"changes"`
			}{ID: state.ID, BaseVersionID: state.BaseVersionID, BuildState: state.BuildState, Changes: designChanges(state, local)}
			if state.ID != "" {
				client, _, err := requireAPIClient(cli)
				if err != nil {
					return err
				}
				ctx := cmd.Context()
				head, err := client.ArtifactGet(ctx, state.ID)
				if err != nil {
					return designError(cli, err)
				}
				status.RemoteAhead = head.HeadRevision > state.HeadRevision || head.CurrentDraftVersionID != state.BaseVersionID
				build, err := client.ArtifactBuild(ctx, state.ID, head.CurrentDraftVersionID, 0)
				if err != nil {
					return designError(cli, err)
				}
				status.BuildState = build.State
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(status)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Remote head ahead: %t", status.RemoteAhead))
			if status.BuildState != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Build: %s", status.BuildState))
			}
			for _, change := range status.Changes {
				cli.Printer.PrintLine(fmt.Sprintf("%s  %s", change.Kind, change.Path))
			}
			return nil
		},
	}
}

func newDesignDiffCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "diff [dir]",
		Short: "Show local changes against the pulled version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := openDesignDir(designWorkDir(args), false)
			if err != nil {
				return designError(cli, err)
			}
			defer func() { _ = root.Close() }()
			state, err := loadDesignState(root)
			if err != nil {
				return designError(cli, err)
			}
			base, err := loadDesignBase(root, state)
			if err != nil {
				return designError(cli, err)
			}
			local, err := scanDesignFiles(root, state)
			if err != nil {
				return designError(cli, err)
			}
			changes := designChanges(state, local)
			for index := range changes {
				if err := designDiff(&changes[index], base, local); err != nil {
					return designError(cli, err)
				}
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(changes)
			}
			for _, change := range changes {
				if _, err := io.WriteString(cli.Printer.Out, change.Patch); err != nil {
					return designError(cli, err)
				}
			}
			return nil
		},
	}
}
