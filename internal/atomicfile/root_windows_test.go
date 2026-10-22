//go:build windows

package atomicfile

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func userSID(t *testing.T) string {
	t.Helper()
	token := windows.GetCurrentProcessToken()
	u, err := token.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return u.User.Sid.String()
}
func setTestDACL(t *testing.T, name, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, flags, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}
func testDACL(t *testing.T, name string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(name, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}
func TestReplaceInRootPreservesDACL(t *testing.T) {
	for _, protected := range []string{"P", ""} {
		t.Run("protected-"+protected, func(t *testing.T) {
			root, dir := rootFixture(t)
			file := filepath.Join(dir, "note.md")
			setTestDACL(t, file, "D:"+protected+"(A;;FA;;;"+userSID(t)+")(A;;FR;;;WD)")
			before := testDACL(t, file)
			if err := ReplaceInRoot(root, "note.md", []byte("saved"), nil); err != nil {
				t.Fatal(err)
			}
			assertRootContents(t, dir, "saved")
			if after := testDACL(t, file); after != before {
				t.Fatalf("DACL changed:\n%s\n%s", before, after)
			}
		})
	}
}
func TestReplaceInRootPermissionFailures(t *testing.T) {
	for _, which := range []string{"target-write", "parent-create", "dacl-change", "stage-cleanup"} {
		t.Run(which, func(t *testing.T) {
			root, dir := rootFixture(t)
			file := filepath.Join(dir, "note.md")
			sid := userSID(t)
			restore := "D:P(A;;FA;;;" + sid + ")"
			t.Cleanup(func() { setTestDACL(t, dir, restore); setTestDACL(t, file, restore) })
			setTestDACL(t, file, restore)
			var callback func(*os.Root, string) error
			switch which {
			case "target-write":
				setTestDACL(t, file, "D:P(D;;0x6;;;"+sid+")(A;;FA;;;"+sid+")")
			case "parent-create":
				setTestDACL(t, dir, "D:P(D;;0x2;;;"+sid+")(A;;FA;;;"+sid+")")
			case "dacl-change":
				callback = func(_ *os.Root, _ string) error { setTestDACL(t, file, "D:P(A;;GRGW;;;"+sid+")"); return nil }
			case "stage-cleanup":
				callback = func(_ *os.Root, _ string) error {
					entries, err := os.ReadDir(dir)
					if err != nil {
						return err
					}
					for _, entry := range entries {
						if entry.Name() != "note.md" {
							setTestDACL(t, filepath.Join(dir, entry.Name()), "D:P(D;;SD;;;"+sid+")(A;;FA;;;"+sid+")")
						}
					}
					return errors.New("stop after restrictive staging ACL")
				}
			}
			if err := ReplaceInRoot(root, "note.md", []byte("new"), callback); err == nil {
				t.Fatal("accepted permission failure")
			}
			assertRootContents(t, dir, "original")
		})
	}
}

func TestReplaceInRootParentSwap(t *testing.T) {
	base := t.TempDir()
	checkout := filepath.Join(base, "checkout")
	parentPath := filepath.Join(checkout, "docs")
	outside := filepath.Join(base, "outside")
	moved := filepath.Join(checkout, "moved")
	for _, dir := range []string{parentPath, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("original"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(checkout)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Swap after the production pin boundary, before child handles are open:
	// NTFS rejects moving a directory once its children have open handles.
	native, err := openDirectory(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	parent, err := root.OpenRoot("docs")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := sameDirectory(parent, native); err != nil {
		t.Fatal(err)
	}
	if err := root.Rename("docs", "moved"); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", parentPath, outside).CombinedOutput(); err != nil {
		t.Fatalf("junction: %s %v", output, err)
	}
	defer os.Remove(parentPath)
	called := false
	err = replaceInParent(root, "docs", parent, native, "note.md", []byte("new"), func(pinned *os.Root, leaf string) error {
		called = true
		data, err := pinned.ReadFile(leaf)
		if err != nil || string(data) != "original" {
			t.Fatalf("pinned target: %q %v", data, err)
		}
		return nil
	})
	if err == nil || !called {
		t.Fatalf("save did not stage and reject changed parent: %v callback=%v", err, called)
	}
	assertRootContents(t, moved, "original")
	assertRootContents(t, outside, "original")
}

func TestReplaceInRootRenameFailure(t *testing.T) {
	root, dir := rootFixture(t)
	path, err := windows.UTF16PtrFromString(filepath.Join(dir, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(lock)
	called := false
	err = ReplaceInRoot(root, "note.md", []byte("new"), func(_ *os.Root, _ string) error { called = true; return nil })
	if err == nil || !called {
		t.Fatalf("did not reach real rename failure: %v callback=%v", err, called)
	}
	assertRootContents(t, dir, "original")
}

func TestReplaceInRootPreservesNullDACL(t *testing.T) {
	root, dir := rootFixture(t)
	file := filepath.Join(dir, "note.md")
	setTestDACL(t, file, "D:NO_ACCESS_CONTROL")
	before := testDACL(t, file)
	if err := ReplaceInRoot(root, "note.md", []byte("saved"), nil); err != nil {
		t.Fatal(err)
	}
	if after := testDACL(t, file); after != before {
		t.Fatalf("null DACL changed: %s -> %s", before, after)
	}
	assertRootContents(t, dir, "saved")
}

func TestRootStageDuplicationFailureUsesGrantedDelete(t *testing.T) {
	root, dir := rootFixture(t)
	native, err := openDirectory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer native.Close()
	stage, err := newRootStage(native, root, ".test-stage", fixtureStagePermissions(t, root))
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.retained.Close(); err != nil {
		t.Fatal(err)
	}
	sid := userSID(t)
	path := filepath.Join(dir, stage.name)
	t.Cleanup(func() { setTestDACL(t, dir, "D:P(A;;FA;;;"+sid+")") })
	t.Cleanup(func() {
		if _, err := os.Stat(path); err == nil {
			setTestDACL(t, path, "D:P(A;;FA;;;"+sid+")")
		}
	})
	duplicateErr := errors.New("duplicate failed")
	got, err := retainRootStage(stage.writer, root, stage.name, func(_ *os.File) (*os.File, error) {
		setTestDACL(t, filepath.Join(dir, "note.md"), "D:P(A;;FA;;;"+sid+")")
		setTestDACL(t, path, "D:P(D;;SD;;;"+sid+")(A;;FA;;;"+sid+")")
		setTestDACL(t, dir, "D:P(D;;0x40;;;"+sid+")(A;;FA;;;"+sid+")")
		if err := root.Remove(stage.name); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("fresh deletion was not denied: %v", err)
		}
		return nil, duplicateErr
	})
	if got != nil || !errors.Is(err, duplicateErr) {
		t.Fatalf("stage=%v err=%v", got, err)
	}
	assertRootContents(t, dir, "original")
}

func TestReplaceInRootRejectsInternalJunction(t *testing.T) {
	root, dir := rootFixture(t)
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "note.md"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(dir, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, real).CombinedOutput(); err != nil {
		t.Fatalf("junction: %s %v", out, err)
	}
	defer os.Remove(junction)
	if err := ReplaceInRoot(root, "junction/note.md", []byte("new"), nil); err == nil {
		t.Fatal("edited through internal junction")
	}
	assertRootContents(t, real, "original")
}
