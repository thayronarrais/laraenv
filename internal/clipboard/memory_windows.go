//go:build windows

package clipboard

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32     = windows.NewLazySystemDLL("kernel32.dll")
	globalAlloc  = kernel32.NewProc("GlobalAlloc")
	globalFree   = kernel32.NewProc("GlobalFree")
	globalLock   = kernel32.NewProc("GlobalLock")
	globalUnlock = kernel32.NewProc("GlobalUnlock")
	globalSize   = kernel32.NewProc("GlobalSize")
)

func winError(operation string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func allocMemory(bytes uintptr) (uintptr, error) {
	handle, _, err := globalAlloc.Call(0x0002, bytes) // GMEM_MOVEABLE
	if handle == 0 {
		return 0, winError("GlobalAlloc", err)
	}
	return handle, nil
}

func freeMemory(handle uintptr) error {
	remaining, _, err := globalFree.Call(handle)
	if remaining != 0 {
		return winError("GlobalFree", err)
	}
	return nil
}

func lockMemory(handle uintptr) (unsafe.Pointer, error) {
	ptr, _, err := globalLock.Call(handle)
	if ptr == 0 {
		return nil, winError("GlobalLock", err)
	}
	// GlobalLock returns an address in Windows-owned memory, not a Go heap
	// pointer. Reinterpret the syscall word without a uintptr-to-pointer
	// conversion that Go's pointer checker treats as a stale Go address.
	return *(*unsafe.Pointer)(unsafe.Pointer(&ptr)), nil
}

func unlockMemory(handle uintptr) error {
	stillLocked, _, err := globalUnlock.Call(handle)
	// Zero means the final lock was released successfully when GetLastError
	// is ERROR_SUCCESS. Proc.Call still returns a non-nil syscall.Errno(0).
	if stillLocked == 0 && err != syscall.Errno(0) {
		return winError("GlobalUnlock", err)
	}
	return nil
}

func memorySize(handle uintptr) (uintptr, error) {
	bytes, _, err := globalSize.Call(handle)
	if bytes == 0 {
		return 0, winError("GlobalSize", err)
	}
	return bytes, nil
}
