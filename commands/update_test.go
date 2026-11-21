//go:build !windows

package commands_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/copera/copera-cli/commands"
	"github.com/copera/copera-cli/internal/build"
	"github.com/copera/copera-cli/internal/exitcodes"
	"github.com/copera/copera-cli/internal/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	installedUpdateVersion = "0.18.2"
	nextUpdateVersion      = "0.19.0"
)

// TestUpdateProcess runs the update command from a real, replaceable executable.
func TestUpdateProcess(t *testing.T) {
	if os.Getenv("COPERA_TEST_UPDATE_CDN") == "" {
		return
	}
	build.Version = installedUpdateVersion
	updater.CDNBaseURL = os.Getenv("COPERA_TEST_UPDATE_CDN")
	err := commands.ExecuteWithWriters(os.Args[2:], os.Stdin, os.Stdout, os.Stderr)
	if exitErr, ok := err.(*exitcodes.ExitError); ok {
		os.Exit(exitErr.Code)
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestUpdateWithoutTerminal(t *testing.T) {
	newBinary := []byte("updated copera binary\n")
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "copera", Mode: 0755, Size: int64(len(newBinary))}))
	_, err := tw.Write(newBinary)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	assetName := fmt.Sprintf("copera-%s-%s-%s.tar.gz", nextUpdateVersion, runtime.GOOS, runtime.GOARCH)
	latestUpdate := "/v" + nextUpdateVersion + "/"
	self, err := os.Executable()
	require.NoError(t, err)
	oldBinary, err := os.ReadFile(self)
	require.NoError(t, err)

	for _, tc := range []struct {
		name, latest                      string
		dirMode, fileMode                 os.FileMode
		plain, badChecksum, expectFailure bool
	}{
		{name: "json installs", latest: nextUpdateVersion, dirMode: 0700, fileMode: 0755},
		{name: "json up to date", latest: installedUpdateVersion, dirMode: 0700, fileMode: 0755},
		{name: "plain writable binary", latest: nextUpdateVersion, dirMode: 0500, fileMode: 0755, plain: true},
		{name: "plain requires terminal", latest: nextUpdateVersion, dirMode: 0500, fileMode: 0555, plain: true, expectFailure: true},
		{name: "checksum mismatch", latest: nextUpdateVersion, dirMode: 0700, fileMode: 0755, badChecksum: true, expectFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.dirMode == 0500 && os.Geteuid() == 0 {
				t.Skip("directory permissions do not restrict root")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/version.json":
					fmt.Fprintf(w, `{"latest":%q}`, tc.latest)
				case latestUpdate + assetName:
					w.Write(archive.Bytes())
				case latestUpdate + "checksums.txt":
					hash := sha256.Sum256(archive.Bytes())
					if tc.badChecksum {
						hash[0] ^= 1
					}
					fmt.Fprintf(w, "%x  %s\n", hash, assetName)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			dir := t.TempDir()
			dest := filepath.Join(dir, "copera")
			require.NoError(t, os.WriteFile(dest, oldBinary, tc.fileMode))
			require.NoError(t, os.Chmod(dir, tc.dirMode))
			t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0700)) })
			tmp := t.TempDir()
			sudoMarker := filepath.Join(tmp, "sudo-called")
			require.NoError(t, os.WriteFile(filepath.Join(tmp, "sudo"), []byte("#!/bin/sh\ntouch \"$COPERA_TEST_SUDO_MARKER\"\nexit 1\n"), 0755))
			args := []string{"-test.run=^TestUpdateProcess$", "update"}
			if tc.plain {
				args = append(args, "--output", "plain")
			}
			if tc.plain && tc.expectFailure {
				args = append(args, "--force")
			}
			cmd := exec.CommandContext(t.Context(), dest, args...)
			cmd.Env = append(os.Environ(), "COPERA_TEST_UPDATE_CDN="+server.URL,
				"COPERA_TEST_SUDO_MARKER="+sudoMarker, "PATH="+tmp+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TMPDIR="+tmp, "CI=false")
			if tc.plain && !tc.expectFailure {
				cmd.Env = append(cmd.Env, "_COPERA_UPDATE_TARGET="+filepath.Join(tmp, "stale-copera"),
					"_COPERA_UPDATE_VERSION=0.1.0")
			}
			cmd.Stdin = strings.NewReader("n\n")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			assert.NoFileExists(t, sudoMarker, "sudo ran without a terminal")
			assert.NotContains(t, stdout.String(), "Continue?")
			if tc.expectFailure {
				assert.Error(t, err)
				assert.Contains(t, stderr.String(), `"error":"update_failed"`)
				if tc.badChecksum {
					assert.Contains(t, stderr.String(), "checksum mismatch")
					assert.Empty(t, stdout.String())
				} else {
					assert.Contains(t, stderr.String(), "sudo "+dest+" update --version "+nextUpdateVersion)
				}
			} else {
				assert.NoError(t, err, "%s", stderr.String())
				if tc.plain {
					assert.Contains(t, stderr.String(), "Updated to v"+nextUpdateVersion)
				} else {
					action := "updated"
					if tc.latest == installedUpdateVersion {
						action = "up_to_date"
					}
					assert.JSONEq(t, fmt.Sprintf(`{"current":%q,"target":%q,"action":%q}`,
						installedUpdateVersion, tc.latest, action), stdout.String())
				}
			}
			installed, err := os.ReadFile(dest)
			require.NoError(t, err)
			if tc.expectFailure || tc.latest == installedUpdateVersion {
				assert.True(t, bytes.Equal(oldBinary, installed), "original binary changed")
			} else {
				assert.True(t, bytes.Equal(newBinary, installed), "new binary did not install")
			}
		})
	}
}
