//go:build windows

package updater

import (
	"fmt"
	"os"
	"strings"
)

// ResumeInstall has no work on Windows, where the running binary can be renamed.
func ResumeInstall() (string, bool, error) { return "", false, nil }

func installBinary(src, dest, version string, _ bool) error {
	// On Windows, rename the running binary to .old (Windows allows renaming open files)
	// then copy the new one into place.
	oldPath := dest + ".old"
	_ = os.Remove(oldPath) // clean up from previous update

	if err := os.Rename(dest, oldPath); err != nil {
		return fmt.Errorf("backup current binary: %w; run in an administrator PowerShell: & %s update --version %s",
			err, powerShellArg(dest), powerShellArg(version))
	}

	if err := copyFile(src, dest, 0755); err != nil {
		_ = os.Rename(oldPath, dest)
		return fmt.Errorf("install new binary: %w", err)
	}

	// Schedule cleanup — .old will be removed on next update or manually
	// (can't delete while the old process may still reference it)
	return nil
}

func powerShellArg(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", "''") + "'"
}
