//go:build !windows

package services

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}

func applyRestrictedToken(cmd *exec.Cmd) (func(), error) { return func() {}, nil }
