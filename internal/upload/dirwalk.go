package upload

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DirEntry represents a local file or folder to upload.
type DirEntry struct {
	LocalPath    string // absolute path
	RelativePath string // path relative to the walk root
	IsDir        bool
	Size         int64
	MimeType     string // detected MIME type (empty for directories)
}

// WalkDir recursively walks root and returns entries sorted so parent
// directories appear before their children (depth-first order).
// Symlinks that escape the root directory are rejected.
func WalkDir(root string) ([]DirEntry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Resolve symlinks in root itself so containment checks are reliable
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}

	var entries []DirEntry
	err = filepath.Walk(resolvedRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// skip the root itself for directory uploads
		if path == resolvedRoot {
			return nil
		}

		// Resolve symlinks and verify the real path stays under root
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(realPath, resolvedRoot+string(filepath.Separator)) && realPath != resolvedRoot {
			return fmt.Errorf("path escapes root via symlink: %s -> %s", path, realPath)
		}

		rel, err := filepath.Rel(resolvedRoot, path)
		if err != nil {
			return err
		}

		entry := DirEntry{
			LocalPath:    realPath,
			RelativePath: rel,
			IsDir:        info.IsDir(),
			Size:         info.Size(),
		}
		if !info.IsDir() {
			entry.MimeType = DetectMimeType(path)
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort: directories before files at the same depth, then alphabetical.
	// This ensures parent folders are created before their contents.
	sort.Slice(entries, func(i, j int) bool {
		di := strings.Count(entries[i].RelativePath, string(filepath.Separator))
		dj := strings.Count(entries[j].RelativePath, string(filepath.Separator))
		if di != dj {
			return di < dj
		}
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].RelativePath < entries[j].RelativePath
	})

	return entries, nil
}

// DetectMimeType returns the MIME type based on file extension,
// falling back to "application/octet-stream".
func DetectMimeType(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return "application/octet-stream"
	}
	mt := mime.TypeByExtension(ext)
	if mt == "" {
		return "application/octet-stream"
	}
	return mt
}
