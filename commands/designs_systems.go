package commands

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/output"
	"github.com/spf13/cobra"
)

type designSystemTracking struct {
	SystemID      string `json:"systemId"`
	Revision      int    `json:"revision"`
	PackageDigest string `json:"packageDigest"`
	Workspace     string `json:"workspace"`
}

type designSystemTransferResult struct {
	SystemID      string `json:"systemId"`
	Revision      int    `json:"revision"`
	PackageDigest string `json:"packageDigest"`
	Path          string `json:"path"`
}

func systemUsage(cli *CLI, err error) error {
	cli.Printer.PrintError("invalid_usage", err.Error(), "", false)
	return exitcodes.New(exitcodes.Usage, err)
}

func systemArgs(cli *CLI, validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return systemUsage(cli, err)
		}
		return nil
	}
}

func systemFlags(cli *CLI) func(*cobra.Command, error) error {
	return func(cmd *cobra.Command, err error) error {
		// Cobra parses flags before the root initializes cli.Printer.
		if cli.Printer == nil {
			cli.Printer = output.New(output.FormatJSON, cmd.OutOrStdout(), cmd.ErrOrStderr(), false)
		}
		return systemUsage(cli, err)
	}
}

func systemFeatureUnavailable(err error) bool {
	var refused *api.APIError
	return errors.As(err, &refused) && refused.Code == "" &&
		(refused.StatusCode == http.StatusNotFound || refused.StatusCode == http.StatusMethodNotAllowed)
}

func systemAPIError(cli *CLI, err error) error {
	var refused *api.APIError
	if errors.As(err, &refused) {
		switch {
		case systemFeatureUnavailable(refused):
			cli.Printer.PrintError("design_system_feature_required", "The server needs Phase 3 Design systems support for this operation.", "", false)
			return exitcodes.New(refused.ExitCode(), err)
		case refused.CurrentRevision > 0 || refused.Reason != "" || len(refused.ChangedPaths) != 0:
			var details []string
			if refused.Reason != "" {
				details = append(details, refused.Reason)
			}
			if refused.CurrentRevision > 0 {
				details = append(details, "Current revision: "+strconv.Itoa(refused.CurrentRevision))
			}
			if len(refused.ChangedPaths) != 0 {
				details = append(details, "Changed paths: "+strings.Join(refused.ChangedPaths, ", "))
			}
			cli.Printer.PrintError("api_error", refused.Error(), strings.Join(details, ". "), false)
			return exitcodes.New(refused.ExitCode(), err)
		}
	}
	return designError(cli, err)
}

func systemConflict(cli *CLI, message string) error {
	cli.Printer.PrintError("design_system_conflict", message, "", false)
	return exitcodes.Newf(exitcodes.Conflict, "%s", message)
}

func systemID(value string) error {
	if !validSystemHex(value, 24) {
		return fmt.Errorf("expected a 24-character lowercase design system ID")
	}
	return nil
}

func systemName(value string) bool {
	return utf8.ValidString(value) && value != "" && utf8.RuneCountInString(value) <= 80
}

func systemFormat(value string) bool {
	switch value {
	case "package", "tokens", "guidance", "shadcn":
		return true
	default:
		return false
	}
}

func printSystemSummary(cli *CLI, summary *api.ArtifactDesignSystem) error {
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(summary)
	}
	printSystemSummaryLine(cli, summary)
	if summary.PackageDigest != nil {
		cli.Printer.PrintLine("Package digest: " + *summary.PackageDigest)
	}
	return nil
}

func printSystemSummaryLine(cli *CLI, summary *api.ArtifactDesignSystem) {
	status := "available"
	if !summary.Available {
		status = "unavailable"
		if summary.UnavailableReason != nil {
			status += ": " + *summary.UnavailableReason
		}
	}
	cli.Printer.PrintLine(fmt.Sprintf("%s  %s  revision %d  %s", summary.Name, summary.ID, summary.Revision, status))
}

func printSystemTransfer(cli *CLI, transfer *api.ArtifactSystemTransfer, filename string) error {
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(designSystemTransferResult{
			SystemID: transfer.SystemID, Revision: transfer.Revision, PackageDigest: transfer.PackageDigest, Path: filename,
		})
	}
	cli.Printer.PrintLine(fmt.Sprintf("Saved system %s revision %d to %s", transfer.SystemID, transfer.Revision, filename))
	return nil
}

func newDesignSystemsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "systems", Short: "Manage portable Design systems"}
	cmd.AddCommand(
		newDesignSystemListCmd(cli),
		newDesignSystemGetCmd(cli),
		newDesignSystemPullCmd(cli),
		newDesignSystemPushCmd(cli),
		newDesignSystemSaveCmd(cli),
		newDesignSystemDefaultsCmd(cli),
		newDesignSystemSetDefaultCmd(cli),
		newDesignSystemExportCmd(cli),
		newDesignSystemImportCmd(cli),
		newDesignSystemRefreshCmd(cli),
	)
	for _, subcommand := range cmd.Commands() {
		subcommand.SetFlagErrorFunc(systemFlags(cli))
	}
	return cmd
}

func newDesignSystemListCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List saved Design systems", Args: systemArgs(cli, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactDesignSystems(cmd.Context())
			if err != nil {
				return systemAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			for _, summary := range result.DesignSystems {
				printSystemSummaryLine(cli, &summary)
			}
			return nil
		},
	}
}

func newDesignSystemGetCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use: "get <id>", Short: "Show a saved Design system", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := systemID(args[0]); err != nil {
				return systemUsage(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactDesignSystem(cmd.Context(), args[0])
			if err != nil {
				return systemAPIError(cli, err)
			}
			return printSystemSummary(cli, result)
		},
	}
}

func systemTransfer(client *api.Client, cmd *cobra.Command, id, format string, revision int) (*api.ArtifactSystemTransfer, error) {
	result, err := client.ArtifactDesignSystemPackage(cmd.Context(), id, format, revision)
	if err != nil {
		return nil, err
	}
	if result.SystemID != id || result.Revision < 1 || (revision > 0 && result.Revision != revision) ||
		!validSystemHex(result.PackageDigest, 64) || result.Format != format {
		return nil, fmt.Errorf("the server returned an unexpected Design system revision or format")
	}
	return result, nil
}

func systemTransferContent(transfer *api.ArtifactSystemTransfer) (*designSystemContent, error) {
	content, err := decodeSystemPackage(transfer.Document)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalSystemManifest(content.manifest)
	if err != nil {
		return nil, err
	}
	if sha256Hex(canonical) != transfer.PackageDigest {
		return nil, fmt.Errorf("the Design system package does not match its checksum")
	}
	return content, nil
}

func newDesignSystemPullCmd(cli *CLI) *cobra.Command {
	var revision int
	cmd := &cobra.Command{
		Use: "pull <id> <dir>", Short: "Copy a revision into a new or empty folder", Args: systemArgs(cli, cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := systemID(args[0]); err != nil {
				return systemUsage(cli, err)
			}
			if cmd.Flags().Changed("expected-revision") && revision < 1 {
				return systemUsage(cli, fmt.Errorf("expected revision must be positive"))
			}
			dir := filepath.Clean(args[1])
			if files, err := os.ReadDir(dir); err == nil && len(files) != 0 {
				return systemConflict(cli, fmt.Sprintf("system folder %q is not empty", dir))
			} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := systemTransfer(client, cmd, args[0], "package", revision)
			if err != nil {
				return systemAPIError(cli, err)
			}
			content, err := systemTransferContent(result)
			if err != nil {
				return designError(cli, err)
			}
			workspace, err := client.WorkspaceInfo(cmd.Context())
			if err != nil {
				return designError(cli, err)
			}
			if !validSystemHex(workspace.ID, 24) {
				return designError(cli, fmt.Errorf("the server did not identify the workspace for the Design system"))
			}
			root, err := openDesignDir(dir, true)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			entries, err := fs.ReadDir(root.FS(), ".")
			if err != nil {
				return designError(cli, err)
			}
			if len(entries) != 0 {
				return systemConflict(cli, fmt.Sprintf("system folder %q is not empty", dir))
			}
			for _, entry := range content.manifest.Files {
				if err := writeDesignFile(root, entry.Path, content.files[entry.Path].Data); err != nil {
					return designError(cli, err)
				}
			}
			tracking := designSystemTracking{SystemID: result.SystemID, Revision: result.Revision, PackageDigest: result.PackageDigest, Workspace: workspace.ID}
			if err := writeSystemTracking(root, content, tracking); err != nil {
				return designError(cli, err)
			}
			return printSystemTransfer(cli, result, dir)
		},
	}
	cmd.Flags().IntVar(&revision, "expected-revision", 0, "Only pull this saved revision")
	return cmd
}

