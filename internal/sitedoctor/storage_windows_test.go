package sitedoctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func createStorageJunction(t *testing.T, root, link, target string) {
	t.Helper()
	// The command may only create links within this test's temporary directory.
	for _, path := range []string{link, target} {
		absolute, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		relative, err := filepath.Rel(root, absolute)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			t.Fatalf("junction fixture is outside its temporary directory: %q", path)
		}
	}
	output, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create temporary storage junction: %v\n%s", err, output)
	}
}

func TestStorageWindowsJunctionsAreRecognizedAndProtected(t *testing.T) {
	// Exercise the Go 1.23+ behavior that classifies junctions as ModeIrregular.
	t.Setenv("GODEBUG", os.Getenv("GODEBUG")+",winsymlink=1")
	for _, test := range []struct {
		name, targetKind, status, code string
	}{
		{"correct", "expected", "pass", "storage_link_ok"},
		{"wrong_target", "other", "optional", "storage_link_conflict"},
		{"broken", "missing", "optional", "storage_link_conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, link, expected := storageFixture(t)
			marker := filepath.Join(expected, "expected.txt")
			if err := os.WriteFile(marker, []byte("expected target data"), 0600); err != nil {
				t.Fatal(err)
			}
			target := expected
			if test.targetKind == "other" {
				target = filepath.Join(root, "other")
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, "other.txt"), []byte("other user data"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if test.targetKind == "missing" {
				target = filepath.Join(root, "missing")
			}
			createStorageJunction(t, root, link, target)
			assertStorageFinding(t, root, test.status, test.code, "")
			if got, err := os.Readlink(link); err != nil || !strings.EqualFold(got, target) {
				t.Fatalf("existing storage junction changed: target=%q err=%v", got, err)
			}
			assertStorageData(t, marker, "expected target data")
			if test.targetKind == "other" {
				assertStorageData(t, filepath.Join(target, "other.txt"), "other user data")
			} else if test.targetKind == "missing" {
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatalf("diagnosis created the missing junction target: %v", err)
				}
			}
		})
	}
}

func TestStorageWindowsDirectoryIsProtectedEvenWhenExpectedIsItsJunction(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "public", "storage")
	expected := filepath.Join(root, "storage", "app", "public")
	for _, path := range []string{link, filepath.Dir(expected)} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(link, "user-file.txt")
	if err := os.WriteFile(marker, []byte("existing user data"), 0600); err != nil {
		t.Fatal(err)
	}
	createStorageJunction(t, root, expected, link)
	actualInfo, err := os.Stat(link)
	if err != nil {
		t.Fatal(err)
	}
	expectedInfo, err := os.Stat(expected)
	if err != nil || !os.SameFile(actualInfo, expectedInfo) {
		t.Fatalf("fixture does not refer to the same directory: %v", err)
	}
	assertStorageFinding(t, root, "optional", "storage_link_conflict", "")
	assertStorageData(t, marker, "existing user data")
}
