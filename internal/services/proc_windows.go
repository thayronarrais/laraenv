//go:build windows

package services

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW prevents a new console window from being created
// when launching console subsystem binaries (php-cgi.exe, mysqld.exe,
// nginx.exe, taskkill.exe). Without this, each spawned process opens
// its own visible cmd window, and closing that window terminates the
// process — which is exactly what we don't want for daemons.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
