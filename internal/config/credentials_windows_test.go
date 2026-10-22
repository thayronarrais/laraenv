//go:build windows

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestFailedAtomicCredentialMigrationPreservesOriginalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"language":"pt-BR","projects":{"sshHosts":{"fixture":{"name":"fixture","password":"legacy-fixture-only"}}}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	// Allow reads/writes but deny deletion/replacement, like an editor holding
	// a Windows handle without FILE_SHARE_DELETE. Real atomic persistence fails.
	handle, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if _, err := newStore(path, fixturePasswordProtector{}); err == nil {
		t.Fatal("failed replacement must surface migration failure")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("failed atomic credential migration changed original file")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("failed migration left a temporary file")
	}
}
