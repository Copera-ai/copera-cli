package commands

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/upload"
	"github.com/spf13/cobra"
)

const designArchiveUploadLimit = 264 << 20

func newDesignAssetsCmd(cli *CLI) *cobra.Command {
	var pathFlag string
	cmd := &cobra.Command{Use: "assets", Short: "Manage a Design's assets"}
	add := &cobra.Command{
		Use:   "add <file> [--path P]",
		Short: "Admit a binary asset to the Design in the current folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := openDesignDir(".", false)
			if err != nil {
				return designError(cli, err)
			}
			defer root.Close()
			state, err := loadDesignState(root)
			if err != nil {
				return designError(cli, err)
			}
			if state.ID == "" {
				return designError(cli, fmt.Errorf("this folder has no remote Design yet"))
			}
			id, err := designID(state.ID)
			if err != nil {
				return designError(cli, err)
			}
			assetPath := pathFlag
			if !cmd.Flags().Changed("path") {
				dir, err := os.Getwd()
				if err != nil {
					return designError(cli, err)
				}
				filename, err := filepath.Abs(args[0])
				if err != nil {
					return designError(cli, err)
				}
				relative, err := filepath.Rel(dir, filename)
				if err != nil || !filepath.IsLocal(relative) {
					relative = filepath.Base(filename)
				}
				assetPath = filepath.ToSlash(relative)
			}
			if err := validateDesignContentPath(assetPath); err != nil {
				return designError(cli, err)
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			mediaType := designAssetMediaType(assetPath)
			if mediaType == "" || utf8.RuneCountInString(assetPath) > 240 {
				return designError(cli, fmt.Errorf("unsupported asset path %q", assetPath))
			}
			file, err := readDesignUpload(args[0], designAssetLimit(mediaType))
			if err != nil {
				return designError(cli, err)
			}
			asset, err := client.ArtifactAssetUpload(cmd.Context(), id, api.ArtifactCheckFile{
				Path: assetPath, MediaType: mediaType, ArtifactUpload: file,
			}, "", "")
			if err != nil {
				return designError(cli, err)
			}
			if err := rememberDesignAsset(root, state, assetPath, asset.ArtifactAsset); err != nil {
				return designError(cli, fmt.Errorf("asset %s was uploaded to Design %s, but local state could not be saved: %w", asset.Path, id, err))
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(asset)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Admitted asset %s (%s)", asset.Path, asset.ID))
			return nil
		},
	}
	add.Flags().StringVar(&pathFlag, "path", "", "Project-relative asset path")
	cmd.AddCommand(add)
	return cmd
}

func newDesignExportCmd(cli *CLI) *cobra.Command {
	var destination string
	cmd := &cobra.Command{
		Use:   "export <id>",
		Short: "Download the current Design as a portable ZIP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if destination == "" {
				return exitcodes.Newf(exitcodes.Usage, "output file is required")
			}
			id, err := designID(args[0])
			if err != nil {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()
			remote, err := client.ArtifactGet(ctx, id)
			if err != nil {
				return designError(cli, err)
			}
			archive, err := exportDesignArchive(ctx, client, id, remote, destination)
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(struct {
					Path         string   `json:"path"`
					Bytes        int64    `json:"bytes"`
					OmittedPaths []string `json:"omittedPaths"`
				}{destination, archive.ByteLength, archive.OmittedPaths})
			}
			cli.Printer.PrintLine(fmt.Sprintf("Exported %s", destination))
			for _, p := range archive.OmittedPaths {
				cli.Printer.PrintLine(fmt.Sprintf("Omitted %s", p))
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&destination, "output-file", "o", "", "Destination ZIP file")
	_ = cmd.MarkFlagRequired("output-file")
	return cmd
}

const designArchiveV1Limit = 26 << 20
const designArchiveV1AssetLimit = 5 << 20
const designArchiveV1AssetTotal = 20 << 20
const designArchiveManifestName = "copera-artifact.json"

func exportDesignArchive(ctx context.Context, client *api.Client, id string, remote *api.Artifact, destination string) (*api.ArtifactArchive, error) {
	versionID := remote.CurrentDraftVersionID
	version := &remote.CurrentVersion
	selectedVersion := func() (*api.ArtifactVersion, error) {
		if version.ID == versionID && version.Files != nil {
			return version, nil
		}
		var err error
		version, err = client.ArtifactVersionGet(ctx, id, versionID)
		return version, err
	}

	archive, err := client.ArtifactArchiveLink(ctx, id, versionID, 2)
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && apiErr.Code == "INVALID_PARAMS" {
		selected, err := selectedVersion()
		if err != nil {
			return nil, err
		}
		if err := checkDesignArchiveV1Fallback(ctx, client, id, selected); err != nil {
			return nil, err
		}
		archive, err = client.ArtifactArchiveLink(ctx, id, versionID, 0)
	}
	if err != nil {
		return nil, err
	}
	if archive.ByteLength <= 0 || archive.ByteLength > designArchiveUploadLimit {
		return nil, fmt.Errorf("archive length is outside the supported ZIP limits")
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".copera-archive-*.zip")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	_, downloadErr := downloadDesignBytes(ctx, client, archive.URL, temp, archive.ByteLength, designArchiveUploadLimit, archive.ContentDigest)
	if err := errors.Join(downloadErr, temp.Close()); err != nil {
		return nil, fmt.Errorf("download Design archive: %w", err)
	}
	if err := verifyDesignArchive(temp.Name(), archive, selectedVersion); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.Rename(temp.Name(), destination); err != nil {
		return nil, err
	}
	return archive, nil
}

func designArchiveV1Type(mediaType string) bool {
	switch mediaType {
	case "image/png", "image/jpeg", "image/webp", "font/woff2":
		return true
	default:
		return false
	}
}

var errDesignArchiveNeedsV2 = errors.New("the selected Design version requires an API that supports portable ZIP v2")

func checkDesignArchiveV1Fallback(ctx context.Context, client *api.Client, id string, version *api.ArtifactVersion) error {
	pins := designVersionBinaryAssets(version)
	if len(pins) == 0 {
		return nil
	}
	if len(pins) > 128 {
		return errDesignArchiveNeedsV2
	}
	for _, pin := range pins {
		if !designArchiveV1Type(pin.MediaType) {
			return errDesignArchiveNeedsV2
		}
	}
	assets, err := client.ArtifactAssets(ctx, id)
	if err != nil {
		return err
	}
	var total int64
	for _, pin := range pins {
		index := slices.IndexFunc(assets, func(asset api.ArtifactUploadedAsset) bool {
			return asset.ID == pin.ID && asset.Path == pin.Path && asset.ContentDigest == pin.ContentDigest && asset.MediaType == pin.MediaType
		})
		if index < 0 || assets[index].ByteLength <= 0 || assets[index].ByteLength > designArchiveV1AssetLimit {
			return errDesignArchiveNeedsV2
		}
		total += assets[index].ByteLength
	}
	if total > designArchiveV1AssetTotal {
		return errDesignArchiveNeedsV2
	}
	return nil
}

// Some older manifests also describe assets that are already included as source.
func designVersionBinaryAssets(version *api.ArtifactVersion) []api.ArtifactAsset {
	return slices.DeleteFunc(slices.Clone(version.AuthoringManifest.Assets), func(asset api.ArtifactAsset) bool {
		return slices.ContainsFunc(version.Files, func(file api.ArtifactFileRef) bool {
			return file.Path == asset.Path
		})
	})
}

type designArchiveEntry struct {
	Path          string `json:"path"`
	MediaType     string `json:"mediaType"`
	ByteLength    int64  `json:"byteLength"`
	ContentDigest string `json:"contentDigest"`
}

type designArchiveManifest struct {
	ArchiveVersion    int                  `json:"archiveVersion"`
	LegacyVersion     int                  `json:"schemaVersion"`
	AuthoringManifest api.ArtifactManifest `json:"authoringManifest"`
	Files             []designArchiveEntry `json:"files"`
	BinaryAssets      []designArchiveEntry `json:"binaryAssets"`
}

func verifyDesignArchive(filename string, archive *api.ArtifactArchive, loadVersion func() (*api.ArtifactVersion, error)) error {
	reader, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) > 1000 {
		return fmt.Errorf("ZIP has too many entries")
	}
	entries := make(map[string]*zip.File, len(reader.File))
	names := make(designPaths)
	for _, entry := range reader.File {
		if err := validateDesignContentPath(entry.Name); err != nil {
			return err
		}
		if err := names.add(entry.Name, false); err != nil {
			return err
		}
		if _, duplicate := entries[entry.Name]; duplicate || !entry.Mode().IsRegular() || entry.Flags&1 != 0 {
			return fmt.Errorf("invalid archive entry %q", entry.Name)
		}
		entries[entry.Name] = entry
	}

	manifestEntry := entries[designArchiveManifestName]
	if manifestEntry == nil || manifestEntry.UncompressedSize64 > 1<<20 {
		return fmt.Errorf("missing or oversized archive manifest")
	}
	source, err := manifestEntry.Open()
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(source, (1<<20)+1))
	if err := errors.Join(readErr, source.Close()); err != nil {
		return err
	}
	var manifest designArchiveManifest
	if len(data) > 1<<20 || json.Unmarshal(data, &manifest) != nil {
		return fmt.Errorf("invalid archive manifest")
	}
	delete(entries, designArchiveManifestName)
	legacy := manifest.ArchiveVersion == 0 && manifest.LegacyVersion == 1
	actual := manifest.ArchiveVersion
	if legacy {
		actual = 1
	}
	if (actual != 1 && actual != 2) || (archive.ArchiveVersion != 0 && archive.ArchiveVersion != actual) {
		return fmt.Errorf("archive version differs from the download contract")
	}
	if actual == 2 {
		// The link's SHA-256 has already bound the exact server-validated ZIP.
		return nil
	}
	if archive.ByteLength > designArchiveV1Limit {
		return errDesignArchiveNeedsV2
	}
	selected, err := loadVersion()
	if err != nil {
		return err
	}
	return checkLegacyArchiveInventory(selected, entries, archive.OmittedPaths, manifest, legacy)
}

