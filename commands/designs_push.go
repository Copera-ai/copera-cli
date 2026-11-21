package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func newDesignPushCmd(cli *CLI) *cobra.Command {
	var summary, title string
	var wait, waitForPreview, noCheck, force bool
	cmd := &cobra.Command{
		Use:   "push [dir] -m SUMMARY",
		Short: "Save local edits as one new Design version",
		Long: "Save local edits as one new Design version. The first push uses --title, the saved init title, " +
			"a common page title, the entry HTML's <title>, or a humanized folder name.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(summary) == "" || utf8.RuneCountInString(summary) > 160 || strings.ContainsAny(summary, "\r\n") {
				msg := "change summary must be one line of 1 to 160 characters"
				cli.Printer.PrintError("invalid_flag", msg, "", false)
				return exitcodes.Newf(exitcodes.Usage, "%s", msg)
			}
			if cmd.Flags().Changed("title") {
				title = strings.Join(strings.Fields(title), " ")
				if title == "" {
					return designUsageError(cli, fmt.Errorf("--title must not be empty"))
				}
			}
			root, state, files, err := openDesignForBuild(cli, args)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			requestedTitle := state.Title
			if cmd.Flags().Changed("title") {
				if state.ID != "" {
					return designUsageError(cli, fmt.Errorf("--title is for the first push. Use copera designs rename to change this Design's title"))
				}
				requestedTitle = title
			}
			if state.ID == "" {
				if changed := changedInitialSystemFiles(state, files); len(changed) != 0 {
					return systemConflict(cli, "Installed Design system files changed: "+strings.Join(changed, ", "))
				}
			}
			client, cfg, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			vendorHint := designVendorHint(cli, files)
			base := state.Files
			if force && state.ID != "" {
				head, err := client.ArtifactGet(ctx, state.ID)
				if err != nil {
					return designPushError(cli, err, designPushProgress(state))
				}
				base = designHeadFiles(&head.CurrentVersion, state.AssetPaths)
				state.BaseVersionID, state.HeadRevision = head.CurrentVersion.ID, head.HeadRevision
			}
			checked := false
			if !noCheck && (!force || state.ID == "") && (state.ID == "" || state.BaseVersionID != "") && state.LastCheck.recent() {
				digest, err := designCheckDigest(cfg, state, files)
				if err != nil {
					return designPushError(cli, err, designPushProgress(state))
				}
				checked = digest == state.LastCheck.Digest
			}
			var check *designBuildReport
			if !noCheck && !checked {
				check, err = checkDesign(ctx, cli, client, root, state, files, force)
				if err != nil {
					return designPushError(cli, err, designPushProgress(state))
				}
				vendorHint(check)
				if check.State != "ready" {
					return printDesignBuild(cli, check, designPushProgress(state))
				}
			}
			var pushed *api.ArtifactPushResult
			var refreshManifest bool
			if state.ID == "" {
				pushed, err = createLocalDesign(ctx, client, root, state, files, summary, requestedTitle)
			} else {
				pushed, refreshManifest, err = pushDesignFiles(ctx, cli, client, root, state, base, files, summary, force, wait || waitForPreview)
			}
			if err != nil {
				if state.ID == "" && (state.DesignSystemRevision > 0 || state.NoDesignSystem) {
					return systemAPIError(cli, err)
				}
				return designPushError(cli, err, designPushProgress(state))
			}
			vendorHint(&designBuildReport{Warnings: pushed.Warnings})
			committedCtx := ctx
			if waitForPreview {
				var cancel context.CancelFunc
				committedCtx, cancel = context.WithTimeout(ctx, 120*time.Second)
				defer cancel()
			}
			defaultPageID := pushed.Version.AuthoringManifest.DefaultPageID
			if refreshManifest {
				version, err := client.ArtifactVersionGet(committedCtx, pushed.ArtifactID, pushed.Version.ID)
				if err != nil {
					cli.Printer.Info("Version %s was saved. Run designs pull to refresh the local manifest: %s", pushed.Version.ID, err)
				} else {
					state.Manifest = version.AuthoringManifest
					defaultPageID = version.AuthoringManifest.DefaultPageID
				}
			}
			if state.BaseVersionID != pushed.Version.ID {
				state.LastCheck = nil
			}
			state.ID, state.BaseVersionID, state.HeadRevision = pushed.ArtifactID, pushed.Version.ID, pushed.HeadRevision
			state.BuildState = pushed.BuildState
			progress := designPushProgress(state)
			state.FirstPushPending, state.PendingAssets = false, nil
			if err := saveDesignState(root, state, files); err != nil {
				err = fmt.Errorf("version %s was saved in Copera, but local state could not be saved: %w", pushed.Version.ID, err)
				if progress != "" {
					err = fmt.Errorf("%s %w", progress, err)
				}
				return designError(cli, err)
			}
			if waitForPreview {
				return waitForDesignPreview(committedCtx, cli, client, pushed, defaultPageID, progress)
			}
			appURL, err := designAppURL(ctx, client, state.ID)
			if err != nil {
				cli.Printer.Info("Version %s was saved. Run copera designs open %s for its URL: %s",
					designsTerminalLine(pushed.Version.ID), designsTerminalLine(state.ID), designsTerminalLine(err.Error()))
			}
			if wait {
				return waitForDesignBuild(ctx, cli, client, root, state, &pushed.Version, check, vendorHint, progress, appURL)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(struct {
					*api.ArtifactPushResult
					AppURL string `json:"appUrl,omitempty"`
				}{pushed, appURL})
			}
			cli.Printer.PrintLine(fmt.Sprintf("Version: %d (%s)", pushed.Version.Version, pushed.Version.ID))
			cli.Printer.PrintLine("Build: " + pushed.BuildState)
			if appURL != "" {
				cli.Printer.PrintLine("URL: " + designsTerminalLine(appURL))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&summary, "message", "m", "", "Summary of the changes (up to 160 characters)")
	cmd.Flags().StringVar(&title, "title", "", "Title for the first push")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for the writer and the resulting build")
	cmd.Flags().BoolVar(&waitForPreview, "wait-for-preview", false, "Wait for the writer and a usable preview")
	cmd.MarkFlagsMutuallyExclusive("wait", "wait-for-preview")
	cmd.Flags().BoolVar(&noCheck, "no-check", false, "Skip the dry-run build")
	cmd.Flags().BoolVar(&force, "force", false, "Push this folder over the current remote head")
	_ = cmd.MarkFlagRequired("message")
	return cmd
}

