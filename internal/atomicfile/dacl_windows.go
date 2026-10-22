//go:build windows

package atomicfile

import (
	"errors"
	"runtime"

	"golang.org/x/sys/windows"
)

// DACLString serializes only the requested DACL and its inheritance controls.
// GetSecurityInfo can return incidental owner/group fields even for a DACL-only
// query; SECURITY_DESCRIPTOR.String serializes every available field (0xff).
func DACLString(descriptor *windows.SECURITY_DESCRIPTOR) (string, error) {
	if descriptor == nil {
		return "", errors.New("cannot represent a nil DACL descriptor")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return "", err
	}
	if control&windows.SE_DACL_PRESENT == 0 {
		return "", errors.New("cannot represent an absent DACL")
	}
	acl, defaulted, err := descriptor.DACL()
	if err != nil {
		return "", err
	}
	onlyDACL, err := windows.NewSecurityDescriptor()
	if err != nil {
		return "", err
	}
	if err := onlyDACL.SetDACL(acl, true, defaulted); err != nil {
		return "", err
	}
	const inheritance = windows.SE_DACL_PROTECTED | windows.SE_DACL_AUTO_INHERIT_REQ | windows.SE_DACL_AUTO_INHERITED
	if err := onlyDACL.SetControl(inheritance, control&inheritance); err != nil {
		return "", err
	}
	value := onlyDACL.String()
	runtime.KeepAlive(descriptor)
	if value == "" {
		return "", errors.New("cannot represent DACL")
	}
	return value, nil
}

// SameStagedDACL compares a captured DACL with a staged copy. SetSecurityInfo
// converts legacy inheritance to the automatic model, adding the AI control
// marker without changing access. Only that marker is ignored: protection,
// null/empty DACLs and every ACE's order, flags, type, mask and SID stay exact.
// Original-versus-current race checks must continue to use strict equality.
func SameStagedDACL(expected, actual string) (bool, error) {
	comparison := func(sddl string) (string, error) {
		descriptor, err := windows.SecurityDescriptorFromString(sddl)
		if err != nil {
			return "", err
		}
		control, _, err := descriptor.Control()
		if err != nil {
			return "", err
		}
		if control&windows.SE_DACL_PRESENT == 0 {
			return "", errors.New("cannot compare an absent staged DACL")
		}
		if err := descriptor.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
			return "", err
		}
		return DACLString(descriptor)
	}
	left, err := comparison(expected)
	if err != nil {
		return false, err
	}
	right, err := comparison(actual)
	if err != nil {
		return false, err
	}
	return left == right, nil
}
