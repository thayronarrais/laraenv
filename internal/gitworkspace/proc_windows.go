//go:build windows

package gitworkspace

import (
	"os/exec"
	"syscall"
)

func hideGitWindow(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