func designPushProgress(state *designState) string {
	if !state.FirstPushPending {
		return ""
	}
	paths := strings.Join(slices.Sorted(maps.Keys(state.PendingAssets)), ", ")
	if paths == "" {
		paths = "none"
	}
	return fmt.Sprintf("Design %s exists. Uploaded files: %s.", state.ID, paths)
}

func designPushError(cli *CLI, err error, progress string) error {
	var apiErr *api.APIError
	hasAPIError := errors.As(err, &apiErr)
	if hasAPIError && apiErr.StatusCode == http.StatusConflict && len(apiErr.ChangedPaths) > 0 {
		suggestion := "Changed paths: " + strings.Join(apiErr.ChangedPaths, ", ")
		if progress != "" {
			suggestion += ". " + progress
		}
		cli.Printer.PrintError("version_conflict", apiErr.Error(), suggestion, false)
		return exitcodes.New(exitcodes.Conflict, err)
	}
	if progress != "" {
		code := exitcodes.Error
		if hasAPIError {
			code = apiErr.ExitCode()
		}
		cli.Printer.PrintError("push_incomplete", err.Error(), progress, false)
		return exitcodes.New(code, err)
	}
	return designError(cli, err)
}

func designHeadFiles(version *api.ArtifactVersion, aliases map[string]string) map[string]string {
	base := make(map[string]string, len(version.Files)+len(version.AuthoringManifest.Assets))
	for _, file := range version.Files {
		base[file.Path] = file.ContentDigest
	}
	for _, asset := range version.AuthoringManifest.Assets {
		base[designLocalAssetPath(asset.Path, aliases)] = asset.ContentDigest
	}
	return base
}

