package sitedoctor

import (
	"os"
	"path/filepath"
	"testing"
)

func storageFixture(t *testing.T) (root, link, expected string) {
	t.Helper()
	root = t.TempDir()
	link = filepath.Join(root, "public", "storage")
	expected = filepath.Join(root, "storage", "app", "public")
	for _, path := range []string{filepath.Dir(link), expected} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root, link, expected
}

func assertStorageFinding(t *testing.T, root, status, code, fixID string) {
	t.Helper()
	got := storageFinding(root, true)
	if got.ID != "storage_link" || got.Status != status || got.Severity != "info" || got.Code != code || got.FixID != fixID || got.RequiresConfirmation {
		t.Fatalf("unexpected storage finding: %#v", got)
	}
}

func assertStorageData(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("storage data changed: path=%q data=%q err=%v", path, got, err)
	}
}

func TestStorageMissingIsOptionalAndOnlyOffersCreationWhenReady(t *testing.T) {
	root, _, _ := storageFixture(t)
	assertStorageFinding(t, root, "optional", "storage_link_missing", "storage_link")
	got := storageFinding(root, false)
	if got.Status != "optional" || got.Severity != "info" || got.FixID != "" {
		t.Fatalf("missing prerequisites offered a storage action: %#v", got)
	}
}

func TestStorageOrdinaryDirectoryIsOptionalAndPreserved(t *testing.T) {
	root, link, _ := storageFixture(t)
	if err := os.Mkdir(link, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(link, "user-file.txt")
	if err := os.WriteFile(marker, []byte("existing user data"), 0600); err != nil {
		t.Fatal(err)
	}
	assertStorageFinding(t, root, "optional", "storage_link_conflict", "")
	assertStorageData(t, marker, "existing user data")
}

func TestStorageSymlinkUsesDirectoryIdentityAndPreservesTargets(t *testing.T) {
	for _, test := range []struct {
		name, targetKind, status, code string
	}{
		{"correct", "expected", "pass", "storage_link_ok"},
		{"wrong_target", "other", "optional", "storage_link_conflict"},
		{"broken", "missing", "optional", "storage_link_conflict"},
		{"file_target", "file", "optional", "storage_link_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, link, expected := storageFixture(t)
			marker := filepath.Join(expected, "expected.txt")
			if err := os.WriteFile(marker, []byte("expected target data"), 0600); err != nil {
				t.Fatal(err)
			}
			target := expected
			switch test.targetKind {
			case "other":
				target = filepath.Join(root, "other")
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "missing":
				target = filepath.Join(root, "missing")
			case "file":
				target = marker
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			assertStorageFinding(t, root, test.status, test.code, "")
			if got, err := os.Readlink(link); err != nil || got != target {
				t.Fatalf("existing storage link changed: target=%q err=%v", got, err)
			}
			assertStorageData(t, marker, "expected target data")
		})
	}
}
