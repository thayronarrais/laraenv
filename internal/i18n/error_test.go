package i18n

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// withGlobal points the global catalogue set and active language at a known
// value for the duration of a test, then restores whatever was there before.
// Both are package-level state shared with every other test in this package
// (and, in the real binary, with every backend package), so a test that
// touches them must never leak a change to the next one — set is nil-able:
// pass nil and the current Active() to test the no-catalogues-loaded case
// without disturbing the active language.
func withGlobal(t *testing.T, set *Set, lang string) {
	t.Helper()
	prevGlobal := Global()
	prevActive := Active()
	SetGlobal(set)
	SetActive(lang)
	t.Cleanup(func() {
		SetGlobal(prevGlobal)
		SetActive(prevActive)
	})
}

func TestErrorNilGlobalFallsBackToKey(t *testing.T) {
	withGlobal(t, nil, Active())
	err := Errorf("errors.vhost.certWriteFailed", map[string]any{"name": "shop"})
	if got, want := err.Error(), "errors.vhost.certWriteFailed"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestErrorRendersFromGlobalInActiveLanguage(t *testing.T) {
	set, err := Load(os.DirFS("testdata"), ".")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	withGlobal(t, set, "en")

	e := Errorf("projects.created", map[string]any{"name": "shop"})
	if got, want := e.Error(), "Project shop created"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}

	// Switching the active language changes what an *already-constructed*
	// error renders as — the whole point of carrying a key instead of a
	// baked-in sentence baked in at construction time.
	SetActive("ru")
	if got, want := e.Error(), "Проект shop создан"; got != want {
		t.Fatalf("Error() after SetActive(ru) = %q, want %q", got, want)
	}
}

func TestErrorAppendsWrappedErrorLikeFmtErrorfW(t *testing.T) {
	set, err := Load(os.DirFS("testdata"), ".")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	withGlobal(t, set, "en")

	inner := fs.ErrNotExist
	wrapped := Wrapf(inner, "projects.title", nil)
	if got, want := wrapped.Error(), "Projects: file does not exist"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

// TestUnwrapPreservesErrorsIsAndAs is the one that matters most: silently
// breaking errors.Is/errors.As across a codebase with hundreds of callers
// checking for sentinel and typed errors would be far worse than an
// untranslated string.
func TestUnwrapPreservesErrorsIsAndAs(t *testing.T) {
	sentinel := os.ErrPermission
	wrapped := Wrapf(sentinel, "errors.services.startFailed", map[string]any{"name": "nginx"})
	if !errors.Is(wrapped, sentinel) {
		t.Fatal("errors.Is(wrapped, sentinel) = false, want true")
	}

	var pathErr *fs.PathError
	pathWrapped := Wrapf(&fs.PathError{Op: "open", Path: "/tmp/x", Err: os.ErrNotExist}, "errors.vhost.certWriteFailed", nil)
	if !errors.As(pathWrapped, &pathErr) {
		t.Fatal("errors.As(pathWrapped, &pathErr) = false, want true")
	}
	if pathErr.Path != "/tmp/x" {
		t.Fatalf("unwrapped PathError.Path = %q, want /tmp/x", pathErr.Path)
	}
}

func TestErrorfHasNoWrappedError(t *testing.T) {
	err := Errorf("errors.vhost.certWriteFailed", nil)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatal("errors.As failed to recover *Error")
	}
	if e.Unwrap() != nil {
		t.Fatalf("Unwrap() = %v, want nil", e.Unwrap())
	}
}

func TestWrapfNilErrReturnsNil(t *testing.T) {
	if err := Wrapf(nil, "errors.services.startFailed", map[string]any{"name": "nginx"}); err != nil {
		t.Fatalf("Wrapf(nil, ...) = %v, want nil", err)
	}
}

func TestSetActiveDefaultsEmptyToFallback(t *testing.T) {
	prev := Active()
	t.Cleanup(func() { SetActive(prev) })

	SetActive("")
	if got := Active(); got != Fallback {
		t.Fatalf("Active() after SetActive(\"\") = %q, want %q", got, Fallback)
	}
}
