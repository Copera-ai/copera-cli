package commands

import (
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
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

func newDesignPushCmd(cli *CLI) *cobra.Command {
	var summary string
	var wait, noCheck, force bool
	cmd := &cobra.Command{
		Use:   "push [dir] -m SUMMARY",
		Short: "Save local edits as one new Design version",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(summary) == "" || utf8.RuneCountInString(summary) > 160 || strings.ContainsAny(summary, "\r\n") {
				msg := "change summary must be one line of 1 to 160 characters"
				cli.Printer.PrintError("invalid_flag", msg, "", false)
				return exitcodes.Newf(exitcodes.Usage, "%s", msg)
			}
			root, state, files, err := openDesignForBuild(args)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			base := state.Files
			if force && state.ID != "" {
				head, err := client.ArtifactGet(ctx, state.ID)
				if err != nil {
					return designPushError(cli, err, designPushProgress(state))
				}
				base = designHeadFiles(&head.CurrentVersion, state.AssetPaths)
				state.BaseVersionID, state.HeadRevision = head.CurrentVersion.ID, head.HeadRevision
			}
			if !noCheck {
				report, err := checkDesign(ctx, client, root, state, files, force)
				if err != nil {
					return designPushError(cli, err, designPushProgress(state))
				}
				if report.State != "ready" {
					return printDesignBuild(cli, report, designPushProgress(state))
				}
			}
			var pushed *api.ArtifactPushResult
			if state.ID == "" {
				pushed, err = createLocalDesign(ctx, client, root, state, files, summary)
			} else {
				pushed, err = pushDesignFiles(ctx, cli, client, root, state, base, files, summary, force, wait)
			}
			if err != nil {
				return designPushError(cli, err, designPushProgress(state))
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
			if wait {
				return waitForDesignBuild(ctx, cli, client, root, state, progress)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(pushed)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Version: %d (%s)", pushed.Version.Version, pushed.Version.ID))
			cli.Printer.PrintLine("Build: " + pushed.BuildState)
			return nil
		},
	}
	cmd.Flags().StringVarP(&summary, "message", "m", "", "Summary of the changes (up to 160 characters)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for the writer and the resulting build")
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

func createLocalDesign(ctx context.Context, client *api.Client, root *os.Root, state *designState, files map[string]designFile, summary string) (*api.ArtifactPushResult, error) {
	dir, err := filepath.Abs(root.Name())
	if err != nil {
		return nil, err
	}
	input := api.ArtifactCreateInput{Title: filepath.Base(dir), ChangeSummary: summary, Tool: designTool(), DesignSystemID: state.DesignSystemID}
	input.Project.ArtifactProjectSettings = designProject(state)
	input.Project.SchemaVersion = 1
	input.Project.Files = designTextFiles(files, nil)
	if len(input.Project.Files) == 0 {
		return nil, fmt.Errorf("add source files before pushing the Design")
	}
	var binaries []api.ArtifactCheckFile
	keyParts := []string{dir, summary}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		file := files[name]
		if file.Binary {
			binaries = append(binaries, api.ArtifactCheckFile{Path: name, MediaType: file.MediaType, Data: file.Data})
			keyParts = append(keyParts, name, sha256Hex(file.Data))
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
	state.Workspace = workspace.ID
	state.Manifest = created.CurrentVersion.AuthoringManifest
	return &api.ArtifactPushResult{
		Outcome: "created", ArtifactID: created.ID, HeadRevision: created.HeadRevision,
		Version: created.CurrentVersion, BuildState: created.Preview.Status, Warnings: created.Warnings,
	}, nil
}

func pushDesignFiles(ctx context.Context, cli *CLI, client *api.Client, root *os.Root, state *designState, base map[string]string, files map[string]designFile, summary string, force, wait bool) (*api.ArtifactPushResult, error) {
	textBase := base
	if state.FirstPushPending {
		textBase = nil
	}
	input := api.ArtifactPushInput{
		BaseVersionID: state.BaseVersionID, ExpectedHeadRevision: state.HeadRevision,
		Tool: designTool(), ChangeSummary: summary, Files: designTextFiles(files, textBase),
	}
	for _, name := range slices.Sorted(maps.Keys(base)) {
		if _, found := files[name]; found {
			continue
		}
		canonical := designCanonicalAssetPath(name, state.AssetPaths)
		if err := validateDesignContentPath(canonical); err != nil {
			return nil, err
		}
		input.RemovedPaths = append(input.RemovedPaths, canonical)
	}
	manifest, aliases, err := pushDesignAssets(ctx, client, root, state, files)
	if err != nil {
		return nil, err
	}
	if force || !slices.Equal(state.Manifest.Assets, manifest.Assets) || !slices.Equal(state.Manifest.Documents, manifest.Documents) {
		input.AuthoringManifest = &manifest
	}
	if force {
		project := designProject(state)
		input.Project = &project
	}
	input.IdempotencyKey, err = designPushKey("", input)
	if err != nil {
		return nil, err
	}
	result, err := sendDesignPush(ctx, cli, client, state.ID, input, wait)
	if err != nil {
		return nil, err
	}
	if input.AuthoringManifest == nil && result.Outcome != "unchanged" {
		version, err := client.ArtifactVersionGet(ctx, state.ID, result.Version.ID)
		if err != nil {
			cli.Printer.Info("Version %s was saved. Run designs pull to refresh the local manifest: %s", result.Version.ID, err)
		} else {
			manifest = version.AuthoringManifest
		}
	}
	state.AssetPaths = aliases
	state.Manifest = manifest
	return result, nil
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
		digest := sha256Hex(file.Data)
		if asset.ID == "" || asset.ContentDigest != digest {
			pending := state.PendingAssets[canonical]
			if !pending.matches(digest) {
				uploaded, err := client.ArtifactAssetUpload(ctx, state.ID, canonical, file.Data, asset.Usage, asset.AltText)
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
	saved, err := loadDesignState(root)
	if err != nil {
		return err
	}
	if saved.PendingAssets == nil {
		saved.PendingAssets = state.PendingAssets
	} else {
		saved.PendingAssets[name] = record
		state.PendingAssets = saved.PendingAssets
	}
	return writeDesignState(root, saved)
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
		return client.ArtifactPush(ctx, id, input)
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

func waitForDesignBuild(ctx context.Context, cli *CLI, client *api.Client, root *os.Root, state *designState, progress string) error {
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
		}
		for _, shot := range build.Screenshots {
			data, err := downloadHandoffAsset(ctx, client, shot.URL)
			if err != nil {
				return designPushError(cli, fmt.Errorf("download Design screenshot: %w", err), progress)
			}
			name, err := saveDesignShot(root, shot.PageID, shot.Width, data)
			if err != nil {
				return designPushError(cli, err, progress)
			}
			report.Screenshots = append(report.Screenshots, name)
		}
		return printDesignBuild(cli, report, progress)
	}
}
