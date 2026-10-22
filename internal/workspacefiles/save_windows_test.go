//go:build windows

package workspacefiles

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func saveDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}
func TestSavePermissionDenialAfterStaging(t *testing.T) {
	svc, ref, dir := fixtureService(t)
	key := mustKey(t, svc, ref)
	file := filepath.Join(dir, "note.md")
	if err := os.WriteFile(file, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	read := svc.Read(ref, key, "note.md")
	wantCode(t, read, "ok")
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()
	restore := "D:P(A;;FA;;;" + sid + ")"
	t.Cleanup(func() { saveDACL(t, dir, restore); saveDACL(t, file, restore) })
	// The third authority check runs from the pinned-parent callback, after
	// staging. Simulate another actor revoking writes without changing bytes.
	resolve := svc.resolve
	checks := 0
	svc.resolve = func(ref ContextRef) (string, string, error) {
		checks++
		if checks == 3 {
			saveDACL(t, file, "D:P(D;;0x6;;;"+sid+")(A;;FA;;;"+sid+")")
		}
		return resolve(ref)
	}
	result := svc.Save(ref, key, "note.md", "new", read.Document.Revision)
	if checks != 3 {
		t.Fatalf("save did not reach staged revalidation: %d checks", checks)
	}
	wantCode(t, result, "permission_denied")
	data, err := os.ReadFile(file)
	if err != nil || string(data) != "original" {
		t.Fatalf("file=%q err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("leftover temps: %v", entries)
	}
}
