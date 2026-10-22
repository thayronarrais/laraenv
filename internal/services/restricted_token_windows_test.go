//go:build windows

package services

import (
	"os/exec"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The postgres binary refuses to run under a token holding the Administrators
// group (the app is elevated via requireAdministrator). applyRestrictedToken
// must hand the child a primary token where that group is disabled — the same
// trick pg_ctl uses.
func TestApplyRestrictedToken_DisablesAdministratorsGroup(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	cleanup, err := applyRestrictedToken(cmd)
	if err != nil {
		t.Fatalf("applyRestrictedToken: %v", err)
	}
	defer cleanup()

	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Token == 0 {
		t.Fatal("no token attached to SysProcAttr")
	}

	// Inspect the attached token: Administrators (S-1-5-32-544) must be
	// absent or deny-only.
	tok := windows.Token(cmd.SysProcAttr.Token)
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := tok.GetTokenGroups()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups.AllGroups() {
		if windows.EqualSid(g.Sid, admins) {
			if g.Attributes&windows.SE_GROUP_ENABLED != 0 {
				t.Fatal("Administrators group still enabled in restricted token")
			}
			if g.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY == 0 {
				t.Fatal("Administrators group present but not deny-only")
			}
		}
	}
}

// The restricted token must still be a valid primary token: spawning a real
// child with it has to work.
func TestApplyRestrictedToken_ChildProcessRuns(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit", "0")
	cleanup, err := applyRestrictedToken(cmd)
	if err != nil {
		t.Fatalf("applyRestrictedToken: %v", err)
	}
	defer cleanup()
	hideWindow(cmd)
	if err := cmd.Run(); err != nil {
		t.Fatalf("child failed to run under restricted token: %v", err)
	}
}

// aclSids lists the SIDs of all ACCESS_ALLOWED aces in an ACL.
func aclSids(t *testing.T, acl *windows.ACL) []*windows.SID {
	t.Helper()
	var sids []*windows.SID
	count := uint32((*(*struct {
		AclRevision byte
		Sbz1        byte
		AclSize     uint16
		AceCount    uint16
		Sbz2        uint16
	})(unsafe.Pointer(acl))).AceCount)
	for i := uint32(0); i < count; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatalf("GetAce(%d): %v", i, err)
		}
		sids = append(sids, (*windows.SID)(unsafe.Pointer(&ace.SidStart)))
	}
	return sids
}

// On an ELEVATED token the default DACL grants access only to Administrators
// and SYSTEM. Once the Administrators SID is disabled in the restricted token,
// the child can no longer access its own objects and dies during DLL init
// with STATUS_DLL_INIT_FAILED (0xc0000142). pg_ctl fixes this by adding the
// token user to the default DACL (AddUserToTokenDacl); addUserToTokenDacl must
// do the same. The test fabricates the elevated-style DACL (SYSTEM only) on a
// duplicated token, since the test runner itself may not be elevated.
func TestAddUserToTokenDacl_GrantsTokenUserAndPreservesExisting(t *testing.T) {
	var procTok windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE, &procTok)
	if err != nil {
		t.Fatal(err)
	}
	defer procTok.Close()

	var dup windows.Token
	err = windows.DuplicateTokenEx(procTok, windows.TOKEN_ALL_ACCESS, nil,
		windows.SecurityImpersonation, windows.TokenPrimary, &dup)
	if err != nil {
		t.Fatal(err)
	}
	defer dup.Close()

	systemSid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	systemOnly, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(systemSid),
		},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := setTokenDefaultDacl(dup, systemOnly); err != nil {
		t.Fatalf("fabricating SYSTEM-only default DACL: %v", err)
	}

	if err := addUserToTokenDacl(dup); err != nil {
		t.Fatalf("addUserToTokenDacl: %v", err)
	}

	dacl, err := getTokenDefaultDacl(dup)
	if err != nil {
		t.Fatal(err)
	}
	user, err := dup.GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	foundUser, foundSystem := false, false
	for _, sid := range aclSids(t, dacl) {
		if windows.EqualSid(sid, user.User.Sid) {
			foundUser = true
		}
		if windows.EqualSid(sid, systemSid) {
			foundSystem = true
		}
	}
	if !foundUser {
		t.Fatal("token user not granted in default DACL after addUserToTokenDacl")
	}
	if !foundSystem {
		t.Fatal("pre-existing SYSTEM ace was dropped by addUserToTokenDacl")
	}
}
