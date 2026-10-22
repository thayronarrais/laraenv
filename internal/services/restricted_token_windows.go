//go:build windows

package services

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"lara4ever/internal/i18n"
)

// The app runs elevated (requireAdministrator manifest), but postgres.exe
// refuses to start under a token that holds the Administrators group. pg_ctl
// solves this by re-launching itself with a restricted token; since the
// manager spawns postgres.exe directly (to keep it a tracked child process),
// we build the same restricted token here and hand it to CreateProcessAsUser
// via SysProcAttr.Token.
//
// x/sys/windows has no CreateRestrictedToken wrapper, so call advapi32.
var procCreateRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// applyRestrictedToken attaches a primary token with the Administrators and
// Power Users groups disabled to cmd. The returned cleanup must be called
// after cmd has started (or failed to start) to release the token handle.
func applyRestrictedToken(cmd *exec.Cmd) (func(), error) {
	var procTok windows.Token
	// The restricted token handle inherits these rights; TOKEN_ADJUST_DEFAULT
	// is required for the default-DACL fix below.
	err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &procTok)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.services.openProcessTokenFailed", nil)
	}
	defer procTok.Close()

	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.services.createAdminsSidFailed", nil)
	}
	powerUsers, err := windows.CreateWellKnownSid(windows.WinBuiltinPowerUsersSid)
	if err != nil {
		return nil, i18n.Wrapf(err, "errors.services.createPowerUsersSidFailed", nil)
	}
	disable := []windows.SIDAndAttributes{
		{Sid: admins},
		{Sid: powerUsers},
	}

	// DISABLE_MAX_PRIVILEGE strips every privilege except SeChangeNotify,
	// matching pg_ctl's CreateRestrictedProcess.
	const disableMaxPrivilege = 0x1
	var restricted windows.Token
	r1, _, lastErr := procCreateRestrictedToken.Call(
		uintptr(procTok),                     // ExistingTokenHandle
		disableMaxPrivilege,                  // Flags
		uintptr(len(disable)),                // DisableSidCount
		uintptr(unsafe.Pointer(&disable[0])), // SidsToDisable
		0, 0,                                 // DeletePrivilegeCount, PrivilegesToDelete
		0, 0, // RestrictedSidCount, SidsToRestrict
		uintptr(unsafe.Pointer(&restricted)), // NewTokenHandle
	)
	if r1 == 0 {
		return nil, i18n.Wrapf(lastErr, "errors.services.createRestrictedTokenFailed", nil)
	}

	if err := addUserToTokenDacl(restricted); err != nil {
		_ = restricted.Close()
		return nil, err
	}

	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Token = syscall.Token(restricted)
	return func() { _ = restricted.Close() }, nil
}

// getTokenDefaultDacl reads the token's default DACL (the ACL stamped on
// objects the process creates without an explicit security descriptor).
func getTokenDefaultDacl(tok windows.Token) (*windows.ACL, error) {
	n := uint32(64)
	for {
		buf := make([]byte, n)
		err := windows.GetTokenInformation(tok, windows.TokenDefaultDacl, &buf[0], n, &n)
		if err == windows.ERROR_INSUFFICIENT_BUFFER {
			continue
		}
		if err != nil {
			return nil, i18n.Wrapf(err, "errors.services.getTokenDefaultDaclFailed", nil)
		}
		return (*struct{ DefaultDacl *windows.ACL })(unsafe.Pointer(&buf[0])).DefaultDacl, nil
	}
}

func setTokenDefaultDacl(tok windows.Token, acl *windows.ACL) error {
	td := struct{ DefaultDacl *windows.ACL }{DefaultDacl: acl}
	err := windows.SetTokenInformation(tok, windows.TokenDefaultDacl,
		(*byte)(unsafe.Pointer(&td)), uint32(unsafe.Sizeof(td)))
	if err != nil {
		return i18n.Wrapf(err, "errors.services.setTokenDefaultDaclFailed", nil)
	}
	return nil
}

// addUserToTokenDacl grants the token's user full access in the token's
// default DACL, preserving existing aces — pg_ctl's AddUserToTokenDacl.
//
// Why: an elevated token's default DACL grants only Administrators + SYSTEM.
// After the Administrators SID is disabled in the restricted token, a child
// created with it cannot access its own objects anymore and dies during DLL
// initialization with STATUS_DLL_INIT_FAILED (0xc0000142).
func addUserToTokenDacl(tok windows.Token) error {
	oldDacl, err := getTokenDefaultDacl(tok)
	if err != nil {
		return err
	}
	user, err := tok.GetTokenUser()
	if err != nil {
		return i18n.Wrapf(err, "errors.services.getTokenUserFailed", nil)
	}
	newDacl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		// pg_ctl adds its ace with OBJECT_INHERIT_ACE.
		Inheritance: windows.SUB_OBJECTS_ONLY_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, oldDacl)
	if err != nil {
		return i18n.Wrapf(err, "errors.services.aclFromEntriesFailed", nil)
	}
	return setTokenDefaultDacl(tok, newDacl)
}
