//go:build windows

package agents

import (
	"os/exec"
	"syscall"
)

func hideUsageWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
