package commands

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/spf13/cobra"
)

const (
	designAssetUploadLimit   = 5 << 20
	designArchiveUploadLimit = 26 << 20
)

func newDesignAssetsCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{Use: "assets", Short: "Manage a Design's assets"}
	cmd.AddCommand(&cobra.Command{
		Use:   "add <file>",
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

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}
			data, err := readDesignUpload(args[0], designAssetUploadLimit)
			if err != nil {
				return designError(cli, err)
			}
			asset, err := client.ArtifactAssetUpload(cmd.Context(), id, args[0], data, "", "")
			if err != nil {
				return designError(cli, err)
			}
			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(asset)
			}
			cli.Printer.PrintLine(fmt.Sprintf("Admitted asset %s (%s)", asset.Path, asset.ID))
			return nil
		},
	})
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
			ctx := cmd.Context()
			remote, err := client.ArtifactGet(ctx, id)
			if err != nil {
				return designError(cli, err)
			}
			archive, err := client.ArtifactArchiveLink(ctx, id, remote.CurrentDraftVersionID)
			if err != nil {
				return designError(cli, err)
			}
			data, err := downloadHandoffAsset(ctx, client, archive.URL)
			if err != nil {
				return designError(cli, fmt.Errorf("download Design archive: %w", err))
			}
			if int64(len(data)) != archive.ByteLength || fmt.Sprintf("%x", sha256.Sum256(data)) != archive.ContentDigest {
				return designError(cli, fmt.Errorf("downloaded archive does not match the server's size or SHA-256"))
			}
			if err := os.WriteFile(destination, data, 0o644); err != nil {
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
			data, err := readDesignUpload(args[0], designArchiveUploadLimit)
			if err != nil {
				return designError(cli, err)
			}
			result, err := client.ArtifactImport(ctx, args[0], data, rand.Text(), designTool())
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

func readDesignUpload(filename string, maxBytes int64) ([]byte, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q must be a regular file", filename)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("%q exceeds the %s upload limit", filename, humanSize(maxBytes))
	}
	return data, nil
}
