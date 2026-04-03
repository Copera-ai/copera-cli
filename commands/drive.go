package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/copera/copera-cli/internal/api"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/upload"
	"github.com/spf13/cobra"
)

func newDriveCmd(cli *CLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drive",
		Short: "Manage drive files and folders",
	}
	cmd.AddCommand(
		newDriveTreeCmd(cli),
		newDriveSearchCmd(cli),
		newDriveGetCmd(cli),
		newDriveDownloadCmd(cli),
		newDriveUploadCmd(cli),
		newDriveMkdirCmd(cli),
	)
	return cmd
}

// ── drive tree ───────────────────────────────────────────────────────────────

func newDriveTreeCmd(cli *CLI) *cobra.Command {
	var flagParent string
	var flagDepth int

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show drive file tree",
		Long: `Show files and folders at the workspace root or under a specific folder.

Use --parent <id> to drill into a subtree. --depth controls nesting (1-10, default 3).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagDepth != 0 && (flagDepth < 1 || flagDepth > 10) {
				cli.Printer.PrintError("invalid_flag", "depth must be between 1 and 10", "", false)
				return exitcodes.Newf(exitcodes.Usage, "depth must be between 1 and 10")
			}

			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			result, err := client.DriveTree(context.Background(), flagParent, flagDepth)
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(result)
			}

			if len(result.Nodes) == 0 {
				cli.Printer.Info("No files or folders found.")
				return nil
			}
			printDriveTree(cli, result.Nodes, "")

			if result.Truncated {
				cli.Printer.Info("\nResults truncated (%d items). Use --parent with a folder ID to drill deeper.", result.TotalItems)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagParent, "parent", "", "Show subtree under this folder ID (default: workspace root)")
	cmd.Flags().IntVar(&flagDepth, "depth", 0, "Max nesting depth, 1-10 (default: server default 3)")
	return cmd
}

func printDriveTree(cli *CLI, nodes []api.DriveNode, prefix string) {
	for i, node := range nodes {
		last := i == len(nodes)-1
		conn := "├── "
		childPrefix := prefix + "│   "
		if last {
			conn = "└── "
			childPrefix = prefix + "    "
		}

		label := node.Name
		if node.Type == "folder" {
			label += "/"
		} else if node.Size > 0 {
			label += fmt.Sprintf("  (%s)", humanSize(node.Size))
		}

		cli.Printer.PrintLine(fmt.Sprintf("%s%s%s  (%s)", prefix, conn, label, node.ID))

		if len(node.Children) > 0 {
			printDriveTree(cli, node.Children, childPrefix)
		}
	}
}

// ── drive search ─────────────────────────────────────────────────────────────

func newDriveSearchCmd(cli *CLI) *cobra.Command {
	var flagSortBy, flagSortOrder string
	var flagLimit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search files and folders",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			hits, totalHits, err := client.DriveSearch(context.Background(), args[0], api.DriveSearchOpts{
				SortBy:    flagSortBy,
				SortOrder: flagSortOrder,
				Limit:     flagLimit,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"hits":      hits,
					"totalHits": totalHits,
				})
			}

			if len(hits) == 0 {
				cli.Printer.Info("No results for %q.", args[0])
				return nil
			}

			for i, h := range hits {
				cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", h.ID))
				cli.Printer.PrintLine(fmt.Sprintf("Name:     %s", h.Name))
				cli.Printer.PrintLine(fmt.Sprintf("Type:     %s", h.Type))
				if h.MimeType != "" {
					cli.Printer.PrintLine(fmt.Sprintf("MIME:     %s", h.MimeType))
				}
				if h.Size > 0 {
					cli.Printer.PrintLine(fmt.Sprintf("Size:     %s", humanSize(h.Size)))
				}
				cli.Printer.PrintLine(fmt.Sprintf("Updated:  %s", h.UpdatedAt.Format("2006-01-02 15:04:05")))
				if i < len(hits)-1 {
					cli.Printer.PrintLine("")
				}
			}

			if totalHits > len(hits) {
				cli.Printer.Info("\nShowing %d of %d results.", len(hits), totalHits)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&flagSortBy, "sort-by", "", "Sort field: createdAt|updatedAt")
	cmd.Flags().StringVar(&flagSortOrder, "sort-order", "", "Sort direction: asc|desc")
	cmd.Flags().IntVar(&flagLimit, "limit", 0, "Max results (1–50)")
	return cmd
}

// ── drive get ────────────────────────────────────────────────────────────────

func newDriveGetCmd(cli *CLI) *cobra.Command {
	return &cobra.Command{
		Use:   "get <fileId>",
		Short: "Get file or folder metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			f, err := client.DriveFileGet(context.Background(), args[0])
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(f)
			}

			cli.Printer.PrintLine(fmt.Sprintf("ID:       %s", f.ID))
			cli.Printer.PrintLine(fmt.Sprintf("Name:     %s", f.Name))
			cli.Printer.PrintLine(fmt.Sprintf("Type:     %s", f.Type))
			if f.MimeType != "" {
				cli.Printer.PrintLine(fmt.Sprintf("MIME:     %s", f.MimeType))
			}
			if f.Size > 0 {
				cli.Printer.PrintLine(fmt.Sprintf("Size:     %s (%d bytes)", humanSize(f.Size), f.Size))
			}
			if f.Parent != "" {
				cli.Printer.PrintLine(fmt.Sprintf("Parent:   %s", f.Parent))
			}
			cli.Printer.PrintLine(fmt.Sprintf("Owner:    %s", f.Owner))
			cli.Printer.PrintLine(fmt.Sprintf("Created:  %s", f.CreatedAt.Format("2006-01-02 15:04:05")))
			cli.Printer.PrintLine(fmt.Sprintf("Updated:  %s", f.UpdatedAt.Format("2006-01-02 15:04:05")))
			return nil
		},
	}
}

// ── drive download ───────────────────────────────────────────────────────────

func newDriveDownloadCmd(cli *CLI) *cobra.Command {
	var flagOutput string

	cmd := &cobra.Command{
		Use:   "download <fileId>",
		Short: "Download a file",
		Long:  `Download a file using a presigned CloudFront URL. Use -o to set the destination path.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			ctx := context.Background()

			// Get file metadata for name and size
			meta, err := client.DriveFileGet(ctx, args[0])
			if err != nil {
				return apiError(cli, err)
			}
			if meta.Type == "folder" {
				cli.Printer.PrintError("invalid_operation", "cannot download a folder", "Use 'drive get' to view folder metadata", false)
				return exitcodes.Newf(exitcodes.Usage, "cannot download a folder")
			}

			// Get presigned download URL
			downloadURL, err := client.DriveDownloadURL(ctx, args[0])
			if err != nil {
				return apiError(cli, err)
			}

			// Determine destination path
			dest := flagOutput
			if dest == "" {
				dest = sanitizeFilename(meta.Name)
			}
			dest, err = safePath(dest)
			if err != nil {
				cli.Printer.PrintError("invalid_path", err.Error(), "Use --dest with a safe file path", false)
				return exitcodes.New(exitcodes.Usage, err)
			}

			// Validate download URL before following it
			if err := upload.ValidatePresignedURL(downloadURL); err != nil {
				cli.Printer.PrintError("unsafe_url", err.Error(), "", false)
				return exitcodes.New(exitcodes.Error, err)
			}

			// Download file
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
			if err != nil {
				return exitcodes.New(exitcodes.Error, fmt.Errorf("build download request: %w", err))
			}

			resp, err := client.HTTPClient().Do(req)
			if err != nil {
				return exitcodes.New(exitcodes.Error, fmt.Errorf("download: %w", err))
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return exitcodes.Newf(exitcodes.Error, "download failed with status %d", resp.StatusCode)
			}

			outFile, err := os.Create(dest)
			if err != nil {
				return exitcodes.New(exitcodes.Error, fmt.Errorf("create output file: %w", err))
			}
			defer outFile.Close()

			var reader io.Reader = resp.Body
			if upload.ShouldShowProgress(cli.Printer.Err) && !cli.Printer.IsJSON() && !cli.flags.quiet {
				prog := upload.NewBarProgress(cli.Printer.Err)
				prog.Init(meta.Name, meta.Size)
				reader = &progressReader{r: resp.Body, progress: prog}
				defer prog.Finish()
			}

			written, err := io.Copy(outFile, reader)
			if err != nil {
				return exitcodes.New(exitcodes.Error, fmt.Errorf("write file: %w", err))
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(map[string]any{
					"file": meta.Name,
					"size": written,
					"path": dest,
				})
			}

			cli.Printer.Info("Downloaded %s (%s)", meta.Name, humanSize(written))
			return nil
		},
	}
	cmd.Flags().StringVarP(&flagOutput, "dest", "o", "", "Destination file path (default: current dir + original filename)")
	return cmd
}

