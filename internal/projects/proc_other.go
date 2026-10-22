//go:build !windows

package projects

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
