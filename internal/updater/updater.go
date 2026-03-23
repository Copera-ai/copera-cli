// Package updater handles version checking and self-updating the CLI binary.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/copera/copera-cli/internal/build"
	"github.com/copera/copera-cli/internal/cache"
)

// CDNBaseURL is the public CDN for release assets and version checks.
// Override at build time: -X github.com/Copera-ai/copera-cli/internal/updater.CDNBaseURL=https://...
var CDNBaseURL = "https://cli.copera.ai" //nolint:revive // exported var with matching name

const (
	// CheckInterval is how often we check for new versions.
	CheckInterval = 24 * time.Hour
)

// VersionInfo holds the latest version from the remote check.
type VersionInfo struct {
	Latest    string    `json:"latest"`
	CheckedAt time.Time `json:"checked_at"`
}

// CheckResult is returned by CheckVersion.
type CheckResult struct {
	Current   string
	Latest    string
	UpdateURL string
	HasUpdate bool
}

// CheckVersion compares the running version against the latest CDN release.
// It caches the result for CheckInterval to avoid hitting the network on every invocation.
// Set noCache to bypass the cached result and always fetch from CDN.
// Returns nil if no update or check was skipped.
func CheckVersion(ctx context.Context, cacheDir string, noCache bool) *CheckResult {
	cacheFile := filepath.Join(cacheDir, "version-check.json")

	// Check cache first (unless bypassed)
	if !noCache {
		if data, err := os.ReadFile(cacheFile); err == nil {
			var info VersionInfo
			if json.Unmarshal(data, &info) == nil && time.Since(info.CheckedAt) < CheckInterval {
				return compareVersions(info.Latest)
			}
		}
	}

	latest, err := fetchLatestVersion(ctx)
	if err != nil {
		return nil
	}

	// Cache the result
	info := VersionInfo{Latest: latest, CheckedAt: time.Now()}
	if data, err := json.Marshal(info); err == nil {
		_ = os.MkdirAll(filepath.Dir(cacheFile), 0700)
		_ = os.WriteFile(cacheFile, data, 0600)
	}

	return compareVersions(latest)
}

func compareVersions(latest string) *CheckResult {
	current := build.Version
	latest = strings.TrimPrefix(latest, "v")
	current = strings.TrimPrefix(current, "v")

	if current == "dev" || latest == "" || latest == current {
		return nil
	}

	if latest != current {
		return &CheckResult{
			Current:   current,
			Latest:    latest,
			UpdateURL: CDNBaseURL + "/v" + latest,
			HasUpdate: true,
		}
	}
	return nil
}

func fetchLatestVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", CDNBaseURL+"/version.json", nil)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("version check failed: %s", resp.Status)
	}

	var info struct {
		Latest string `json:"latest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.Latest, nil
}

// Update downloads and replaces the current binary with the specified version.
// If version is empty, it uses the latest release.
func Update(ctx context.Context, version string) error {
	if version == "" {
		latest, err := fetchLatestVersion(ctx)
		if err != nil {
			return fmt.Errorf("failed to fetch latest version: %w", err)
		}
		version = strings.TrimPrefix(latest, "v")
	}
	version = strings.TrimPrefix(version, "v")

	osName := runtime.GOOS
	arch := runtime.GOARCH
	ext := "tar.gz"
	if osName == "windows" {
		ext = "zip"
	}

	assetName := fmt.Sprintf("copera-%s-%s-%s.%s", version, osName, arch, ext)
	assetURL := fmt.Sprintf("%s/v%s/%s", CDNBaseURL, version, assetName)

	// Download to temp file
	tmpDir, err := os.MkdirTemp("", "copera-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "archive."+ext)
	if err := downloadFile(ctx, assetURL, archivePath); err != nil {
		return fmt.Errorf("download v%s: %w", version, err)
	}

	// Extract binary
	binaryName := "copera"
	if osName == "windows" {
		binaryName = "copera.exe"
	}
	extractedPath := filepath.Join(tmpDir, binaryName)
	if err := extractBinary(archivePath, extractedPath, binaryName, ext); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	// Find current binary path
	currentBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current binary: %w", err)
	}
	currentBin, err = filepath.EvalSymlinks(currentBin)
	if err != nil {
		return fmt.Errorf("resolve symlink: %w", err)
	}

	// Backup current binary
	backupPath := currentBin + ".backup"
	if err := os.Rename(currentBin, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	// Move new binary into place
	if err := copyFile(extractedPath, currentBin, 0755); err != nil {
		// Restore backup on failure
		_ = os.Rename(backupPath, currentBin)
		return fmt.Errorf("install new binary: %w", err)
	}

	// Remove backup
	_ = os.Remove(backupPath)

	// Invalidate version check cache
	cacheDir := cache.SharedDir()
	_ = os.Remove(filepath.Join(cacheDir, "version-check.json"))

	return nil
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
