//go:build !windows

package queueworker

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
