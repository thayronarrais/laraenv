//go:build windows

package workspacefiles

import "strings"

func canonicalCase(path string) string { return strings.ToLower(path) }
