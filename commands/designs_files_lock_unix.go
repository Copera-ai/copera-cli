//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package commands

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func lockDesignStateFile(file *os.File, wait bool) error {
	flags := unix.LOCK_EX
	if !wait {
		flags |= unix.LOCK_NB
	}
	for {
		err := unix.Flock(int(file.Fd()), flags)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.EWOULDBLOCK) {
			return errDesignStateBusy
		}
		return err
	}
}
