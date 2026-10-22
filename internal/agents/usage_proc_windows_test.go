//go:build windows

package agents

import (
	"os/exec"
	"testing"
)

func TestAgentHelperProcessesDoNotFlashConsoleWindows(t *testing.T) {
	command := exec.Command("cmd.exe", "/c", "exit", "0")
	hideUsageWindow(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("agent helper process is not hidden")
	}
	if command.SysProcAttr.CreationFlags&0x08000000 == 0 {
		t.Fatal("agent helper process does not use CREATE_NO_WINDOW")
	}
}
