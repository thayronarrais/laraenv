//go:build windows

package workspacefiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func directoryIdentity(root *os.Root, path string) (string, string, error) {
	f, err := root.Open(".")
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	return handleIdentity(f)
}

func handleIdentity(f *os.File) (string, string, error) {
	var data windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &data); err != nil {
		return "", "", err
	}
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", "", err
	}
	if n >= uint32(len(buf)) {
		return "", "", fmt.Errorf("canonical path too long")
	}
	canonical := windows.UTF16ToString(buf[:n])
	if strings.HasPrefix(canonical, `\\?\UNC\`) {
		canonical = `\\` + strings.TrimPrefix(canonical, `\\?\UNC\`)
	} else {
		canonical = strings.TrimPrefix(canonical, `\\?\`)
	}
	canonical = strings.ToLower(filepath.Clean(canonical))
	return canonical, fmt.Sprintf("%08x:%08x:%08x", data.VolumeSerialNumber, data.FileIndexHigh, data.FileIndexLow), nil
}

func openedCanonical(f *os.File, expected string) (string, error) {
	canonical, _, err := handleIdentity(f)
	if err != nil {
		return "", err
	}
	if canonical != canonicalCase(expected) {
		return "", fmt.Errorf("file path changed during open")
	}
	return canonical, nil
}

func linked(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return data.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return false
}
