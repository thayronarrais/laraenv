//go:build windows

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestConfigSavePreservesProtectedDACLUnderPermissiveParent(t *testing.T) {
	dir := t.TempDir()
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := tokenUser.User.Sid.String()
	// Everyone can read inherited files in this directory. The config opts
	// out of inheritance and grants access only to the current Windows user.
	setConfigFixtureDACL(t, dir, "D:P(A;OICI;FA;;;"+sid+")(A;OICI;FR;;;WD)")
	store, err := NewStore(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	setConfigFixtureDACL(t, store.path, "D:P(A;;FA;;;"+sid+")")
	before := configFixtureDACL(t, store.path)
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatal(err)
	}
	beforeWorld := effectiveConfigRights(t, store.path, world)
	beforeUser := effectiveConfigRights(t, store.path, tokenUser.User.Sid)
	if beforeWorld != 0 {
		t.Fatalf("fixture is readable by Everyone: 0x%x", beforeWorld)
	}
	if err := store.SetLanguage("pt-BR"); err != nil {
		t.Fatal(err)
	}
	if after := configFixtureDACL(t, store.path); after != before {
		t.Fatalf("config save weakened/replaced DACL:\nbefore %s\nafter  %s", before, after)
	}
	if got := effectiveConfigRights(t, store.path, world); got != beforeWorld {
		t.Fatalf("Everyone permissions changed: 0x%x -> 0x%x", beforeWorld, got)
	}
	if got := effectiveConfigRights(t, store.path, tokenUser.User.Sid); got != beforeUser {
		t.Fatalf("owner permissions changed: 0x%x -> 0x%x", beforeUser, got)
	}
	reloaded, err := NewStore(store.path)
	if err != nil || reloaded.Get().Language != "pt-BR" {
		t.Fatalf("save did not persist content: %v", err)
	}
}

func TestConfigSavePermissionFailurePreservesOriginalAndRemovesStage(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	restore := "D:P(A;OICI;FA;;;" + user.User.Sid.String() + ")"
	t.Cleanup(func() { setConfigFixtureDACL(t, dir, restore) })
	setConfigFixtureDACL(t, dir, "D:P(D;;0x2;;;"+user.User.Sid.String()+")(A;OICI;FA;;;"+user.User.Sid.String()+")")
	if err := store.SetLanguage("pt-BR"); err == nil {
		t.Fatal("save bypassed denied staging creation")
	}
	after, err := os.ReadFile(store.path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed save changed the original config")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed save left a staging file: %v (%d entries)", err, len(entries))
	}
}

func setConfigFixtureDACL(t *testing.T, path, sddl string) {
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
	runtime.KeepAlive(sd)
}

func configFixtureDACL(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func effectiveConfigRights(t *testing.T, path string, sid *windows.SID) windows.ACCESS_MASK {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	trustee := windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_UNKNOWN, TrusteeValue: windows.TrusteeValueFromSID(sid)}
	var rights windows.ACCESS_MASK
	status, _, _ := windows.NewLazySystemDLL("advapi32.dll").NewProc("GetEffectiveRightsFromAclW").Call(uintptr(unsafe.Pointer(acl)), uintptr(unsafe.Pointer(&trustee)), uintptr(unsafe.Pointer(&rights)))
	runtime.KeepAlive(sd)
	runtime.KeepAlive(sid)
	if status != 0 {
		t.Fatalf("GetEffectiveRightsFromAclW: %v", windows.Errno(status))
	}
	return rights
}
