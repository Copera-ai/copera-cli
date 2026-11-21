//go:build !windows

package updater

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSudoInstallCommands(t *testing.T) {
	originalRunner := runSudo
	t.Cleanup(func() { runSudo = originalRunner })
	dest := filepath.Join(t.TempDir(), "copera")
	src := filepath.Join(t.TempDir(), "copera")

	t.Run("stages before replacing", func(t *testing.T) {
		var commands [][]string
		runSudo = func(args ...string) error {
			commands = append(commands, args)
			return nil
		}

		require.NoError(t, sudoInstall(src, dest))
		require.Len(t, commands, 2)
		require.NotEmpty(t, commands[0])
		staged := commands[0][len(commands[0])-1]
		require.NotEqual(t, dest, staged, "the elevated copy must not target the running binary")
		require.Equal(t, filepath.Dir(dest), filepath.Dir(staged))
		require.Equal(t, []string{"install", "-m", "0755", src, staged}, commands[0])
		require.Equal(t, []string{"mv", "-f", staged, dest}, commands[1])
	})

	t.Run("cleans up after copy failure", func(t *testing.T) {
		copyErr := errors.New("copy failed")
		var commands [][]string
		runSudo = func(args ...string) error {
			commands = append(commands, args)
			if len(commands) == 1 {
				return copyErr
			}
			return nil
		}

		require.ErrorIs(t, sudoInstall(src, dest), copyErr)
		require.Len(t, commands, 2, "expected only the copy attempt and cleanup")
		staged := commands[0][len(commands[0])-1]
		require.NotEqual(t, dest, staged)
		require.Equal(t, []string{"rm", "-f", staged}, commands[1])
	})
}

func TestInstallBinaryWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "copera")
	src := filepath.Join(dir, "new-copera")
	newBinary := []byte("new binary\n")
	require.NoError(t, os.WriteFile(dest, []byte("old binary\n"), 0755))
	require.NoError(t, os.WriteFile(src, newBinary, 0600))

	originalRunner := runSudo
	t.Cleanup(func() { runSudo = originalRunner })
	runSudo = func(args ...string) error {
		t.Fatalf("writable install called sudo %q", args)
		return nil
	}

	require.NoError(t, installBinary(src, dest))
	installed, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, newBinary, installed)
	require.NoFileExists(t, dest+".backup")
}
