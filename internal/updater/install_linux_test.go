//go:build linux

package updater

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStagedUpdaterRejectsChangedBinary(t *testing.T) {
	if os.Getenv("COPERA_TEST_STAGED_UPDATER") == "1" {
		_, resumed, err := ResumeInstall()
		require.True(t, resumed)
		require.ErrorContains(t, err, "checksum mismatch")
		return
	}

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "copera-update-test")
	require.NoError(t, os.Mkdir(dir, 0700))
	self, err := os.Executable()
	require.NoError(t, err)
	runner := filepath.Join(dir, installRunner)
	require.NoError(t, copyFile(self, runner, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "copera"), []byte("changed binary\n"), 0700))
	dest := filepath.Join(t.TempDir(), "copera")
	oldBinary := []byte("old binary\n")
	require.NoError(t, os.WriteFile(dest, oldBinary, 0755))

	cmd := exec.CommandContext(t.Context(), runner, "-test.run=^TestStagedUpdaterRejectsChangedBinary$")
	cmd.Env = append(os.Environ(), "TMPDIR="+tmp, "COPERA_TEST_STAGED_UPDATER=1", installTarget+"="+dest,
		fmt.Sprintf("%s=%x", installSHA256, sha256.Sum256([]byte("verified binary\n"))))
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	installed, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, oldBinary, installed)
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
