package commands

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
)

const installedBrandPrefix = "shared/brand/"
const installedBrandStylesheet = installedBrandPrefix + "tokens.css"

func resolveInitialDesignSystem(ctx context.Context, client *api.Client, explicit string, decline, kitFlag bool) (*api.ArtifactDesignSystem, error) {
	defaults, err := client.ArtifactDesignSystemDefaults(ctx)
	if err != nil {
		if explicit == "" && !decline && !kitFlag && systemFeatureUnavailable(err) {
			// The legacy server has no default to apply on an ordinary init.
			return nil, nil
		}
		return nil, err
	}
	if defaults.Locked && decline {
		return nil, workspaceSystemLocked()
	}
	if decline {
		return nil, nil
	}
	if explicit != "" {
		selected, err := findInitialDesignSystem(ctx, client, explicit)
		if err != nil {
			return nil, err
		}
		if defaults.Locked && (defaults.WorkspaceSystemID == nil || selected.ID != *defaults.WorkspaceSystemID) {
			return nil, workspaceSystemLocked()
		}
		return selected, nil
	}
	if defaults.Locked {
		if defaults.WorkspaceSystemID == nil {
			return nil, workspaceSystemLocked()
		}
		return client.ArtifactDesignSystem(ctx, *defaults.WorkspaceSystemID)
	}
	for _, id := range []*string{defaults.PersonalSystemID, defaults.WorkspaceSystemID} {
		if id == nil {
			continue
		}
		selected, err := client.ArtifactDesignSystem(ctx, *id)
		var missing *api.APIError
		if errors.As(err, &missing) && missing.StatusCode == http.StatusNotFound && !systemFeatureUnavailable(missing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if selected.Available {
			return selected, nil
		}
	}
	return nil, nil
}

func workspaceSystemLocked() error {
	return &api.APIError{
		StatusCode: http.StatusConflict,
		Code:       "ARTIFACT_DESIGN_SYSTEM_CHANGED",
		Reason:     "workspace_system_locked",
		Message:    "The workspace requires its default Design system.",
	}
}

func findInitialDesignSystem(ctx context.Context, client *api.Client, query string) (*api.ArtifactDesignSystem, error) {
	if validSystemHex(query, 24) {
		return client.ArtifactDesignSystem(ctx, query)
	}
	systems, err := client.ArtifactDesignSystems(ctx)
	if err != nil {
		return nil, err
	}
	var chosen *api.ArtifactDesignSystem
	for _, system := range systems.DesignSystems {
		if !strings.EqualFold(system.Name, query) {
			continue
		}
		if chosen != nil {
			return nil, exitcodes.Newf(exitcodes.Usage, "multiple Design systems are named %q; use an ID", query)
		}
		chosen = &system
	}
	if chosen == nil {
		return nil, &api.APIError{StatusCode: http.StatusNotFound, Code: "NOT_FOUND", Message: fmt.Sprintf("Design system %q was not found.", query)}
	}
	return chosen, nil
}

func materializeInitialSystem(ctx context.Context, client *api.Client, state *designState, selected *api.ArtifactDesignSystem, kit string) (map[string]designFile, error) {
	if !selected.Available {
		reason := ""
		if selected.UnavailableReason != nil {
			reason = *selected.UnavailableReason
		}
		return nil, &api.APIError{
			StatusCode: http.StatusConflict, Code: "ARTIFACT_DESIGN_SYSTEM_UNAVAILABLE", Reason: reason,
			Message: "The saved Design system is unavailable.",
		}
	}
	if systemID(selected.ID) != nil || selected.Revision < 1 || selected.PackageDigest == nil || !validSystemHex(*selected.PackageDigest, 64) {
		return nil, fmt.Errorf("the server needs Phase 3 Design systems support to pin an installation")
	}
	projection, err := client.ArtifactDesignSystemInstallation(ctx, selected.ID, state.Runtime, kit, selected.Revision)
	if err != nil {
		return nil, err
	}
	pin := projection.Install
	if pin.SystemID != selected.ID || pin.Revision != selected.Revision || pin.PackageDigest != *selected.PackageDigest || pin.Kit != kit {
		return nil, fmt.Errorf("the server returned an unexpected Design system installation pin")
	}
	files, err := readSystemProjectionFiles(projection)
	if err != nil {
		return nil, err
	}
	workspace, err := client.WorkspaceInfo(ctx)
	if err != nil {
		return nil, err
	}
	if !validSystemHex(workspace.ID, 24) {
		return nil, fmt.Errorf("the server did not identify the workspace for the Design system")
	}
	state.Workspace = workspace.ID
	state.DesignSystemID, state.DesignSystemRevision, state.DesignSystemDigest, state.DesignSystemKit = pin.SystemID, pin.Revision, pin.PackageDigest, pin.Kit
	state.BrandStylesheet = installedBrandStylesheet
	if kit == "shadcn" {
		state.BrandStylesheet = "shared/tokens.css"
		for line := range strings.SplitSeq(string(files["shared/DESIGN.md"].Data), "\n") {
			if strings.HasPrefix(line, "The managed kit retains its explicit values for ") {
				state.BrandWarning = line
				break
			}
		}
	}
	state.DesignSystemFiles = make(map[string]string, len(files))
	for name, file := range files {
		state.DesignSystemFiles[name] = sha256Hex(file.Data)
	}
	if len(projection.Dependencies) != 0 {
		combined := make(map[string]api.ArtifactDependency)
		for _, dep := range state.Dependencies {
			combined[dep.Name] = dep
		}
		seen := make(map[string]bool)
		for _, dep := range projection.Dependencies {
			if dep.Name == "" || dep.Version == "" || seen[dep.Name] {
				return nil, fmt.Errorf("the server returned an invalid Design system dependency pin")
			}
			seen[dep.Name] = true
			if required := combined[dep.Name]; required.Version != dep.Version {
				combined[dep.Name] = api.ArtifactDependency{Name: dep.Name, Version: dep.Version}
			}
		}
		state.Dependencies = nil
		for _, name := range slices.Sorted(maps.Keys(combined)) {
			state.Dependencies = append(state.Dependencies, combined[name])
		}
	}
	return files, nil
}

func readSystemProjectionFiles(projection *api.ArtifactSystemProjection) (map[string]designFile, error) {
	paths := make(designPaths)
	result := make(map[string]designFile, len(projection.Files))
	budget := systemFileBudget{}
	for _, entry := range projection.Files {
		if err := validateSystemPath(entry.Path, entry.Path); err != nil {
			return nil, err
		}
		if _, exists := result[entry.Path]; exists {
			return nil, fmt.Errorf("repeated installed path %q", entry.Path)
		}
		if err := paths.add(entry.Path, false); err != nil {
			return nil, err
		}
		name, media, err := projectionFileMedia(entry.Path, projection.Install.Kit)
		if err != nil {
			return nil, err
		}
		if media != entry.MediaType {
			return nil, fmt.Errorf("unexpected media type for installed path %q", entry.Path)
		}
		if err := validateSystemFileUsage(entry.Path, media, entry.Usage); err != nil {
			return nil, err
		}
		data, err := decodeSystemBase64(entry, systemFileLimit(name, media))
		if err != nil {
			return nil, err
		}
		if err := budget.add(name, media, int64(len(data))); err != nil {
			return nil, err
		}
		result[entry.Path] = designFile{Data: data, MediaType: media, Binary: systemBinary(media)}
	}
	for _, file := range []string{"DESIGN.md", "tokens.json", "tokens.css"} {
		if _, ok := result[installedBrandPrefix+file]; !ok {
			return nil, fmt.Errorf("installed Design system is missing %q", installedBrandPrefix+file)
		}
	}
	return result, nil
}

func projectionFileMedia(name, kit string) (string, string, error) {
	if relative, found := strings.CutPrefix(name, installedBrandPrefix); found {
		media, err := systemFileMedia(relative, nil)
		return relative, media, err
	}
	if kit == "shadcn" {
		switch name {
		case "shared/shadcn.json", "shared/components.json", "shared/assets.json":
			return name, "application/json", nil
		case "shared/DESIGN.md":
			return name, "text/markdown", nil
		case "shared/tokens.css":
			return name, "text/css", nil
		}
		if relative, found := strings.CutPrefix(name, "shared/"); found {
			root, _, _ := strings.Cut(relative, "/")
			if root == "components" || root == "lib" || root == "hooks" {
				media, err := systemFileMedia(relative, &api.ArtifactSystemKit{})
				return relative, media, err
			}
		}
	}
	return "", "", fmt.Errorf("unsupported installed path %q", name)
}

func linkInitialBrand(state *designState, template map[string]api.ArtifactContentFile, guide *api.ArtifactGuide) error {
	if state.Runtime == "static_web" {
		file, exists := template[state.Entrypoint]
		if exists && !strings.Contains(file.Content, installedBrandStylesheet) {
			href, err := filepath.Rel(filepath.Dir(state.Entrypoint), installedBrandStylesheet)
			if err != nil {
				return err
			}
			before, after, found := strings.Cut(file.Content, "</head>")
			link := "  <link rel=\"stylesheet\" href=\"" + filepath.ToSlash(href) + "\">\n"
			if found {
				file.Content = before + link + "</head>" + after
			} else {
				file.Content = link + file.Content
			}
			template[state.Entrypoint] = file
		}
	}
	guide.Markdown += "\n\n## Installed brand\n\n" + brandImportGuidance(state.DesignSystemKit)
	if state.BrandWarning != "" {
		guide.Markdown += "\n\n" + state.BrandWarning
	}
	guide.Markdown += "\n"
	return nil
}

func brandImportGuidance(kit string) string {
	if kit == "shadcn" {
		return "Load managed shared/tokens.css after your own stylesheet. It imports " + installedBrandStylesheet + " before the managed shadcn globals. shadcn controls common roles such as --primary, while --design-* uses the installed brand values."
	}
	return "Load " + installedBrandStylesheet + " after your own stylesheet."
}

func pullRefreshedDesign(ctx context.Context, cli *CLI, client *api.Client, result *api.ArtifactDesignChange) error {
	root, err := openDesignDir(".", false)
	if err != nil {
		return designError(cli, err)
	}
	defer root.Close()
	state, err := loadDesignState(root)
	if errors.Is(err, os.ErrNotExist) || (err == nil && state.ID != result.ArtifactID) {
		return nil
	}
	if err != nil {
		return designError(cli, err)
	}
	workspace, err := client.WorkspaceInfo(ctx)
	if err != nil {
		return designError(cli, err)
	}
	if state.Workspace != "" && state.Workspace != workspace.ID {
		return systemConflict(cli, "The current folder tracks a Design in another workspace.")
	}
	version, err := client.ArtifactVersionGet(ctx, result.ArtifactID, result.VersionID)
	if err != nil {
		return designError(cli, err)
	}
	conflicts, err := pullDesignVersion(ctx, client, root.Name(), result.ArtifactID, version, result.HeadRevision, workspace.ID)
	if err != nil {
		return designError(cli, fmt.Errorf("the Design was refreshed remotely but the working folder could not be updated: %w", err))
	}
	return printDesignPullConflicts(cli, conflicts)
}
