//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"lara4ever/internal/paths"
	"lara4ever/internal/projectroutes"
)

func sslTestUserSID(t *testing.T) string {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return user.User.Sid.String()
}

func sslTestNoStages(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".laraenv-ssl-") {
			t.Fatalf("SSL rollback left a staging file: %s", entry.Name())
		}
	}
}

func sslTestSetDACL(t *testing.T, path, sddl string) {
	t.Helper()
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatal(err)
	}
	flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func sslTestDACL(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return descriptor.String()
}

// The rollback must restore permissions captured before an SSL operation, even
// if that operation removed the private key or replaced it with a broader ACL.
func TestSSLFileRollbackRestoresOriginalDACLWindows(t *testing.T) {
	for _, state := range []string{"modified", "removed", "recreated"} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "fixture.test.key")
			original := []byte("fixture-private-key-before-ssl-change")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			sid := sslTestUserSID(t)
			sslTestSetDACL(t, path, "D:P(A;;FA;;;"+sid+")")
			wantDACL := sslTestDACL(t, path)
			snapshot, err := snapshotSSLFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if state != "modified" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if state != "removed" {
				if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
				sslTestSetDACL(t, path, "D:P(A;;FA;;;"+sid+")(A;;FR;;;WD)")
			}
			if err := restoreSSLFile(snapshot); err != nil {
				t.Fatal(err)
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != string(original) {
				t.Fatalf("private key bytes were not restored: %q, %v", actual, err)
			}
			if actual := sslTestDACL(t, path); actual != wantDACL {
				t.Fatalf("rollback broadened the original explicit DACL:\nwant %s\ngot  %s", wantDACL, actual)
			}
		})
	}
}

func TestSSLFileRollbackDetectsPermissionOnlyChangesWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.test.key")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	sid := sslTestUserSID(t)
	sslTestSetDACL(t, path, "D:P(A;;FA;;;"+sid+")")
	before, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sslTestSetDACL(t, path, "D:P(A;;FA;;;"+sid+")(A;;FR;;;WD)")
	broader, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if equalSSLFileSnapshots(before, broader) {
		t.Fatal("SSL rollback skipped a permission-only change")
	}
	if err := restoreSSLFile(before); err != nil {
		t.Fatal(err)
	}
	after, err := snapshotSSLFile(path)
	if err != nil || !equalSSLFileSnapshots(before, after) {
		t.Fatalf("SSL permissions were not restored: %v", err)
	}
}

