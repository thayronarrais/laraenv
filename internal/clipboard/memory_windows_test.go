//go:build windows

package clipboard

import (
	"runtime"
	"testing"
	"unsafe"
)

// This uses a fresh memory allocation, never the system clipboard. The final
// GlobalUnlock returns zero on success; the Wails 2.12 reader discards the text
// because it treats that return value as an error.
func TestFinalGlobalUnlockSucceeds(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	handle, err := allocMemory(8)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := freeMemory(handle); err != nil {
			t.Error(err)
		}
	})
	ptr, err := lockMemory(handle)
	if err != nil {
		t.Fatal(err)
	}
	copy(unsafe.Slice((*uint16)(ptr), 4), []uint16{'o', 'k', 0, 0})
	if err := unlockMemory(handle); err != nil {
		t.Fatalf("final unlock of valid clipboard-style memory must succeed: %v", err)
	}
	ptr, err = lockMemory(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got := unsafe.Slice((*uint16)(ptr), 3); got[0] != 'o' || got[1] != 'k' || got[2] != 0 {
		t.Errorf("memory changed after unlock: %v", got)
	}
	if err := unlockMemory(handle); err != nil {
		t.Error(err)
	}
}

func TestMessageOnlyOwnerWindowLifecycle(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	owner, err := newOwnerWindow()
	if err != nil {
		t.Fatal(err)
	}
	if err := requireSuccess(destroyWindow, owner); err != nil {
		t.Fatal(err)
	}
}
