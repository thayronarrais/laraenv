//go:build !windows

package pty

// livePath is a no-op off Windows; buildEnv falls back to the process PATH.
func livePath() string { return "" }
