package cache

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/copera/copera-cli/internal/build"
)

// DefaultDir returns the default cache root: os.TempDir()/copera-cli-{version}.
func DefaultDir() string {
	return filepath.Join(os.TempDir(), "copera-cli-"+build.Version)
}

// DocsDir returns the docs content cache subdirectory.
func DocsDir(base string) string {
	if base == "" {
		base = DefaultDir()
	}
	return filepath.Join(base, "docs")
}

// TablesDir returns the table schema cache subdirectory.
func TablesDir(base string) string {
	if base == "" {
		base = DefaultDir()
	}
	return filepath.Join(base, "tables")
}

// DirInfo holds cache directory stats.
type DirInfo struct {
	Path  string
	Files int
	Bytes int64
}

// Stat returns file count and total size for a cache directory.
func Stat(dir string) DirInfo {
	if dir == "" {
		dir = DefaultDir()
	}
	info := DirInfo{Path: dir}
	_ = filepath.Walk(dir, func(_ string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		info.Files++
		info.Bytes += fi.Size()
		return nil
	})
	return info
}

// Clean removes the entire cache directory.
func Clean(dir string) error {
	if dir == "" {
		dir = DefaultDir()
	}
	return os.RemoveAll(dir)
}

// FormatSize returns a human-readable size string.
func FormatSize(bytes int64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
