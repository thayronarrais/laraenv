//go:build !windows

package workspacefiles

func canonicalCase(path string) string { return path }
