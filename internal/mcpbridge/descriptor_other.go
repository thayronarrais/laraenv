//go:build !windows

package mcpbridge

import (
	"errors"
	"os"
)

func protectDescriptor(path string) error           { return os.Chmod(path, 0600) }
func replaceDescriptor(source, target string) error { return os.Rename(source, target) }
func checkDescriptorPrivacy(_ string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("local MCP connection permissions must restrict access to its owner")
	}
	return nil
}