func TestSSLFileRollbackRejectsNewInheritedAccessWindows(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.test.key")
	sid := sslTestUserSID(t)
	sslTestSetDACL(t, directory, "D:P(A;OICI;FA;;;"+sid+")")
	if err := os.WriteFile(path, []byte("fixture-private-key"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("current-public-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	// This represents an independently broadened parent since the snapshot.
	// Re-enabling inheritance on a staged file would add Everyone read access.
	sslTestSetDACL(t, directory, "D:P(A;OICI;FA;;;"+sid+")(A;OICI;FR;;;WD)")
	if err := restoreSSLFile(before); err == nil {
		t.Fatal("restored private key with new inherited access")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "current-public-fixture" {
		t.Fatalf("failed rollback replaced the target: %q, %v", actual, err)
	}
	sslTestNoStages(t, directory)
}

func TestSSLFileRollbackRestoresUnprotectedDACLWindows(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.test.key")
	sslTestSetDACL(t, directory, "D:P(A;OICI;FA;;;"+sslTestUserSID(t)+")")
	if err := os.WriteFile(path, []byte("fixture-private-key"), 0600); err != nil {
		t.Fatal(err)
	}
	wantDACL := sslTestDACL(t, path)
	before, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := restoreSSLFile(before); err != nil {
		t.Fatal(err)
	}
	if actual := sslTestDACL(t, path); actual != wantDACL {
		t.Fatalf("unprotected DACL changed:\nwant %s\ngot %s", wantDACL, actual)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "fixture-private-key" {
		t.Fatalf("private key was not restored: %q, %v", actual, err)
	}
	sslTestNoStages(t, directory)
}

func TestSSLFileRollbackRejectsInvalidCapturedDACLWindows(t *testing.T) {
	for _, dacl := range []string{"", "invalid", "O:SY"} {
		t.Run(dacl, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "fixture.test.key")
			if err := os.WriteFile(path, []byte("current-public-fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			snapshot, err := snapshotSSLFile(path)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.data = []byte("fixture-private-key-not-to-be-written")
			snapshot.permissions.dacl = dacl
			if err := restoreSSLFile(snapshot); err == nil {
				t.Fatal("SSL rollback accepted missing or invalid DACL")
			}
			actual, err := os.ReadFile(path)
			if err != nil || string(actual) != "current-public-fixture" {
				t.Fatalf("failed rollback replaced target bytes: %q, %v", actual, err)
			}
			sslTestNoStages(t, directory)
		})
	}
}

func TestSSLFileRollbackRemovesOnlyNewFileWindows(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.test.key")
	untouched := filepath.Join(directory, "ca.key")
	if err := os.WriteFile(untouched, []byte("fixture-ca-identity"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new-leaf-key"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreSSLFile(before); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("new leaf key survived rollback: %v", err)
	}
	actual, err := os.ReadFile(untouched)
	if err != nil || string(actual) != "fixture-ca-identity" {
		t.Fatalf("unrelated CA changed: %q, %v", actual, err)
	}
}

func TestSSLFileAppRollbackRestoresPrivateKeyDACLWindows(t *testing.T) {
	if mcpPauseFixtureProcess(t) {
		return
	}
	fixture := newSSLFixture(t, true, false)
	key := filepath.Join(paths.NginxCerts(), "demo.test.key")
	sid := sslTestUserSID(t)
	sslTestSetDACL(t, key, "D:P(A;;FA;;;"+sid+")")
	wantDACL := sslTestDACL(t, key)
	store, err := projectroutes.NewStore(filepath.Join(paths.Root(), "fixture-acl-routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	resolverFailure := errors.New("fixture resolver failed after key permission change")
	fixture.app.routes = projectroutes.NewManager(store, func(context.Context, string) (projectroutes.Project, error) {
		// The real SSL transaction has captured its snapshot. Change only the
		// DACL at a real failure boundary, leaving bytes and os.FileMode equal.
		sslTestSetDACL(t, key, "D:P(A;;FA;;;"+sid+")(A;;FR;;;WD)")
		return projectroutes.Project{}, resolverFailure
	}, nil, nil)
	if err := fixture.app.SetProjectSSL("demo", false); !errors.Is(err, resolverFailure) {
		t.Fatalf("want resolver failure, got %v", err)
	}
	fixture.assertPreserved(t, true)
	if actual := sslTestDACL(t, key); actual != wantDACL {
		t.Fatalf("SSL app rollback skipped private-key DACL restoration:\nwant %s\ngot %s", wantDACL, actual)
	}
}

func TestSSLFileStageIsRestrictedAtCreationWindows(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.test.key")
	sid := sslTestUserSID(t)
	sslTestSetDACL(t, directory, "D:P(A;OICI;FA;;;"+sid+")(A;OICI;FR;;;WD)")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	sslTestSetDACL(t, path, "D:P(A;;FA;;;"+sid+")")
	expected, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	// Windows may serialize a numeric SID using an equivalent alias, such as
	// LA for the local Administrator. Keep the complete canonical DACL exact.
	wantDACL := expected.String()
	snapshot, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	parent, _, err := sslOpenParent(path)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	stage, err := newSSLFileStage(parent, ".laraenv-ssl-fixture.tmp", snapshot.permissions)
	if err != nil {
		t.Fatal(err)
	}
	defer stage.Close()
	defer sslDeleteByHandle(stage)
	actual, err := readSSLFilePermissions(stage)
	if err != nil {
		t.Fatal(err)
	}
	// A reader opening even an empty broad stage could retain its access after
	// the DACL is restricted. The creation itself must exclude new principals.
	if actual.dacl != wantDACL {
		t.Fatalf("stage was briefly readable through inherited access:\nwant %s\ngot %s", wantDACL, actual.dacl)
	}
}

func TestSSLFileRollbackWithGuestLegacyInheritedDACLWindows(t *testing.T) {
	const parentDACL = "D:(A;ID;FA;;;BA)(A;OICIIOID;GA;;;BA)(A;ID;FA;;;SY)(A;OICIIOID;GA;;;SY)(A;ID;0x1301bf;;;AU)(A;OICIIOID;SDGXGWGR;;;AU)(A;ID;0x1200a9;;;BU)(A;OICIIOID;GXGR;;;BU)"
	const fileDACL = "D:(A;ID;FA;;;BA)(A;ID;FA;;;SY)(A;ID;0x1301bf;;;AU)(A;ID;0x1200a9;;;BU)"
	legacyDACL := func(path, sddl string) {
		t.Helper()
		descriptor, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			t.Fatal(err)
		}
		name, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		handle, err := windows.CreateFile(name, windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(handle)
		// Test setup only: reproduce the legacy inheritance model from the guest.
		if err := windows.SetKernelObjectSecurity(handle, windows.DACL_SECURITY_INFORMATION, descriptor); err != nil {
			t.Fatal(err)
		}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.test.key")
	legacyDACL(directory, parentDACL)
	if err := os.WriteFile(path, []byte("fixture-private-key"), 0600); err != nil {
		t.Fatal(err)
	}
	// Keep the source normally inherited: applying an explicit ACL setter here
	// would hide incidental owner/group fields returned by a DACL-only query.
	before, err := snapshotSSLFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.permissions.dacl != fileDACL {
		t.Fatalf("fixture differs from guest DACL: %s", before.permissions.dacl)
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreSSLFile(before); err != nil {
		t.Fatalf("cannot restore SSL file with legacy inherited DACL: %v", err)
	}
	if got, want := sslTestDACL(t, path), "D:AI"+fileDACL[2:]; got != want {
		t.Fatalf("SSL DACL changed beyond automatic-inheritance metadata:\nwant %s\ngot %s", want, got)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "fixture-private-key" {
		t.Fatalf("SSL bytes were not restored: %q, %v", data, err)
	}
	sslTestNoStages(t, directory)
}
