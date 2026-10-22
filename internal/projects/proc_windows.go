//go:build windows

package projects

import (
	"os/exec"
	"syscall"
)

// createNoWindow stops Go's exec from popping a stray console window when
// launching console-subsystem binaries (php.exe, composer running through
// php, npm/pnpm/yarn, git). Without it the user sees a black cmd window
// flicker over the modal during every create-project — including a php.exe
// window that lingers while composer pulls dependencies.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