func changedInitialSystemFiles(state *designState, files map[string]designFile) []string {
	var changed []string
	for _, name := range slices.Sorted(maps.Keys(state.DesignSystemFiles)) {
		file, exists := files[name]
		if !exists || file.checksum() != state.DesignSystemFiles[name] {
			changed = append(changed, name)
		}
	}
	return changed
}

func designCreateTitle(dir, entrypoint string, files map[string]designFile, requested string) string {
	if entrypoint == "" {
		entrypoint = "index.html"
	}
	title := strings.TrimSpace(requested)
	if title == "" {
		title = designHTMLTitle(files[entrypoint])
		if shared := sharedDesignPageTitle(title, entrypoint, files); shared != "" {
			title = shared
		}
	}
	if title == "" {
		folder := strings.NewReplacer("-", " ", "_", " ").Replace(filepath.Base(dir))
		title = cases.Title(language.Und, cases.NoLower).String(strings.Join(strings.Fields(folder), " "))
		if title == "" {
			return "Untitled Design"
		}
	}
	// The API measures its title limit in UTF-16 code units.
	units := 0
	for i, r := range title {
		units += utf16.RuneLen(r)
		if units > 240 {
			return strings.TrimSpace(title[:i])
		}
	}
	return title
}

func designHTMLTitle(file designFile) string {
	if file.MediaType != "text/html" {
		return ""
	}
	doc, err := html.Parse(bytes.NewReader(file.Data))
	if err != nil {
		return ""
	}
titles:
	for node := range doc.Descendants() {
		if node.DataAtom != atom.Title || node.Namespace != "" || node.FirstChild == nil {
			continue
		}
		for parent := range node.Ancestors() {
			if parent.DataAtom == atom.Template {
				continue titles
			}
		}
		if title := strings.Join(strings.Fields(node.FirstChild.Data), " "); title != "" {
			return title
		}
	}
	return ""
}

func sharedDesignPageTitle(entryTitle, entrypoint string, files map[string]designFile) string {
	separators := []string{" — ", " – ", " - ", " | ", " · ", ": "}
	type part struct {
		text     string
		trailing bool
	}
	type matches struct {
		count      int
		firstOther string
		varies     bool
		entry      bool
	}
	parts := make(map[part]matches)
	record := func(key part, other string, entry bool) {
		if other == "" || !strings.ContainsFunc(key.text, func(r rune) bool {
			return unicode.IsLetter(r) || unicode.IsDigit(r)
		}) {
			return
		}
		match := parts[key]
		if match.count == 0 {
			match.firstOther = other
		}
		match.varies = match.varies || other != match.firstOther
		match.entry = match.entry || entry
		match.count++
		parts[key] = match
	}
	addTitle := func(title string, entry bool) {
		for _, separator := range separators {
			if i := strings.Index(title, separator); i >= 0 {
				record(part{text: title[:i]}, title[i+len(separator):], entry)
			}
			if i := strings.LastIndex(title, separator); i >= 0 {
				record(part{text: title[i+len(separator):], trailing: true}, title[:i], entry)
			}
		}
	}
	addTitle(entryTitle, true)
	seen := map[string]bool{"": true, entryTitle: true}
	for name, file := range files {
		if name == entrypoint {
			continue
		}
		title := designHTMLTitle(file)
		if !seen[title] {
			seen[title] = true
			addTitle(title, false)
		}
	}
	var shared string
	var best matches
	for key, match := range parts {
		if !match.varies || match.count < best.count {
			continue
		}
		if match.count == best.count {
			if best.entry && !match.entry {
				continue
			}
			if best.entry == match.entry && (len(key.text) < len(shared) || len(key.text) == len(shared) && key.text >= shared) {
				continue
			}
		}
		shared, best = key.text, match
	}
	return shared
}

