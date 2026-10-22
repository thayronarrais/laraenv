//go:build !windows

package gitworkspace

import "path/filepath"

func canonicalExistingWorktreePath(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
