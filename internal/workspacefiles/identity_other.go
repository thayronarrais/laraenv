//go:build !windows

package workspacefiles

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
)

func directoryIdentity(root *os.Root, path string) (string, string, error) {
	f, err := root.Open(".")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return "", "", err
	}
	canonical, err := openedCanonical(f, resolvedPath)
	if err != nil {
		return "", "", err
	}
	_, id, err := handleIdentity(f)
	if err != nil {
		return "", "", err
	}
	return canonical, id, nil
}

func handleIdentity(f *os.File) (string, string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", "", fmt.Errorf("unsupported filesystem identity")
	}
	return "", fmt.Sprintf("%d:%d", st.Dev, st.Ino), nil
}

func openedCanonical(f *os.File, expected string) (string, error) {
	expected = filepath.Clean(expected)
	if !filepath.IsAbs(expected) {
		return "", fmt.Errorf("canonical path is not absolute")
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	current, err := os.Lstat(expected)
	if err != nil {
		return "", err
	}
	if linked(current) || !os.SameFile(info, current) {
		return "", fmt.Errorf("file path changed during open")
	}
	if runtime.GOOS == "linux" {
		path, err := os.Readlink("/proc/self/fd/" + strconv.FormatUint(uint64(f.Fd()), 10))
		if err != nil {
			return "", err
		}
		if filepath.Clean(path) != expected {
			return "", fmt.Errorf("file path changed during open")
		}
		return filepath.Clean(path), nil
	}
	// Other Unix systems may not expose a path from the descriptor. Accept a
	// path only when Lstat proves it currently names the opened object.
	return expected, nil
}

func linked(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
