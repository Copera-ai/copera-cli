//go:build linux

package updater

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

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
