//go:build windows

package gitworkspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

func canonicalExistingWorktreePath(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	buffer := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(file.Fd()), &buffer[0], uint32(len(buffer)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(buffer)) {
		return "", fmt.Errorf("invalid canonical worktree path length")
	}
	canonical := windows.UTF16ToString(buffer[:n])
	if strings.HasPrefix(canonical, `\\?\UNC\`) {
		canonical = `\\` + strings.TrimPrefix(canonical, `\\?\UNC\`)
	} else {
		canonical = strings.TrimPrefix(canonical, `\\?\`)
	}
	return filepath.Clean(canonical), nil
}
