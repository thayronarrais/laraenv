//go:build windows

package gitworkspace

import (
	"os/exec"
	"testing"
)

func TestHideGitWindowPreventsConsoleFlash(t *testing.T) {
	command := exec.Command("git", "--version")
	hideGitWindow(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("Git helper process is not hidden")
	}
	if command.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("Git helper process does not use CREATE_NO_WINDOW")
	}
}
