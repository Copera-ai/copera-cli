// Package testutil provides helpers for unit-testing CLI commands and config.
// It must only be imported in _test.go files.
package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// WriteTempConfig writes tomlContent to a temp file and returns its path.
// The file is automatically removed when the test ends.
// Permissions are set to 0600 to mirror real config file behaviour.
func WriteTempConfig(t *testing.T, tomlContent string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("testutil.WriteTempConfig: write %s: %v", path, err)
	}
	return path
}

// WriteTempConfigAt writes tomlContent to a specific path.
// Use this when you need to control the exact filename (e.g. .copera.toml).
func WriteTempConfigAt(t *testing.T, path, tomlContent string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(tomlContent), 0600); err != nil {
		t.Fatalf("testutil.WriteTempConfigAt: write %s: %v", path, err)
	}
}

// SetEnv sets an environment variable for the duration of a test and restores
// the original value (or unsets it) when the test ends.
func SetEnv(t *testing.T, key, value string) {
	t.Helper()
	original, exists := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("testutil.SetEnv: %v", err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, original)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// UnsetEnv removes an environment variable for the duration of a test.
func UnsetEnv(t *testing.T, key string) {
	t.Helper()
	original, exists := os.LookupEnv(key)
	_ = os.Unsetenv(key)
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, original)
		}
	})
}
