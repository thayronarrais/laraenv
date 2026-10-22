//go:build windows

package runtimeinventory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsVersionResourceWithoutLaunchingExecutable(t *testing.T) {
	file := filepath.Join(os.Getenv("SystemRoot"), "System32", "kernel32.dll")
	if _, err := os.Stat(file); err != nil {
		t.Skip("Windows system version fixture unavailable")
	}
	got, err := readExecutableMetadata(file)
	if err != nil || got.FullVersion == "" {
		t.Fatalf("Windows resource = %+v, %v", got, err)
	}
}
