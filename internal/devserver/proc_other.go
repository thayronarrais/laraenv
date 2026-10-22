//go:build !windows

package devserver

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
