//go:build !windows

package updater

import (
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
	cmd := exec.Command("sudo", "cp", src, dest)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo cp: %w", err)
	}

	cmd = exec.Command("sudo", "chmod", "755", dest)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo chmod: %w", err)
	}

	return nil
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
