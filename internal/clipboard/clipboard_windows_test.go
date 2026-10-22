//go:build windows

package clipboard

import (
	"errors"
	"reflect"
	"testing"
	"time"
	"unsafe"
)

// The fake replaces only Win32 clipboard calls. Memory handling and ownership
// orchestration still run through the production code, without opening or
// changing the user's clipboard.
type fakeClipboard struct {
	data        []uint16
	steps       []string
	fail        string
	available   bool
	openedOwner uintptr
}

func (f *fakeClipboard) step(name string) error {
	f.steps = append(f.steps, name)
	if name == f.fail {
		return errors.New(name + " failed")
	}
	return nil
}

func (f *fakeClipboard) api() clipboardAPI {
	return clipboardAPI{
		open:    func(owner uintptr) error { f.openedOwner = owner; return f.step("open") },
		close:   func() error { return f.step("close") },
		hasText: func() bool { return f.available },
		get:     func() (uintptr, error) { return 1, f.step("get") },
		empty:   func() error { return f.step("empty") },
		set:     func(h uintptr) error { return f.step("set") },
		alloc: func(bytes uintptr) (uintptr, error) {
			f.data = make([]uint16, bytes/2)
			return 1, f.step("alloc")
		},
		free: func(h uintptr) error { return f.step("free") },
		size: func(h uintptr) (uintptr, error) { return uintptr(len(f.data) * 2), f.step("size") },
		lock: func(h uintptr) (unsafe.Pointer, error) {
			if err := f.step("lock"); err != nil {
				return nil, err
			}
			return unsafe.Pointer(&f.data[0]), nil
		},
		unlock:       func(h uintptr) error { return f.step("unlock") },
		owner:        func() (uintptr, error) { return 2, f.step("owner") },
		destroyOwner: func(h uintptr) error { return f.step("destroyOwner") },
	}
}

func TestReadNativeClipboardUsesBoundedMemoryAndReleasesIt(t *testing.T) {
	f := &fakeClipboard{data: []uint16{'h', 'i', 0, 'x'}, available: true}
	got, err := readNative(f.api())
	if err != nil || got != "hi" {
		t.Fatalf("read=%q error=%v", got, err)
	}
	want := []string{"open", "get", "size", "lock", "unlock", "close"}
	if !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("cleanup=%v, want %v", f.steps, want)
	}
	if f.data[0] != 'h' || f.data[3] != 'x' {
		t.Fatal("read changed clipboard data")
	}
	if f.openedOwner != 0 {
		t.Fatal("reading must not take clipboard ownership")
	}
}

func TestReadNativeClipboardClosesOnFailure(t *testing.T) {
	for _, fail := range []string{"get", "size", "lock", "unlock"} {
		t.Run(fail, func(t *testing.T) {
			f := &fakeClipboard{data: []uint16{'x', 0}, available: true, fail: fail}
			if _, err := readNative(f.api()); err == nil {
				t.Fatal("expected failure")
			}
			if f.steps[len(f.steps)-1] != "close" {
				t.Fatalf("clipboard stayed open: %v", f.steps)
			}
		})
	}
	f := &fakeClipboard{}
	if got, err := readNative(f.api()); err != nil || got != "" {
		t.Fatalf("nontext read=%q, %v", got, err)
	}
	if !reflect.DeepEqual(f.steps, []string{"open", "close"}) {
		t.Fatal(f.steps)
	}
}

func TestWriteNativeClipboardPreparesBeforeEmptyingAndTransfersOwnership(t *testing.T) {
	f := &fakeClipboard{}
	if err := writeNative(f.api(), "🦊\n"); err != nil {
		t.Fatal(err)
	}
	want := []string{"alloc", "lock", "unlock", "owner", "open", "empty", "set", "close", "destroyOwner"}
	if !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("ownership=%v, want %v", f.steps, want)
	}
	if got, err := decodeText(f.data); err != nil || got != "🦊\n" {
		t.Fatalf("text=%q, %v", got, err)
	}
	if f.openedOwner != 2 {
		t.Fatal("copy must use the valid owner window")
	}
}

func TestReadNativeClipboardRejectsInvalidAllocationBeforeLocking(t *testing.T) {
	for _, bytes := range []uintptr{0, 1, 3, ^uintptr(0)} {
		f := &fakeClipboard{available: true}
		api := f.api()
		api.size = func(uintptr) (uintptr, error) { return bytes, nil }
		if _, err := readNative(api); err == nil {
			t.Fatalf("accepted allocation of %d bytes", bytes)
		}
		for _, step := range f.steps {
			if step == "lock" {
				t.Fatal("locked invalid allocation")
			}
		}
		if f.steps[len(f.steps)-1] != "close" {
			t.Fatal("clipboard remained open")
		}
	}
}

func TestWriteNativeClipboardPreservesOldContentBeforePreparationSucceeds(t *testing.T) {
	for _, fail := range []string{"alloc", "lock", "unlock", "owner"} {
		t.Run(fail, func(t *testing.T) {
			f := &fakeClipboard{fail: fail}
			if err := writeNative(f.api(), "new"); err == nil {
				t.Fatal("expected failure")
			}
			for _, step := range f.steps {
				if step == "open" || step == "empty" || step == "set" {
					t.Fatalf("clipboard touched before preparation succeeded: %v", f.steps)
				}
			}
			if fail != "alloc" && f.steps[len(f.steps)-1] != "free" {
				t.Fatalf("memory leaked: %v", f.steps)
			}
		})
	}
	f := &fakeClipboard{}
	if err := writeNative(f.api(), "invalid\x00text"); err == nil {
		t.Fatal("expected validation error")
	}
	if len(f.steps) != 0 {
		t.Fatalf("clipboard or memory touched for invalid text: %v", f.steps)
	}
}

func TestWriteNativeClipboardFreesMemoryAndClosesOnTransferFailure(t *testing.T) {
	f := &fakeClipboard{fail: "set"}
	if err := writeNative(f.api(), "new"); err == nil {
		t.Fatal("expected failure")
	}
	want := []string{"alloc", "lock", "unlock", "owner", "open", "empty", "set", "close", "destroyOwner", "free"}
	if !reflect.DeepEqual(f.steps, want) {
		t.Fatalf("failure cleanup=%v, want %v", f.steps, want)
	}
}

func TestOpenClipboardRetriesAndStopsAtDeadline(t *testing.T) {
	calls := 0
	errBusy := errors.New("busy")
	open := func(uintptr) error {
		calls++
		if calls < 3 {
			return errBusy
		}
		return nil
	}
	if err := openWithRetry(open, 0, 100*time.Millisecond); err != nil || calls != 3 {
		t.Fatalf("attempts=%d error=%v", calls, err)
	}
	calls = 0
	open = func(uintptr) error { calls++; return errBusy }
	if err := openWithRetry(open, 0, 15*time.Millisecond); !errors.Is(err, errBusy) || calls < 2 {
		t.Fatalf("attempts=%d error=%v", calls, err)
	}
}