// progressReader wraps an io.Reader and reports bytes read to a Progress.
type progressReader struct {
	r        io.Reader
	progress upload.Progress
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.progress.Add(int64(n))
	}
	return n, err
}

// ── drive upload ─────────────────────────────────────────────────────────────

func newDriveUploadCmd(cli *CLI) *cobra.Command {
	var flagParent string
	var flagChunkSize int64
	var flagConcurrency int

	cmd := &cobra.Command{
		Use:   "upload <path>",
		Short: "Upload a file or directory",
		Long: `Upload a file or recursively upload a directory to the drive.

For single files, the multipart upload flow splits the file into chunks
and uploads them in parallel via S3 presigned URLs.

For directories, folders are created first, then files are uploaded.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			ctx := context.Background()
			path := args[0]

			info, err := os.Stat(path)
			if err != nil {
				cli.Printer.PrintError("file_error", err.Error(), "", false)
				return exitcodes.New(exitcodes.Error, err)
			}

			var progress upload.Progress = upload.NoopProgress{}
			if upload.ShouldShowProgress(cli.Printer.Err) && !cli.Printer.IsJSON() && !cli.flags.quiet {
				progress = upload.NewBarProgress(cli.Printer.Err)
			}

			uploader := upload.NewUploader(client.HTTPClient(), flagChunkSize, flagConcurrency, progress)

			if !info.IsDir() {
				if info.Size() == 0 {
					cli.Printer.PrintError("empty_file", "cannot upload empty file", "File must have content", false)
					return exitcodes.Newf(exitcodes.Usage, "cannot upload empty file")
				}
				// Single file upload
				result, err := uploadSingleFile(ctx, client, uploader, path, info.Size(), flagParent)
				if err != nil {
					return err
				}
				if cli.Printer.IsJSON() {
					return cli.Printer.PrintJSON(result)
				}
				cli.Printer.Info("Uploaded %s (%s) -> %s", result.Name, humanSize(result.Size), result.ID)
				return nil
			}

			// Directory upload
			return uploadDirectory(ctx, cli, client, uploader, path, flagParent, progress)
		},
	}
	cmd.Flags().StringVar(&flagParent, "parent", "", "Target folder ID")
	cmd.Flags().Int64Var(&flagChunkSize, "chunk-size", upload.DefaultChunkSize, "Chunk size in bytes for multipart upload")
	cmd.Flags().IntVar(&flagConcurrency, "concurrency", upload.DefaultConcurrency, "Number of concurrent part uploads")
	return cmd
}

func uploadSingleFile(ctx context.Context, client *api.Client, uploader *upload.Uploader, path string, fileSize int64, parentID string) (*api.DriveFile, error) {
	mimeType := upload.DetectMimeType(path)
	fileName := filepath.Base(path)

	// Step 1: Start multipart upload
	startResp, err := client.DriveMultipartStart(ctx, &api.MultipartStartRequest{
		FileName: fileName,
		FileSize: fileSize,
		MimeType: mimeType,
		ParentID: parentID,
	})
	if err != nil {
		return nil, err
	}

	// Step 2: Get presigned URLs
	numParts := upload.NumParts(fileSize, uploader.ChunkSize())
	urlsResp, err := client.DrivePresignedURLs(ctx, &api.PresignedURLsRequest{
		UploadID: startResp.UploadID,
		Parts:    numParts,
		FileKey:  startResp.FileKey,
	})
	if err != nil {
		return nil, err
	}

	// Step 3: Upload parts to S3
	completed, err := uploader.UploadParts(ctx, path, urlsResp.Parts)
	if err != nil {
		return nil, fmt.Errorf("upload %s: %w", fileName, err)
	}

	// Step 4: Finalize
	result, err := client.DriveMultipartFinalize(ctx, &api.MultipartFinalizeRequest{
		UploadID: startResp.UploadID,
		FileKey:  startResp.FileKey,
		Parts:    completed,
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func uploadDirectory(ctx context.Context, cli *CLI, client *api.Client, uploader *upload.Uploader, rootPath, parentID string, progress upload.Progress) error {
	entries, err := upload.WalkDir(rootPath)
	if err != nil {
		cli.Printer.PrintError("walk_error", err.Error(), "", false)
		return exitcodes.New(exitcodes.Error, err)
	}

	// Map local relative paths to remote folder IDs
	folderMap := map[string]string{
		"": parentID, // root maps to --parent flag value
	}

	var fileCount, folderCount int
	var totalBytes int64
	var results []*api.DriveFile

	for _, entry := range entries {
		if entry.IsDir {
			// Create folder
			parentDir := filepath.Dir(entry.RelativePath)
			if parentDir == "." {
				parentDir = ""
			}
			remoteParent := folderMap[parentDir]

			folder, err := client.DriveFolderCreate(ctx, &api.CreateFolderRequest{
				Name:     filepath.Base(entry.RelativePath),
				ParentID: remoteParent,
			})
			if err != nil {
				return apiError(cli, err)
			}
			folderMap[entry.RelativePath] = folder.ID
			folderCount++
			cli.Printer.Info("Created folder: %s", entry.RelativePath)
		} else {
			if entry.Size == 0 {
				cli.Printer.Info("Skipping empty file: %s", entry.RelativePath)
				continue
			}
			// Upload file
			parentDir := filepath.Dir(entry.RelativePath)
			if parentDir == "." {
				parentDir = ""
			}
			remoteParent := folderMap[parentDir]

			result, err := uploadSingleFile(ctx, client, uploader, entry.LocalPath, entry.Size, remoteParent)
			if err != nil {
				return apiError(cli, err)
			}
			results = append(results, result)
			fileCount++
			totalBytes += entry.Size
		}
	}

	if cli.Printer.IsJSON() {
		return cli.Printer.PrintJSON(map[string]any{
			"files":      results,
			"fileCount":  fileCount,
			"folderCount": folderCount,
			"totalBytes": totalBytes,
		})
	}

	cli.Printer.Info("Uploaded %d files, %d folders (%s total)", fileCount, folderCount, humanSize(totalBytes))
	return nil
}

// ── drive mkdir ──────────────────────────────────────────────────────────────

func newDriveMkdirCmd(cli *CLI) *cobra.Command {
	var flagParent string

	cmd := &cobra.Command{
		Use:   "mkdir <name>",
		Short: "Create a folder",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := requireAPIClient(cli)
			if err != nil {
				return err
			}

			folder, err := client.DriveFolderCreate(context.Background(), &api.CreateFolderRequest{
				Name:     args[0],
				ParentID: flagParent,
			})
			if err != nil {
				return apiError(cli, err)
			}

			if cli.Printer.IsJSON() {
				return cli.Printer.PrintJSON(folder)
			}

			cli.Printer.Info("Created folder %q (%s)", folder.Name, folder.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&flagParent, "parent", "", "Parent folder ID")
	return cmd
}

// safePath validates a destination path to prevent path traversal.
// Rejects paths containing ".." components.
func safePath(dest string) (string, error) {
	cleaned := filepath.Clean(dest)
	// Reject paths that traverse upward
	for _, part := range strings.Split(cleaned, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("path traversal not allowed: %q", dest)
		}
	}
	return cleaned, nil
}

// sanitizeFilename removes path separators and dangerous characters from
// a server-provided filename so it cannot escape the current directory.
func sanitizeFilename(name string) string {
	// Take only the base name to strip any path components
	name = filepath.Base(name)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "download"
	}
	return name
}

// humanSize formats bytes into a human-readable string.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
