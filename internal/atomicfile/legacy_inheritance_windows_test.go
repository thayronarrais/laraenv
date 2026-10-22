//go:build windows

package atomicfile

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// These are the DACLs captured from the fresh graphical Sandbox guest on
// 2026-10-19. They use inherited ACEs without SE_DACL_AUTO_INHERITED (D:AI).
const guestLegacyParentDACL = "D:(A;ID;FA;;;BA)(A;OICIIOID;GA;;;BA)(A;ID;FA;;;SY)(A;OICIIOID;GA;;;SY)(A;ID;0x1301bf;;;AU)(A;OICIIOID;SDGXGWGR;;;AU)(A;ID;0x1200a9;;;BU)(A;OICIIOID;GXGR;;;BU)"
const guestLegacyFileDACL = "D:(A;ID;FA;;;BA)(A;ID;FA;;;SY)(A;ID;0x1301bf;;;AU)(A;ID;0x1200a9;;;BU)"

func setLegacyFixtureDACL(t *testing.T, path, sddl string) {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(name, windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	// Test setup only: the legacy setter deliberately creates the old ACL
	// inheritance model reproduced by the guest. Production uses SetSecurityInfo.
	if err := windows.SetKernelObjectSecurity(handle, windows.DACL_SECURITY_INFORMATION, descriptor); err != nil {
		t.Fatal(err)
	}
}

func TestWritePreservingPermissionsWithGuestLegacyInheritedDACL(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	setLegacyFixtureDACL(t, directory, guestLegacyParentDACL)
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	setLegacyFixtureDACL(t, path, guestLegacyFileDACL)
	if got := testDACL(t, path); got != guestLegacyFileDACL {
		t.Fatalf("fixture does not reproduce guest DACL:\nwant %s\ngot  %s", guestLegacyFileDACL, got)
	}
	// Capture the real stage boundary as evidence: only the AI control marker
	// may change; inherited ACEs and their order must be byte-for-byte identical.
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := openDirectory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	original, err := openTarget(dir, "config.json")
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := readPermissions(original)
	original.Close()
	if err != nil {
		t.Fatal(err)
	}
	stage, err := newRootStage(dir, root, ".guest-inheritance-probe", permissions)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.preserve(permissions); err != nil {
		t.Fatal(err)
	}
	staged, err := readPermissions(stage.retained)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("original DACL: %s; staged DACL: %s", permissions.representation, staged.representation)
	stage.Close()
	stage.cleanup()
	stage.release()
	if err := WritePreservingPermissions(path, []byte("after"), 0600); err != nil {
		t.Fatalf("cannot save config with guest inherited DACL: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "after" {
		t.Fatalf("config was not saved: %q, %v", data, err)
	}
	// SetSecurityInfo converts the legacy model to automatic inheritance. Every
	// ACE (including order and inherited flags) and protection must remain equal.
	if got, want := testDACL(t, path), "D:AI"+guestLegacyFileDACL[2:]; got != want {
		t.Fatalf("config permissions changed beyond automatic-inheritance metadata:\nwant %s\ngot  %s", want, got)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("staging files remain: %v, %v", entries, err)
	}
}

func TestRootStageIsRestrictedAtCreation(t *testing.T) {
	root, directory := rootFixture(t)
	sid := userSID(t)
	setTestDACL(t, directory, "D:P(A;OICI;FA;;;"+sid+")(A;OICI;FR;;;WD)")
	setTestDACL(t, filepath.Join(directory, "note.md"), "D:P(A;;FA;;;"+sid+")")
	expected, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	// Canonicalize SID aliases only; protection and every ACE remain exact.
	want := expected.String()
	dir, err := openDirectory(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	stage, err := newRootStage(dir, root, ".creation-fixture", fixtureStagePermissions(t, root))
	if err != nil {
		t.Fatal(err)
	}
	defer stage.release()
	defer stage.cleanup()
	defer stage.Close()
	actual, err := readPermissions(stage.retained)
	if err != nil {
		t.Fatal(err)
	}
	if actual.representation != want {
		t.Fatalf("stage inherited broader access before restriction:\nwant %s\ngot %s", want, actual.representation)
	}
}

func TestWritePreservingPermissionsWithNormallyInheritedGuestDACL(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	setLegacyFixtureDACL(t, directory, guestLegacyParentDACL)
	// Do not apply a setter to the source: that would strip incidental O:/G:
	// fields from a DACL-only query and hide the real guest creation behavior.
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := readPermissions(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if permissions.representation != guestLegacyFileDACL {
		t.Fatalf("DACL-only snapshot contains unrelated metadata:\nwant %s\ngot %s", guestLegacyFileDACL, permissions.representation)
	}
	if err := WritePreservingPermissions(path, []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	after, err := readPermissions(file)
	if err != nil || after.representation != "D:AI"+guestLegacyFileDACL[2:] {
		t.Fatalf("DACL changed beyond AI: %s, %v", after.representation, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "after" {
		t.Fatalf("config not saved: %q, %v", data, err)
	}
}
