//go:build windows

package webviewruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRecoveryLeavesHealthyInstalledDiscoveryAlone(t *testing.T) {
	// Scanning after a successful normal query could pin a healthy Evergreen app.
	folder, err := recoveryBrowserFolder(func() (string, error) { return "155.0.4283.62", nil }, func() ([]string, error) {
		t.Fatal("healthy discovery must not enumerate recovery directories")
		return nil, nil
	}, nil)
	if err != nil || folder != "" {
		t.Fatalf("healthy discovery: folder=%q err=%v", folder, err)
	}
}

func TestRecoverySelectsHighestVerifiedNumericVersion(t *testing.T) {
	// Lexical ordering chooses 99 over 155; trusting the first directory chooses
	// an incomplete newer update over the intact runtime already on disk.
	root := t.TempDir()
	for _, name := range []string{"99.0.1.1", "155.0.4283.62", "156.0.1.0", "93.0.992.31", "155.0.4283", "155.0.4283.62.old", "155.0.+2.1", "SetupMetrics"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	folder, err := recoveryBrowserFolder(missingRuntime, rootsAt(root), func(_, candidate string) error {
		if filepath.Base(candidate) == "156.0.1.0" {
			return errors.New("incomplete update")
		}
		return nil
	})
	if err != nil || folder != filepath.Join(root, "155.0.4283.62") {
		t.Fatalf("recovery: folder=%q err=%v", folder, err)
	}
}

func TestRecoveryMinimumVersionBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted bool
	}{
		{"94.0.992.30", false}, {"94.0.992.31", true}, {"94.0.992.32", true},
		{"94.0.992", false}, {"94.0.992.31.0", false}, {"94.0.992.-1", false},
		{"94.0.992.65536", false}, {"94.0.992. 31", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			candidate := filepath.Join(root, tc.name)
			if err := os.Mkdir(candidate, 0700); err != nil {
				t.Fatal(err)
			}
			folder, err := recoveryBrowserFolder(missingRuntime, rootsAt(root), func(_, _ string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if (folder == candidate) != tc.accepted {
				t.Fatalf("folder=%q, accepted=%v", folder, tc.accepted)
			}
		})
	}
}

func TestRecoveryDoesNotMaskNormalDiscoveryErrors(t *testing.T) {
	denied := errors.New("access denied querying installed runtime")
	_, err := recoveryBrowserFolder(func() (string, error) { return "", denied }, func() ([]string, error) {
		t.Fatal("recovery must not mask an unrelated discovery failure")
		return nil, nil
	}, nil)
	if !errors.Is(err, denied) {
		t.Fatalf("got %v, want original discovery error", err)
	}
}

func TestRecoveryAcceptsNotFoundDiscoveryError(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "155.0.4283.62")
	if err := os.Mkdir(want, 0700); err != nil {
		t.Fatal(err)
	}
	folder, err := recoveryBrowserFolder(func() (string, error) { return "", os.ErrNotExist }, rootsAt(root), func(_, _ string) error { return nil })
	if err != nil || folder != want {
		t.Fatalf("folder=%q err=%v", folder, err)
	}
}

func TestRecoveryWithNoInstalledCandidateKeepsOrdinaryBootstrap(t *testing.T) {
	folder, err := recoveryBrowserFolder(missingRuntime, rootsAt(filepath.Join(t.TempDir(), "absent")), nil)
	if err != nil || folder != "" {
		t.Fatalf("folder=%q err=%v", folder, err)
	}
}

func TestRecoveryRejectsEveryUnverifiedCandidate(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "155.0.4283.62"), 0700); err != nil {
		t.Fatal(err)
	}
	folder, err := recoveryBrowserFolder(missingRuntime, rootsAt(root), func(_, _ string) error { return errors.New("untrusted publisher") })
	if folder != "" || err == nil || !strings.Contains(err.Error(), "untrusted publisher") {
		t.Fatalf("folder=%q err=%v", folder, err)
	}
}

func TestRuntimePathsCannotEscapeRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "elsewhere", "msedgewebview2.exe")
	if err := containedWithoutReparse(root, outside); err == nil {
		t.Fatal("accepted file outside the machine runtime root")
	}
}

func TestRuntimePathsRejectReparseDirectories(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "155.0.4283.62")
	if err := os.Symlink(outside, link); err != nil {
		// Standard Windows compatibility junctions let the sandboxed test prove
		// this guard without obtaining the privilege to create a symlink.
		volume := filepath.VolumeName(root) + string(filepath.Separator)
		for _, name := range []string{`Users\All Users`, `Documents and Settings`} {
			existing := filepath.Join(volume, name)
			pointer, conversionErr := windows.UTF16PtrFromString(existing)
			if conversionErr != nil {
				continue
			}
			attributes, attributeErr := windows.GetFileAttributes(pointer)
			if attributeErr != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
				continue
			}
			if err := containedWithoutReparse(volume, existing); err == nil {
				t.Fatal("accepted an existing Windows reparse directory")
			}
			t.Logf("read-only reparse fixture: %s", existing)
			return
		}
		t.Skipf("no readable reparse fixture and Windows symlink unavailable: %v", err)
	}
	if err := containedWithoutReparse(root, link); err == nil {
		t.Fatal("accepted a reparse directory")
	}
}

func missingRuntime() (string, error) { return "", nil }
func rootsAt(root string) func() ([]string, error) {
	return func() ([]string, error) { return []string{root}, nil }
}