func checkLegacyArchiveInventory(version *api.ArtifactVersion, entries map[string]*zip.File, omitted []string, manifest designArchiveManifest, legacy bool) error {
	source := make(map[string]designArchiveEntry, len(manifest.Files))
	for _, file := range manifest.Files {
		source[file.Path] = file
	}
	expectedFiles := 0
	for _, file := range version.Files {
		if file.Path == "draft/start-request.json" || slices.Contains(omitted, file.Path) {
			continue
		}
		expectedFiles++
		portable := source[file.Path]
		if legacy {
			portable = designArchiveEntry{file.Path, file.MediaType, file.UTF8Bytes, file.ContentDigest}
		}
		mutableMetadata := file.Path == version.AuthoringManifest.SharedDesign.AssetsPath
		if portable.MediaType != file.MediaType || (!mutableMetadata &&
			(portable.ContentDigest != file.ContentDigest || portable.ByteLength != file.UTF8Bytes)) {
			return fmt.Errorf("ZIP v1 source differs from selected version at %q", file.Path)
		}
		entry := entries[file.Path]
		if entry == nil || portable.ByteLength < 0 || portable.ByteLength > designSourceFileLimit ||
			entry.UncompressedSize64 != uint64(portable.ByteLength) {
			return fmt.Errorf("ZIP v1 source length differs at %q", file.Path)
		}
		delete(entries, file.Path)
	}
	if !legacy && expectedFiles != len(manifest.Files) {
		return fmt.Errorf("ZIP v1 source inventory differs from the selected Design version")
	}

	pins := make(map[string]api.ArtifactAsset, len(manifest.AuthoringManifest.Assets))
	for _, pin := range manifest.AuthoringManifest.Assets {
		pins[pin.Path] = pin
	}
	binaries := make(map[string]int64, len(manifest.BinaryAssets))
	for _, asset := range manifest.BinaryAssets {
		binaries[asset.Path] = asset.ByteLength
	}
	var total int64
	count := 0
	for _, pin := range designVersionBinaryAssets(version) {
		if !designArchiveV1Type(pin.MediaType) {
			return errDesignArchiveNeedsV2
		}
		copied := pins[pin.Path]
		size, entry := binaries[pin.Path], entries[pin.Path]
		if entry == nil || size <= 0 || size > designArchiveV1AssetLimit || uint64(size) != entry.UncompressedSize64 ||
			copied.ContentDigest != pin.ContentDigest || copied.MediaType != pin.MediaType {
			return errDesignArchiveNeedsV2
		}
		count++
		total += size
		delete(entries, pin.Path)
	}
	if count > 128 || total > designArchiveV1AssetTotal || count != len(manifest.BinaryAssets) || len(entries) != 0 {
		return errDesignArchiveNeedsV2
	}
	return nil
}

