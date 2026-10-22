//go:build !windows

package gitworkspace

import "os/exec"

func hideGitWindow(_ *exec.Cmd) {}
