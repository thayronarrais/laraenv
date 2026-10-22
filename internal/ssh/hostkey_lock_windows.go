//go:build windows

package ssh

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

// OpenSSH does not acquire LaraEnv's sidecar lock. Holding a writable handle
// without FILE_SHARE_WRITE/DELETE closes the check/append race with any other
// Windows writer, while still allowing knownhosts.New to open it for reading.
func openKnownHosts(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == windows.ERROR_ACCESS_DENIED {
		// Existing trust remains usable when its file is read-only. Keep
		// excluding external writers/deletion; an unknown key will fail its
		// append rather than being accepted without durable persistence.
		handle, err = windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("open SSH known_hosts handle")
	}
	return file, nil
}

func lockKnownHosts(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
}
func unlockKnownHosts(file *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, &windows.Overlapped{})
}