func createLocalDesign(ctx context.Context, client *api.Client, root *os.Root, state *designState, files map[string]designFile, summary, title string) (*api.ArtifactPushResult, error) {
	dir, err := filepath.Abs(root.Name())
	if err != nil {
		return nil, err
	}
	input := api.ArtifactCreateInput{
		Title: designCreateTitle(dir, state.Entrypoint, files, title), ChangeSummary: summary, Tool: designTool(), DesignSystemID: state.DesignSystemID,
		DesignSystemRevision: state.DesignSystemRevision, DesignSystemKit: state.DesignSystemKit, NoDesignSystem: state.NoDesignSystem,
	}
	input.Project.ArtifactProjectSettings = designProject(state)
	input.Project.SchemaVersion = 1
	input.Project.Files = designTextFiles(files, state.DesignSystemFiles)
	if len(input.Project.Files) == 0 {
		return nil, fmt.Errorf("add source files before pushing the Design")
	}
	var binaries []api.ArtifactCheckFile
	keyParts := []string{dir, summary}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := files[name]
		if file.Binary && state.DesignSystemFiles[name] == "" {
			binaries = append(binaries, file.uploadPart(name))
			keyParts = append(keyParts, name, file.checksum())
		}
	}
	input.IdempotencyKey, err = designPushKey(strings.Join(keyParts, "\x00"), input)
	if err != nil {
		return nil, err
	}
	workspace, err := client.WorkspaceInfo(ctx)
	if err != nil {
		return nil, err
	}
	if state.DesignSystemRevision != 0 && state.Workspace != "" && state.Workspace != workspace.ID {
		return nil, &api.APIError{StatusCode: http.StatusConflict, Message: "This project was initialized with a Design system in another workspace."}
	}
	if state.DesignSystemRevision > 0 || state.NoDesignSystem {
		if _, err := client.ArtifactDesignSystemDefaults(ctx); err != nil {
			return nil, err
		}
	}
	created, err := client.ArtifactCreate(ctx, input, binaries...)
	if err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) {
			rejected := *apiErr
			rejected.Message = "No Design was created. " + rejected.Message
			return nil, &rejected
		}
		return nil, fmt.Errorf("could not confirm Design creation. Retry with the same files and message: %w", err)
	}
	if state.DesignSystemRevision > 0 {
		expected := api.ArtifactSystemInstall{
			SystemID: state.DesignSystemID, Revision: state.DesignSystemRevision,
			Kit: state.DesignSystemKit, PackageDigest: state.DesignSystemDigest,
		}
		if pin := created.DesignSystemInstallation; pin == nil || *pin != expected {
			return nil, fmt.Errorf("Design %s was created but the server did not confirm the requested Design system installation", created.ID)
		}
	}
	if state.NoDesignSystem && created.DesignSystemInstallation != nil {
		return nil, fmt.Errorf("Design %s was created but the server applied a Design system despite the decline", created.ID)
	}
	state.Workspace = workspace.ID
	state.Manifest = created.CurrentVersion.AuthoringManifest
	return &api.ArtifactPushResult{
		Outcome: "created", ArtifactID: created.ID, HeadRevision: created.HeadRevision,
		Version: created.CurrentVersion, BuildState: created.Preview.Status, Warnings: created.Warnings,
	}, nil
}

