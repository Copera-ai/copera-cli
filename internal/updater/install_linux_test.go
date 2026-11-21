//go:build linux

package updater

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagedUpdater(t *testing.T) {
	if wantError, child := os.LookupEnv("COPERA_TEST_STAGED_UPDATER"); child {
		_, resumed, err := ResumeInstall()
		require.True(t, resumed)
		if wantError == "" {
			require.NoError(t, err)
		} else {
			require.ErrorContains(t, err, wantError)
		}
		return
	}

	self, err := os.Executable()
	require.NoError(t, err)
	newBinary := []byte("verified binary\n")
	for _, tc := range []struct {
		name      string
		wantError string
	}{
		{"unrelated", "update target does not match the running binary"},
		{"symlink", "open current binary for update"},
		{"hard_link", "exactly one link"},
		{"relative", "absolute, clean path"},
		{"changed_staged_binary", "checksum mismatch for staged binary"},
		{"legitimate", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			dir := filepath.Join(tmp, "copera-update-test")
			require.NoError(t, os.Mkdir(dir, 0700))
			runner := filepath.Join(dir, installRunner)
			require.NoError(t, copyFile(self, runner, 0700))

			staged := newBinary
			if tc.name == "changed_staged_binary" {
				staged = []byte("changed binary\n")
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "copera"), staged, 0700))

			dest := filepath.Join(tmp, "installed-copera")
			if tc.name == "unrelated" {
				dest = filepath.Join(tmp, "authorized_keys")
				require.NoError(t, os.WriteFile(dest, []byte("fake authorized keys\n"), 0600))
			} else {
				require.NoError(t, copyFile(self, dest, 0755))
			}
			target := dest
			switch tc.name {
			case "symlink":
				target = filepath.Join(tmp, "symlink")
				require.NoError(t, os.Symlink(dest, target))
			case "hard_link":
				target = filepath.Join(tmp, "hard-link")
				require.NoError(t, os.Link(dest, target))
			case "relative":
				target = filepath.Base(dest)
			}
			before, err := os.ReadFile(dest)
			require.NoError(t, err)

			cmd := exec.CommandContext(t.Context(), runner, "-test.run=^TestStagedUpdater$")
			cmd.Dir = tmp
			cmd.Env = append(cmd.Environ(), "TMPDIR="+tmp, "COPERA_TEST_STAGED_UPDATER="+tc.wantError,
				installTarget+"="+target, fmt.Sprintf("%s=%x", installSHA256, sha256.Sum256(newBinary)))
			output, runErr := cmd.CombinedOutput()
			installed, err := os.ReadFile(dest)
			require.NoError(t, err)
			if tc.wantError == "" {
				require.Equal(t, newBinary, installed)
			} else {
				require.True(t, bytes.Equal(before, installed), "rejected update changed %s", dest)
				require.NoFileExists(t, filepath.Join(dir, "copera.backup"))
			}
			require.NoError(t, runErr, "%s", output)
		})
	}
}

func TestSudoInstallReplacesRunningBinary(t *testing.T) {
	dir := t.TempDir()
	sleep, err := exec.LookPath("sleep")
	require.NoError(t, err)
	dest := filepath.Join(dir, "sleep")
	require.NoError(t, copyFile(sleep, dest, 0755))
	process := exec.Command(dest, "60")
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})

	require.NoError(t, os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\nexec \"$@\"\n"), 0755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	src := filepath.Join(t.TempDir(), "new-sleep")
	newBinary := []byte("new binary\n")
	require.NoError(t, os.WriteFile(src, newBinary, 0600))

	require.NoError(t, sudoInstall(src, dest))
	installed, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, newBinary, installed)
	info, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0755), info.Mode().Perm())
}
