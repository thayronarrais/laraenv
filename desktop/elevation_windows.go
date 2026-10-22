//go:build windows && !bindings

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"strings"
	"syscall"
)

func guiElevationArguments(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = syscall.EscapeArg(arg)
	}
	return strings.Join(quoted, " ")
}

// Only GUI startup calls this. Stdio and other early CLI handlers never ask
// for elevation or open a window, even from a non-administrator MCP client.
func ensureGUIElevation(args []string) (bool, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	cwd, _ := os.Getwd()
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	parameters, err := windows.UTF16PtrFromString(guiElevationArguments(args[1:]))
	if err != nil {
		return false, err
	}
	dir, _ := windows.UTF16PtrFromString(cwd)
	if err = windows.ShellExecute(0, verb, file, parameters, dir, windows.SW_SHOWNORMAL); err != nil {
		return false, errors.New("administrator access is required to open the LaraEnv environment")
	}
	return true, nil
}
