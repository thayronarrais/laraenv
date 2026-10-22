//go:build !windows

package agents

import "os/exec"

func hideUsageWindow(_ *exec.Cmd) {}
