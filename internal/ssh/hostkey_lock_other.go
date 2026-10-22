//go:build !windows

package ssh

import (
	"golang.org/x/sys/unix"
	"os"
)

func openKnownHosts(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
}

func lockKnownHosts(file *os.File) error { return unix.Flock(int(file.Fd()), unix.LOCK_EX) }
func unlockKnownHosts(file *os.File)     { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }
