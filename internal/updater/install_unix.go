//go:build !windows

package updater

import (
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func installBinary(src, dest string) error {
	dir := filepath.Dir(dest)

	if isWritable(dir) {
		return directInstall(src, dest)
	}

	// Escalate to sudo on Unix
	return sudoInstall(src, dest)
}

func directInstall(src, dest string) error {
	backupPath := dest + ".backup"
	if err := os.Rename(dest, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}

	if err := copyFile(src, dest, 0755); err != nil {
		_ = os.Rename(backupPath, dest)
		return fmt.Errorf("install new binary: %w", err)
	}

	_ = os.Remove(backupPath)
	return nil
}

func sudoInstall(src, dest string) error {
	// Stage next to dest so mv can replace the running binary atomically.
	staged := filepath.Join(filepath.Dir(dest), ".copera-new-"+rand.Text())
	if err := runSudo("install", "-m", "0755", src, staged); err != nil {
		_ = runSudo("rm", "-f", staged)
		return fmt.Errorf("sudo install: %w", err)
	}

	if err := runSudo("mv", "-f", staged, dest); err != nil {
		_ = runSudo("rm", "-f", staged)
		return fmt.Errorf("sudo mv: %w", err)
	}

	return nil
}

var runSudo = func(args ...string) error {
	cmd := exec.Command("sudo", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func isWritable(dir string) bool {
	tmp := filepath.Join(dir, ".copera-write-test")
	f, err := os.Create(tmp)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(tmp)
	return true
}