func pushDesignFiles(ctx context.Context, cli *CLI, client *api.Client, root *os.Root, state *designState, base map[string]string, files map[string]designFile, summary string, force, wait bool) (*api.ArtifactPushResult, bool, error) {
	textBase := base
	if state.FirstPushPending {
		textBase = nil
	}
	input := api.ArtifactPushInput{
		BaseVersionID: state.BaseVersionID, ExpectedHeadRevision: state.HeadRevision,
		Tool: designTool(), ChangeSummary: summary, Files: designTextFiles(files, textBase),
	}
	for _, name := range slices.Sorted(maps.Keys(base)) {
		canonical := designCanonicalAssetPath(name, state.AssetPaths)
		if file, found := files[name]; found {
			// A tracked vendor source stays source until explicitly admitted.
			// Converting it to a pin removes the previous source at that path.
			wasPinned := slices.ContainsFunc(state.Manifest.Assets, func(asset api.ArtifactAsset) bool {
				return asset.Path == canonical
			})
			if !file.Binary || !vendorDesignAssetMediaType(file.MediaType) || wasPinned ||
				!state.PendingAssets[canonical].matches(file.checksum()) {
				continue
			}
		}
		if err := validateDesignContentPath(canonical); err != nil {
			return nil, false, err
		}
		input.RemovedPaths = append(input.RemovedPaths, canonical)
	}
	manifest, aliases, err := pushDesignAssets(ctx, client, root, state, files)
	if err != nil {
		return nil, false, err
	}
	if force {
		shared := &manifest.SharedDesign
		for _, index := range []*string{&shared.ComponentsPath, &shared.AssetsPath} {
			if slices.Contains(input.RemovedPaths, *index) {
				*index = ""
			}
		}
		project := designProject(state)
		input.Project = &project
	} else if state.Capabilities != nil {
		input.Project = &api.ArtifactProjectSettings{Capabilities: state.Capabilities}
	}
	if force || !slices.Equal(state.Manifest.Assets, manifest.Assets) || !slices.Equal(state.Manifest.Documents, manifest.Documents) {
		input.AuthoringManifest = &manifest
	}
	input.IdempotencyKey, err = designPushKey("", input)
	if err != nil {
		return nil, false, err
	}
	result, err := sendDesignPush(ctx, cli, client, state.ID, input, wait)
	if err != nil {
		return nil, false, err
	}
	state.AssetPaths = aliases
	state.Manifest = manifest
	return result, input.AuthoringManifest == nil && result.Outcome != "unchanged", nil
}