func writeSystemTracking(root *os.Root, content *designSystemContent, tracking designSystemTracking) error {
	manifest, err := canonicalSystemManifest(content.manifest)
	if err != nil {
		return err
	}
	metadata, err := json.MarshalIndent(tracking, "", "  ")
	if err != nil {
		return err
	}
	if err := writeDesignFile(root, designSystemManifest, append(manifest, '\n')); err != nil {
		return err
	}
	return writeDesignFile(root, designSystemLocal, append(metadata, '\n'))
}

func readSystemTracking(root *os.Root) (*designSystemTracking, error) {
	data, err := readRootSystemFile(root, designSystemLocal, 4<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tracking designSystemTracking
	if err := decodeSystemJSON(data, &tracking); err != nil {
		return nil, err
	}
	if systemID(tracking.SystemID) != nil || !validSystemHex(tracking.PackageDigest, 64) ||
		!validSystemHex(tracking.Workspace, 24) || tracking.Revision < 1 {
		return nil, fmt.Errorf("invalid saved Design system tracking metadata")
	}
	return &tracking, nil
}

func systemCreate(ctx context.Context, client *api.Client, name, format string, document json.RawMessage) (*api.ArtifactDesignSystem, error) {
	input := api.ArtifactSystemCreate{Name: name, ArtifactSystemDocument: api.ArtifactSystemDocument{Format: format, Document: document}}
	var err error
	input.IdempotencyKey, err = designPushKey(rand.Text(), input)
	if err != nil {
		return nil, err
	}
	return client.ArtifactDesignSystemCreate(ctx, input)
}

func newDesignSystemPushCmd(cli *CLI) *cobra.Command {
	var name, id string
	var revision int
	cmd := &cobra.Command{
		Use: "push <dir>", Short: "Create or replace a saved Design system from local files", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(name)
			if cmd.Flags().Changed("name") && !systemName(name) {
				return systemUsage(cli, fmt.Errorf("name must have 1 to 80 characters"))
			}
			idSet, revisionSet := cmd.Flags().Changed("system"), cmd.Flags().Changed("expected-revision")
			if idSet != revisionSet || (revisionSet && (revision < 1 || systemID(id) != nil)) {
				return systemUsage(cli, fmt.Errorf("provide a system ID and positive expected revision together"))
			}
			root, err := openDesignDir(args[0], false)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			tracking, err := readSystemTracking(root)
			if err != nil {
				return systemUsage(cli, err)
			}
			targetID, targetRevision := id, revision
			if tracking != nil {
				if idSet {
					return systemUsage(cli, fmt.Errorf("this folder already tracks a system revision"))
				}
				targetID, targetRevision = tracking.SystemID, tracking.Revision
			}
			if targetID == "" && !systemName(name) {
				return systemUsage(cli, fmt.Errorf("name is required when creating a system"))
			}
			content, err := readSystemFolder(root)
			if err != nil {
				return systemUsage(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			workspace, err := client.WorkspaceInfo(ctx)
			if err != nil {
				return designError(cli, err)
			}
			if !validSystemHex(workspace.ID, 24) {
				return designError(cli, fmt.Errorf("the server did not identify the workspace for the Design system"))
			}
			if tracking != nil && tracking.Workspace != workspace.ID {
				return systemConflict(cli, "This folder tracks a Design system in another workspace.")
			}
			canonical, err := canonicalSystemManifest(content.manifest)
			if err != nil {
				return designError(cli, err)
			}
			pack := content.packageDocument()
			var result *api.ArtifactDesignSystem
			if targetID == "" {
				document, err := json.Marshal(pack)
				if err != nil {
					return designError(cli, err)
				}
				result, err = systemCreate(ctx, client, name, "package", document)
				if err != nil {
					return systemAPIError(cli, err)
				}
			} else {
				result, err = client.ArtifactDesignSystemReplace(ctx, targetID, api.ArtifactSystemReplace{
					ExpectedRevision: targetRevision, Name: name, Package: pack,
				})
				if err != nil {
					return systemAPIError(cli, err)
				}
			}
			if result.PackageDigest == nil || !validSystemHex(*result.PackageDigest, 64) ||
				systemID(result.ID) != nil || result.Revision < 1 {
				return designError(cli, fmt.Errorf("the server saved the system but did not return a valid revision"))
			}
			if (targetID != "" && result.ID != targetID) || *result.PackageDigest != sha256Hex(canonical) {
				return systemConflict(cli, fmt.Sprintf("The server returned system %s revision %d with different content. Pull it into a new folder before replacing it.", result.ID, result.Revision))
			}
			metadata := designSystemTracking{SystemID: result.ID, Revision: result.Revision, PackageDigest: *result.PackageDigest, Workspace: workspace.ID}
			if err := writeSystemTracking(root, content, metadata); err != nil {
				return designError(cli, fmt.Errorf("system %s revision %d was saved, but local tracking could not be updated: %w", result.ID, result.Revision, err))
			}
			return printSystemSummary(cli, result)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name for the saved Design system")
	cmd.Flags().StringVar(&id, "system", "", "ID to replace from an untracked folder")
	cmd.Flags().IntVar(&revision, "expected-revision", 0, "Revision to replace from an untracked folder")
	return cmd
}

func newDesignSystemSaveCmd(cli *CLI) *cobra.Command {
	var name, version string
	cmd := &cobra.Command{
		Use: "save-from <design>", Short: "Save the brand from a Design version", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return systemUsage(cli, err)
			}
			name := strings.TrimSpace(name)
			if !systemName(name) {
				return systemUsage(cli, fmt.Errorf("name must have 1 to 80 characters"))
			}
			if cmd.Flags().Changed("version") && !validSystemHex(version, 24) {
				return systemUsage(cli, fmt.Errorf("expected a 24-character lowercase version ID"))
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			versionID := version
			if versionID == "" {
				head, err := client.ArtifactGet(cmd.Context(), id)
				if err != nil {
					return designError(cli, err)
				}
				versionID = head.CurrentDraftVersionID
				if versionID == "" {
					return designError(cli, fmt.Errorf("the Design does not have a draft version"))
				}
			}
			result, err := client.ArtifactDesignSystemSave(cmd.Context(), api.ArtifactSystemSave{ArtifactID: id, VersionID: versionID, Name: name})
			if err != nil {
				return systemAPIError(cli, err)
			}
			return printSystemSummary(cli, result)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name for the saved Design system")
	cmd.Flags().StringVar(&version, "version", "", "Version ID (defaults to the current draft)")
	return cmd
}

func printSystemDefaults(cli *CLI, result *api.ArtifactSystemDefaults) error {
	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(result)
	}
	personal, workspace := "none", "none"
	if result.PersonalSystemID != nil {
		personal = *result.PersonalSystemID
	}
	if result.WorkspaceSystemID != nil {
		workspace = *result.WorkspaceSystemID
	}
	cli.Printer.PrintLine("Personal default: " + personal)
	cli.Printer.PrintLine("Workspace default: " + workspace)
	cli.Printer.PrintLine(fmt.Sprintf("Workspace lock: %t", result.Locked))
	return nil
}

func newDesignSystemDefaultsCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use: "defaults", Short: "Show personal and workspace Design system defaults", Args: systemArgs(cli, cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactDesignSystemDefaults(cmd.Context())
			if err != nil {
				return systemAPIError(cli, err)
			}
			return printSystemDefaults(cli, result)
		},
	}
}

func newDesignSystemSetDefaultCmd(cli *CLI) *cobra.Command {
	var scope string
	var lock, unlock bool
	cmd := &cobra.Command{
		Use: "set-default <id-or-none>", Short: "Select or clear a Design system default", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			lockFlag, unlockFlag := cmd.Flags().Changed("lock"), cmd.Flags().Changed("unlock")
			if (scope != "personal" && scope != "workspace") ||
				(scope != "workspace" && (lockFlag || unlockFlag)) || (lockFlag && unlockFlag) {
				return systemUsage(cli, fmt.Errorf("scope must be personal or workspace; choose at most one lock option with workspace scope"))
			}
			input := api.ArtifactSystemDefaultInput{Scope: scope}
			if args[0] != "none" {
				if err := systemID(args[0]); err != nil {
					return systemUsage(cli, err)
				}
				input.SystemID = &args[0]
			}
			if lockFlag {
				input.Locked = &lock
			}
			if unlockFlag {
				locked := !unlock
				input.Locked = &locked
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactDesignSystemSetDefault(cmd.Context(), input)
			if err != nil {
				return systemAPIError(cli, err)
			}
			return printSystemDefaults(cli, result)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "personal", "Default scope (personal or workspace)")
	cmd.Flags().BoolVar(&lock, "lock", false, "Require the workspace default for new Designs")
	cmd.Flags().BoolVar(&unlock, "unlock", false, "Allow a different system on new Designs")
	return cmd
}

func newDesignSystemExportCmd(cli *CLI) *cobra.Command {
	var format, output string
	var revision int
	cmd := &cobra.Command{
		Use: "export <id>", Short: "Export a package, shadcn theme, tokens or brand book", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := systemID(args[0]); err != nil {
				return systemUsage(cli, err)
			}
			if output == "" || !systemFormat(format) || (cmd.Flags().Changed("expected-revision") && revision < 1) {
				return systemUsage(cli, fmt.Errorf("output and valid format are required; expected revision must be positive if supplied"))
			}
			root, err := openDesignDir(filepath.Dir(output), false)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			filename := filepath.Base(output)
			if _, err := root.Lstat(filename); err == nil {
				return systemConflict(cli, fmt.Sprintf("output file %q already exists", output))
			} else if !errors.Is(err, fs.ErrNotExist) {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := systemTransfer(client, cmd, args[0], format, revision)
			if err != nil {
				return systemAPIError(cli, err)
			}
			data, err := systemExportBytes(result)
			if err != nil {
				return designError(cli, err)
			}
			file, err := root.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
			if errors.Is(err, fs.ErrExist) {
				return systemConflict(cli, fmt.Sprintf("output file %q already exists", output))
			}
			if err != nil {
				return designError(cli, err)
			}
			_, writeErr := file.Write(data)
			if err := errors.Join(writeErr, file.Close()); err != nil {
				return designError(cli, err)
			}
			return printSystemTransfer(cli, result, output)
		},
	}
	cmd.Flags().StringVar(&format, "format", "package", "package, shadcn, tokens or guidance")
	cmd.Flags().StringVarP(&output, "output", "o", "", "File to write")
	cmd.Flags().IntVar(&revision, "expected-revision", 0, "Only export this saved revision")
	return cmd
}

func systemExportBytes(transfer *api.ArtifactSystemTransfer) ([]byte, error) {
	switch transfer.Format {
	case "package":
		content, err := systemTransferContent(transfer)
		if err != nil {
			return nil, err
		}
		return writeSystemZIP(content)
	case "guidance":
		var guidance string
		if err := json.Unmarshal(transfer.Document, &guidance); err != nil {
			return nil, err
		}
		return []byte(guidance), nil
	default:
		if len(transfer.Document) == 0 || transfer.Document[0] != '{' || !json.Valid(transfer.Document) {
			return nil, fmt.Errorf("the server returned an invalid Design system document")
		}
		return append(transfer.Document, '\n'), nil
	}
}

func newDesignSystemImportCmd(cli *CLI) *cobra.Command {
	var name, format string
	cmd := &cobra.Command{
		Use: "import <file>", Short: "Create a Design system from a portable document", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(name)
			if !systemName(name) || !systemFormat(format) {
				return systemUsage(cli, fmt.Errorf("name must have 1 to 80 characters and format must be package, shadcn, tokens or guidance"))
			}
			document, err := systemImportDocument(args[0], format)
			if err != nil {
				return systemUsage(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := systemCreate(cmd.Context(), client, name, format, document)
			if err != nil {
				return systemAPIError(cli, err)
			}
			return printSystemSummary(cli, result)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Name for the imported Design system")
	cmd.Flags().StringVar(&format, "format", "package", "package, shadcn, tokens or guidance")
	return cmd
}

func systemImportDocument(filename, format string) (json.RawMessage, error) {
	var limit int64 = 64 << 10
	switch format {
	case "package":
		limit = 26 * designSystemMiB
	case "shadcn":
		limit = designSystemMiB
	}
	data, err := readSystemExchangeFile(filename, limit)
	if err != nil {
		return nil, err
	}
	switch format {
	case "package":
		content, err := readSystemZIP(data)
		if err != nil {
			return nil, err
		}
		return json.Marshal(content.packageDocument())
	case "guidance":
		if err := validateSystemBytes(filename, "text/markdown", data); err != nil {
			return nil, err
		}
		return json.Marshal(string(data))
	default:
		document := strings.TrimSpace(string(data))
		if !utf8.Valid(data) || !strings.HasPrefix(document, "{") || !json.Valid([]byte(document)) {
			return nil, fmt.Errorf("expected a JSON object in %q", filename)
		}
		return json.RawMessage(document), nil
	}
}

func designSystemMutationHead(ctx context.Context, client *api.Client, id string) (api.ArtifactDesignChangeHead, error) {
	var input api.ArtifactDesignChangeHead
	head, err := client.ArtifactGet(ctx, id)
	if err != nil {
		return input, err
	}
	if head.CurrentDraftVersionID == "" || head.HeadRevision < 1 {
		return input, fmt.Errorf("the Design does not have a draft head")
	}
	input.BaseVersionID, input.ExpectedHeadRevision = head.CurrentDraftVersionID, head.HeadRevision
	return input, nil
}

func newDesignSystemRefreshCmd(cli *CLI) *cobra.Command {
	var revision int
	cmd := &cobra.Command{
		Use: "refresh <design>", Short: "Apply a newer saved system revision to a Design", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if revision < 1 {
				return systemUsage(cli, fmt.Errorf("expected system revision must be positive"))
			}
			id, err := designID(args[0])
			if err != nil {
				return systemUsage(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			head, err := designSystemMutationHead(ctx, client, id)
			if err != nil {
				return designError(cli, err)
			}
			input := api.ArtifactSystemRefresh{ArtifactDesignChangeHead: head, ExpectedSystemRevision: revision}
			input.IdempotencyKey, err = designPushKey(rand.Text()+id, input)
			if err != nil {
				return designError(cli, err)
			}
			result, err := client.ArtifactDesignSystemRefresh(ctx, id, input)
			if err != nil {
				return systemAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				if err := cli.Printer.PrintJSON(result); err != nil {
					return err
				}
			} else {
				cli.Printer.PrintLine(fmt.Sprintf("Refreshed Design %s to system revision %d (%s)", id, revision, result.Outcome))
			}
			if err := pullRefreshedDesign(ctx, cli, client, result); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&revision, "expected-system-revision", 0, "Saved system revision to apply")
	return cmd
}

func newDesignShadcnCmd(cli *CLI) *cobra.Command {
	var setup, filename string
	setupCmd := &cobra.Command{
		Use: "setup <design>", Short: "Update the managed shadcn setup", Args: systemArgs(cli, cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("setup") == cmd.Flags().Changed("setup-file") {
				return systemUsage(cli, fmt.Errorf("provide exactly one of setup and setup-file"))
			}
			id, err := designID(args[0])
			if err != nil {
				return systemUsage(cli, err)
			}
			value := setup
			if cmd.Flags().Changed("setup-file") {
				data, err := readSystemExchangeFile(filename, 64<<10)
				if err != nil {
					return systemUsage(cli, err)
				}
				value = string(data)
			}
			if strings.TrimSpace(value) == "" || len(value) > 64<<10 || !utf8.ValidString(value) {
				return systemUsage(cli, fmt.Errorf("setup must contain 1 to 64 KiB of UTF-8 text"))
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			head, err := designSystemMutationHead(ctx, client, id)
			if err != nil {
				return designError(cli, err)
			}
			input := api.ArtifactShadcnSetup{ArtifactDesignChangeHead: head, Setup: value}
			input.IdempotencyKey, err = designPushKey(rand.Text()+id, input)
			if err != nil {
				return designError(cli, err)
			}
			result, err := client.ArtifactShadcnSetup(ctx, id, input)
			if err != nil {
				return systemAPIError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(result.Shadcn.Summary)
			for _, warning := range append(result.Shadcn.Warnings, result.Warnings...) {
				cli.Printer.PrintLine(warning)
			}
			return nil
		},
	}
	setupCmd.Flags().StringVar(&setup, "setup", "", "Preset, theme CSS or components.json")
	setupCmd.Flags().StringVar(&filename, "setup-file", "", "Read the setup text from a file")
	setupCmd.SetFlagErrorFunc(systemFlags(cli))
	cmd := &cobra.Command{Use: "shadcn", Short: "Manage a Design's shadcn setup"}
	cmd.AddCommand(setupCmd)
	return cmd
}
