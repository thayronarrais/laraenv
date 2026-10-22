//go:build windows

package mcpbridge

import (
	"context"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDescriptorHasProtectedWindowsACL(t *testing.T) {
	s := startTestServer(t, backendFunc(func(context.Context, string, Args, func(string)) (any, error) { return nil, nil }), nil)
	sd, err := windows.GetNamedSecurityInfo(s.Status().DescriptorPath, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("descriptor inherits permissions")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("descriptor must grant only its user and SYSTEM, got %#v", dacl)
	}
}
