//go:build windows

package workspacefiles

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListBlocksJunction(t *testing.T) {
	svc, ref, root := fixtureService(t)
	key := mustKey(t, svc, ref)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, outside).CombinedOutput(); err != nil {
		t.Skipf("junction creation unavailable: %v (%s)", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(junction) })
	page := svc.List(ref, key, "", "")
	wantCode(t, page, "ok")
	found := false
	for _, entry := range page.Directory.Entries {
		if entry.Name == "junction" {
			found = true
			if entry.Kind != "blocked" {
				t.Fatalf("junction kind = %q", entry.Kind)
			}
		}
	}
	if !found {
		t.Fatal("junction missing from directory page")
	}
	wantCode(t, svc.Read(ref, key, "junction/secret.txt"), "invalid_path")
}
