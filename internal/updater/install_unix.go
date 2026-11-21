//go:build !windows

package updater

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
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
	installSHA256  = "_COPERA_UPDATE_SHA256"
)

func installBinary(src, dest, version string, interactive bool) error {
	dir := filepath.Dir(dest)

	if isWritable(dir) {
		return directInstall(src, dest)
	}

	if runtime.GOOS != "darwin" && unix.Access(dest, unix.W_OK) == nil {
		// Linux refuses writes to an executing inode. Re-exec the current
		// CLI from the staging directory so it can write the original file.
		source, err := os.Open(src)
		if err != nil {
			return fmt.Errorf("open staged binary: %w", err)
		}
		checksum, err := stagedBinaryChecksum(source)
		_ = source.Close()
		if err != nil {
			return fmt.Errorf("hash staged binary: %w", err)
		}
		runner := filepath.Join(filepath.Dir(src), installRunner)
		if err := copyFile(dest, runner, 0700); err != nil {
			return fmt.Errorf("stage updater: %w", err)
		}
		env := slices.DeleteFunc(os.Environ(), func(entry string) bool {
			return strings.HasPrefix(entry, installTarget+"=") || strings.HasPrefix(entry, installVersion+"=") ||
				strings.HasPrefix(entry, installSHA256+"=")
		})
		env = append(env, installTarget+"="+dest, installVersion+"="+version, installSHA256+"="+checksum)
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
	if dest == "" || runtime.GOOS == "darwin" {
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
	if err := overwriteBinary(src, dest, os.Getenv(installSHA256)); err != nil {
		return version, true, err
	}
	_ = os.Remove(filepath.Join(cache.SharedDir(), "version-check.json"))
	return version, true, nil
}

func overwriteBinary(src, dest, expectedChecksum string) error {
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
	saved, err := os.OpenFile(backup, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("open backup for current binary: %w", err)
	}
	keepBackup := false
	defer func() {
		_ = saved.Close()
		if !keepBackup {
			_ = os.Remove(backup)
		}
	}()
	_, err = io.Copy(saved, file)
	if err := errors.Join(err, saved.Sync()); err != nil {
		return fmt.Errorf("back up current binary: %w", err)
	}

	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open staged binary: %w", err)
	}
	defer source.Close()
	checksum, err := stagedBinaryChecksum(source)
	if err != nil {
		return fmt.Errorf("hash staged binary: %w", err)
	}
	if checksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch for staged binary")
	}
	signal.Ignore(unix.SIGINT, unix.SIGTERM, unix.SIGHUP)
	defer signal.Reset(unix.SIGINT, unix.SIGTERM, unix.SIGHUP)
	fmt.Fprintf(os.Stderr, "Update backup: %q\n", backup)
	if err := replaceBinaryContents(file, source); err != nil {
		if restoreErr := replaceBinaryContents(file, saved); restoreErr != nil {
			keepBackup = true
			return fmt.Errorf("install new binary: %w, recovery failed: %v (backup at %s)", err, restoreErr, backup)
		}
		return fmt.Errorf("install new binary: %w", err)
	}
	return nil
}

func stagedBinaryChecksum(source io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, source); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func replaceBinaryContents(dest, src *os.File) error {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := dest.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := dest.Truncate(0); err != nil {
		return err
	}
	if _, err := io.Copy(dest, src); err != nil {
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
