//go:build !windows

package updater

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/copera/copera-cli/internal/cache"
	"golang.org/x/sys/unix"
)

const (
	installRunner  = "copera-update-runner"
	installTarget  = "_COPERA_UPDATE_TARGET"
	installVersion = "_COPERA_UPDATE_VERSION"
)

func installBinary(src, dest, version string, interactive bool) error {
	dir := filepath.Dir(dest)

	if isWritable(dir) {
		return directInstall(src, dest)
	}

	if unix.Access(dest, unix.W_OK) == nil {
		// Linux refuses writes to an executing inode. Re-exec the current
		// CLI from the staging directory so it can write the original file.
		runner := filepath.Join(filepath.Dir(src), installRunner)
		if err := copyFile(dest, runner, 0700); err != nil {
			return fmt.Errorf("stage updater: %w", err)
		}
		env := slices.DeleteFunc(os.Environ(), func(entry string) bool {
			return strings.HasPrefix(entry, installTarget+"=") || strings.HasPrefix(entry, installVersion+"=")
		})
		env = append(env, installTarget+"="+dest, installVersion+"="+version)
		return fmt.Errorf("run staged updater: %w", unix.Exec(runner, os.Args, env))
	}
	if interactive {
		return sudoInstall(src, dest)
	}
	return fmt.Errorf("cannot write %s; run in a terminal: sudo %s update --version %s",
		dest, shellArg(dest), shellArg(version))
}

// ResumeInstall finishes an in-place update after the CLI re-execs from staging.
func ResumeInstall() (version string, resumed bool, err error) {
	dest := os.Getenv(installTarget)
	if dest == "" {
		return "", false, nil
	}
	self, err := os.Executable()
	dir := filepath.Dir(self)
	if err != nil || filepath.Base(self) != installRunner || !strings.HasPrefix(filepath.Base(dir), "copera-update-") {
		return "", false, nil
	}
	version = os.Getenv(installVersion)
	src := filepath.Join(dir, "copera")
	defer func() {
		_ = os.Remove(src)
		_ = os.Remove(self)
		_ = os.Remove(filepath.Join(dir, "archive.tar.gz"))
		_ = os.Remove(filepath.Join(dir, "checksums.txt"))
		_ = os.Remove(dir)
	}()
	if err := overwriteBinary(src, dest); err != nil {
		return version, true, err
	}
	_ = os.Remove(filepath.Join(cache.SharedDir(), "version-check.json"))
	return version, true, nil
}

func overwriteBinary(src, dest string) error {
	// The kernel can keep the old image busy briefly after exec.
	deadline := time.Now().Add(time.Second)
	file, err := os.OpenFile(dest, os.O_RDWR, 0)
	for errors.Is(err, unix.ETXTBSY) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		file, err = os.OpenFile(dest, os.O_RDWR, 0)
	}
	if err != nil {
		return fmt.Errorf("open current binary for update: %w", err)
	}
	defer file.Close()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("lock current binary for update: %w", err)
	}

	backup := filepath.Join(filepath.Dir(src), "copera.backup")
	saved, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("open backup for current binary: %w", err)
	}
	_, err = io.Copy(saved, file)
	if err := errors.Join(err, saved.Close()); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("back up current binary: %w", err)
	}
	if err := replaceBinaryContents(file, src); err != nil {
		if restoreErr := replaceBinaryContents(file, backup); restoreErr != nil {
			return fmt.Errorf("install new binary: %w, recovery failed: %v (backup at %s)", err, restoreErr, backup)
		}
		_ = os.Remove(backup)
		return fmt.Errorf("install new binary: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func replaceBinaryContents(dest *os.File, src string) error {
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := dest.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := dest.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(dest, file); err != nil {
		return err
	}
	return dest.Sync()
}

func shellArg(arg string) string {
	if strings.ContainsAny(arg, " \t\n'\"\\`$|&;<>()*?!{}[]#~") {
		return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return arg
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
	f, err := os.CreateTemp(dir, ".copera-write-test-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}