func newDesignImportCmd(cli *CLI) *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "import <file.zip>",
		Short: "Create a Design from a portable ZIP",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if cmd.Flags().Changed("workspace") {
				if workspace == "" {
					return exitcodes.Newf(exitcodes.Usage, "workspace cannot be empty")
				}
				ws, err := client.WorkspaceInfo(ctx)
				if err != nil {
					return designError(cli, err)
				}
				if workspace != ws.Slug && workspace != ws.ID && workspace != ws.Name {
					message := fmt.Sprintf("the active token belongs to workspace %q, not %q", ws.Slug, workspace)
					cli.Printer.PrintError("workspace_mismatch", message, "Select a token for the target workspace with --profile", false)
					return exitcodes.Newf(exitcodes.Usage, "%s", message)
				}
			}
			file, err := readDesignUpload(args[0], designArchiveUploadLimit)
			if err != nil {
				return designError(cli, err)
			}
			result, err := client.ArtifactImport(ctx, args[0], file, rand.Text(), designTool())
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Imported %s (%s)", result.Title, result.ID))
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "Verify the authenticated workspace by ID, slug, or name")
	return cmd
}

func newDesignRenameCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <id> <title>",
		Short: "Change a Design's title",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := designID(args[0])
			if err != nil {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			result, err := client.ArtifactRename(cmd.Context(), id, args[1])
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Renamed %s to %s", result.ID, result.Title))
			return nil
		},
	}
}

func newDesignTrashCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "trash <id>",
		Short: "Move a Design to the Drive trash",
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
			result, err := client.ArtifactTrash(cmd.Context(), id)
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Trashed %s", result.ID))
			return nil
		},
	}
}

func newDesignRestoreCmd(cli *CLI) *cobra.Command {
	var number int
	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Store an earlier Design version as a new head",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if number < 1 {
				return exitcodes.Newf(exitcodes.Usage, "version must be a positive number")
			}
			id, err := designID(args[0])
			if err != nil {
				return designError(cli, err)
			}
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			head, err := client.ArtifactGet(ctx, id)
			if err != nil {
				return designError(cli, err)
			}
			version, err := designVersion(ctx, client, head, number)
			if err != nil {
				return designError(cli, err)
			}
			result, err := client.ArtifactRestore(ctx, id, version.ID, api.ArtifactRestoreInput{
				BaseVersionID:        head.CurrentDraftVersionID,
				ExpectedHeadRevision: head.HeadRevision,
				IdempotencyKey:       rand.Text(),
				Tool:                 designTool(),
			})
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Version %d (%s, build %s)", result.Version.Version, result.Outcome, result.BuildState))
			return nil
		},
	}
	cmd.Flags().IntVar(&number, "version", 0, "Version number to restore")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

func newDesignForkCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "fork <id>",
		Short: "Copy a Design into a new private Design",
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
			result, err := client.ArtifactFork(cmd.Context(), id, rand.Text(), designTool())
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Forked %s (%s)", result.Title, result.ID))
			return nil
		},
	}
}

func readDesignUpload(filename string, maxBytes int64) (api.ArtifactUpload, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return api.ArtifactUpload{}, err
	}
	if !info.Mode().IsRegular() {
		return api.ArtifactUpload{}, fmt.Errorf("%q must be a regular file", filename)
	}
	return captureDesignUpload(filename, maxBytes, func() (*os.File, error) {
		return os.Open(filename)
	})
}

func downloadDesignBytes(ctx context.Context, client *api.Client, rawURL string, dst io.Writer, expected, limit int64, digest string) (int64, error) {
	if err := upload.ValidatePresignedURL(rawURL); err != nil {
		return 0, err
	}
	checksum, err := hex.DecodeString(digest)
	if err != nil || len(checksum) != sha256.Size {
		return 0, fmt.Errorf("missing or invalid download SHA-256")
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, fmt.Errorf("invalid download request")
	}
	httpClient := *client.HTTPClient()
	httpClient.Timeout = 120 * time.Second
	resp, err := httpClient.Do(req)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) {
			return 0, requestError.Err
		}
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	if expected > 0 {
		limit = min(limit, expected)
	}
	if resp.ContentLength > limit {
		return 0, fmt.Errorf("download exceeds its %s limit", humanSize(limit))
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, err
	}
	if n > limit || (expected != 0 && n != expected) || hex.EncodeToString(hash.Sum(nil)) != digest {
		return 0, fmt.Errorf("download does not match its size or SHA-256")
	}
	return n, nil
}