func pushDesignAssets(ctx context.Context, client *api.Client, root *os.Root, state *designState, files map[string]designFile) (api.ArtifactManifest, map[string]string, error) {
	manifest := designManifestForFiles(state, files)
	assets := make(map[string]api.ArtifactAsset, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		assets[asset.Path] = asset
	}
	aliases := make(map[string]string, len(state.AssetPaths))
	names := make(designPaths)
	seenAssets := make(map[string]string)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if !files[name].Binary {
			if err := names.add(name, false); err != nil {
				return manifest, nil, err
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := files[name]
		if !file.Binary {
			continue
		}
		canonical := designCanonicalAssetPath(name, state.AssetPaths)
		if err := validateDesignContentPath(canonical); err != nil {
			return manifest, nil, err
		}
		asset := assets[canonical]
		digest := file.checksum()
		if asset.ID == "" || asset.ContentDigest != digest {
			pending := state.PendingAssets[canonical]
			if !pending.matches(digest) {
				uploaded, err := client.ArtifactAssetUpload(ctx, state.ID, file.uploadPart(canonical), asset.Usage, asset.AltText)
				if err != nil {
					return manifest, nil, fmt.Errorf("upload %s: %w", name, err)
				}
				if err := rememberDesignAsset(root, state, canonical, uploaded.ArtifactAsset); err != nil {
					return manifest, nil, err
				}
				pending = state.PendingAssets[canonical]
			}
			manifest.Assets = slices.DeleteFunc(manifest.Assets, func(a api.ArtifactAsset) bool { return a.Path == canonical })
			manifest.Assets = append(manifest.Assets, pending.ArtifactAsset)
		}
		if other, exists := seenAssets[canonical]; exists {
			return manifest, nil, fmt.Errorf("assets %q and %q share the server path %q", other, name, canonical)
		}
		if file, exists := files[canonical]; exists && !file.Binary {
			return manifest, nil, fmt.Errorf("asset %q conflicts with text file %q", name, canonical)
		}
		seenAssets[canonical] = name
		if err := names.add(canonical, false); err != nil {
			return manifest, nil, err
		}
		if canonical != name {
			aliases[name] = canonical
		}
	}
	return manifest, aliases, nil
}

func rememberDesignAsset(root *os.Root, state *designState, name string, asset api.ArtifactAsset) error {
	if asset.Path != name {
		return fmt.Errorf("asset upload returned path %q for %q", asset.Path, name)
	}
	record := pendingDesignAsset{ArtifactAsset: asset, UploadedAt: time.Now()}
	if previous := state.PendingAssets[name]; previous.ID == asset.ID {
		record.UploadedAt = previous.UploadedAt
	}
	if state.PendingAssets == nil {
		state.PendingAssets = make(map[string]pendingDesignAsset)
	}
	state.PendingAssets[name] = record
	// Keep the saved base intact if --force read a newer head for this push.
	release, err := lockDesignState(root, true)
	if err != nil {
		return err
	}
	defer release()
	saved, err := loadDesignState(root)
	if err != nil {
		return err
	}
	if saved.ID != state.ID {
		return fmt.Errorf("the linked Design changed while saving asset %q", name)
	}
	if saved.PendingAssets == nil {
		saved.PendingAssets = state.PendingAssets
	} else {
		saved.PendingAssets[name] = record
		state.PendingAssets = saved.PendingAssets
	}
	return writeDesignStateLocked(root, saved)
}

func designPushKey(prefix string, input interface{}) (string, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return sha256Hex(append([]byte(prefix), body...)), nil
}

func sendDesignPush(ctx context.Context, cli *CLI, client *api.Client, id string, input api.ArtifactPushInput, wait bool) (*api.ArtifactPushResult, error) {
	if !wait {
		result, err := client.ArtifactPush(ctx, id, input)
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict &&
			apiErr.Reason == "writer_busy" && apiErr.RetryAfterSeconds > 0 {
			busy := *apiErr
			busy.Message = fmt.Sprintf("%s (retry in %d seconds)", apiErr.Message, apiErr.RetryAfterSeconds)
			return result, &busy
		}
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var busy error
	for {
		result, err := client.ArtifactPush(ctx, id, input)
		var apiErr *api.APIError
		if busy != nil && errors.Is(err, context.DeadlineExceeded) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, busy
		}
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict || apiErr.Reason != "writer_busy" {
			return result, err
		}
		busy = err
		seconds := max(apiErr.RetryAfterSeconds, 1)
		if seconds > 600 {
			return nil, err
		}
		delay := time.Duration(seconds) * time.Second
		if time.Until(deadline) <= delay {
			return nil, err
		}
		cli.Printer.Info("Copera AI is editing this Design. Retrying the push in %s.", delay)
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, busy
			}
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func waitForDesignPreview(ctx context.Context, cli *CLI, client *api.Client, pushed *api.ArtifactPushResult, page, progress string) error {
	id, version := pushed.ArtifactID, pushed.Version.ID
	wait := max(0, min(20, int(client.HTTPClient().Timeout/time.Second)-1))
	unsupported := errors.New("the API does not support waiting for a Design preview. Use --wait to wait for build evidence")
	for {
		build, err := client.ArtifactBuildForPreview(ctx, id, version, wait)
		if err != nil {
			var apiErr *api.APIError
			if errors.As(err, &apiErr) {
				if wait > 0 && apiErr.StatusCode == http.StatusTooManyRequests {
					wait = 0
					continue
				}
				if apiErr.StatusCode == http.StatusBadRequest && apiErr.Code != "NOT_FOUND" {
					return designPushError(cli, unsupported, progress)
				}
			}
			return designPushError(cli, err, progress)
		}
		if build.EvidenceStatus == "" {
			return designPushError(cli, unsupported, progress)
		}
		if build.PreviewReadyAt != nil || build.State == "ready" {
			if page == "" {
				exact, err := client.ArtifactVersionGet(ctx, id, version)
				if err != nil {
					return designPushError(cli, err, progress)
				}
				page = exact.AuthoringManifest.DefaultPageID
				if page == "" {
					return designPushError(cli, errors.New("the saved Design version has no default page"), progress)
				}
			}
			preview, err := client.ArtifactPagePreview(ctx, id, version, page)
			if err != nil {
				return designPushError(cli, err, progress)
			}
			if preview.Status == "ready" && preview.URL != "" {
				appURL, err := designAppURL(ctx, client, id)
				if err != nil {
					cli.Printer.Info("Version %s was saved. Run copera designs open %s for its URL: %s",
						designsTerminalLine(version), designsTerminalLine(id), designsTerminalLine(err.Error()))
				}
				if cli.Printer.IsJSON() {
					return cli.Printer.PrintJSON(struct {
						ArtifactID     string     `json:"artifactId"`
						VersionID      string     `json:"versionId"`
						State          string     `json:"state"`
						PreviewReadyAt *time.Time `json:"previewReadyAt"`
						EvidenceStatus string     `json:"evidenceStatus"`
						URL            string     `json:"url"`
						AppURL         string     `json:"appUrl,omitempty"`
					}{id, version, build.State, build.PreviewReadyAt, build.EvidenceStatus, preview.URL, appURL})
				}
				cli.Printer.PrintLine(designsTerminalLine(preview.URL))
				if appURL != "" {
					cli.Printer.PrintLine("URL: " + designsTerminalLine(appURL))
				}
				return nil
			}
		} else if build.Done {
			message := "Design build " + build.State
			if build.FailureCode != "" {
				message += " (" + build.FailureCode + ")"
			}
			return designPushError(cli, errors.New(message), progress)
		} else if wait > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return designPushError(cli, ctx.Err(), progress)
		case <-time.After(time.Second):
		}
	}
}

func waitForDesignBuild(ctx context.Context, cli *CLI, client *api.Client, root *os.Root, state *designState, version *api.ArtifactVersion, check *designBuildReport, vendorHint func(*designBuildReport), progress, appURL string) error {
	wait := max(0, min(20, int(client.HTTPClient().Timeout/time.Second)-1))
	for {
		build, err := client.ArtifactBuild(ctx, state.ID, state.BaseVersionID, wait)
		if err != nil {
			var apiErr *api.APIError
			if wait > 0 && errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
				wait = 0
				continue
			}
			return designPushError(cli, err, progress)
		}
		if !build.Done {
			if wait == 0 {
				select {
				case <-ctx.Done():
					return designPushError(cli, ctx.Err(), progress)
				case <-time.After(time.Second):
				}
			}
			continue
		}

		report := &designBuildReport{
			State: build.State, FailureCode: build.FailureCode, ArtifactBuildFindings: build.ArtifactBuildFindings,
			Version: version, AppURL: appURL,
		}
		vendorHint(report)
		if check != nil {
			report.Screenshots = check.Screenshots
			report.ScreenshotPages = check.ScreenshotPages
			report.ScreenshotsOmittedPageIDs = check.ScreenshotsOmittedPageIDs
		}
		defaultPage := state.Manifest.DefaultPageID
		if defaultPage == "" {
			defaultPage = version.AuthoringManifest.DefaultPageID
		}
		if defaultPage == "" && len(build.Screenshots) > 0 {
			defaultPage = build.Screenshots[0].PageID
		}
		for _, shot := range build.Screenshots {
			data, err := downloadHandoffAsset(ctx, client, shot.URL)
			if err != nil {
				if build.State != "ready" {
					return designPushError(cli, fmt.Errorf("download Design screenshot: %w", err), progress)
				}
				cli.Printer.Info("Warning: download Design screenshot: %s", err)
				continue
			}
			scheme := ""
			if shot.PageID != defaultPage {
				scheme = "light"
			}
			name, err := saveDesignShot(root, shot.PageID, shot.Width, scheme, data)
			if err != nil {
				if build.State != "ready" {
					return designPushError(cli, err, progress)
				}
				cli.Printer.Info("Warning: save Design screenshot: %s", err)
				continue
			}
			report.addScreenshot(shot.PageID, shot.Route, name)
		}
		return printDesignBuild(cli, report, progress)
	}
}
