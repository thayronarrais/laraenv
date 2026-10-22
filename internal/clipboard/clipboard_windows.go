//go:build windows

package clipboard

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const unicodeText = 13 // CF_UNICODETEXT

var (
	clipboardMu      sync.Mutex
	user32           = windows.NewLazySystemDLL("user32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	isTextAvailable  = user32.NewProc("IsClipboardFormatAvailable")
	getClipboardData = user32.NewProc("GetClipboardData")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
)

// These operations form the seam for tests that must never access the user's
// clipboard. Native allocation and unlock behavior are also tested separately.
type clipboardAPI struct {
	open         func(uintptr) error
	close        func() error
	hasText      func() bool
	get          func() (uintptr, error)
	empty        func() error
	set          func(uintptr) error
	alloc        func(uintptr) (uintptr, error)
	free         func(uintptr) error
	size         func(uintptr) (uintptr, error)
	lock         func(uintptr) (unsafe.Pointer, error)
	unlock       func(uintptr) error
	owner        func() (uintptr, error)
	destroyOwner func(uintptr) error
}

func requireSuccess(proc *windows.LazyProc, args ...uintptr) error {
	result, _, err := proc.Call(args...)
	if result == 0 {
		return winError(proc.Name, err)
	}
	return nil
}

func nativeAPI() clipboardAPI {
	return clipboardAPI{
		open:    func(owner uintptr) error { return requireSuccess(openClipboard, owner) },
		close:   func() error { return requireSuccess(closeClipboard) },
		hasText: func() bool { result, _, _ := isTextAvailable.Call(unicodeText); return result != 0 },
		get: func() (uintptr, error) {
			handle, _, err := getClipboardData.Call(unicodeText)
			if handle == 0 {
				return 0, winError("GetClipboardData", err)
			}
			return handle, nil
		},
		empty:        func() error { return requireSuccess(emptyClipboard) },
		set:          func(handle uintptr) error { return requireSuccess(setClipboardData, unicodeText, handle) },
		alloc:        allocMemory,
		free:         freeMemory,
		size:         memorySize,
		lock:         lockMemory,
		unlock:       unlockMemory,
		owner:        newOwnerWindow,
		destroyOwner: func(owner uintptr) error { return requireSuccess(destroyWindow, owner) },
	}
}

func newOwnerWindow() (uintptr, error) {
	// SetClipboardData requires a non-null owner after EmptyClipboard. A
	// message-only window owns this immediate text transfer without appearing
	// on screen. Its lifetime stays on the locked OS thread.
	class := syscall.StringToUTF16Ptr("STATIC")
	window, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0,
		0, 0, 0, 0, ^uintptr(2), 0, 0, 0) // HWND_MESSAGE = -3
	if window == 0 {
		return 0, winError("CreateWindowExW", err)
	}
	return window, nil
}

func openWithRetry(open func(uintptr) error, owner uintptr, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := open(owner)
		if err == nil {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("clipboard remained busy: %w", err)
		}
		time.Sleep(min(10*time.Millisecond, remaining))
	}
}

// ReadText returns an empty string when the clipboard contains no text.
func ReadText(_ context.Context) (string, error) {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return readNative(nativeAPI())
}

func readNative(api clipboardAPI) (text string, err error) {
	if err := openWithRetry(api.open, 0, time.Second); err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, api.close()) }()
	if !api.hasText() {
		return "", nil
	}
	handle, err := api.get()
	if err != nil {
		return "", err
	}
	bytes, err := api.size(handle)
	if err != nil {
		return "", err
	}
	if bytes < 2 || bytes%2 != 0 || bytes > uintptr(^uint(0)>>1) {
		return "", errors.New("clipboard text has an invalid allocation size")
	}
	ptr, err := api.lock(handle)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, api.unlock(handle)) }()
	// The clipboard owns the handle: release its lock, never free its memory.
	return decodeText(unsafe.Slice((*uint16)(ptr), int(bytes/2)))
}

// WriteText prepares all memory before replacing the existing clipboard text.
func WriteText(_ context.Context, text string) error {
	clipboardMu.Lock()
	defer clipboardMu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return writeNative(nativeAPI(), text)
}

func writeNative(api clipboardAPI, text string) (err error) {
	data, err := encodeText(text)
	if err != nil {
		return err
	}
	handle, err := api.alloc(uintptr(len(data)) * 2)
	if err != nil {
		return err
	}
	transferred := false
	defer func() {
		if !transferred {
			err = errors.Join(err, api.free(handle))
		}
	}()
	ptr, err := api.lock(handle)
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*uint16)(ptr), len(data)), data)
	if err := api.unlock(handle); err != nil {
		// Retry releasing the lock before the deferred free on a failed unlock.
		return errors.Join(err, api.unlock(handle))
	}
	owner, err := api.owner()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, api.destroyOwner(owner)) }()
	if err := openWithRetry(api.open, owner, time.Second); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, api.close()) }()
	if err := api.empty(); err != nil {
		return err
	}
	if err := api.set(handle); err != nil {
		return err
	}
	transferred = true // Windows owns the allocation after SetClipboardData.
	return nil
}
