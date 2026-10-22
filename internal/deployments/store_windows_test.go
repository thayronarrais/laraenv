//go:build windows

package deployments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/credentials"
)

// The real current-user DPAPI envelope must protect config, scripts, errors, and logs independently of cloud keys.
func TestWindowsDPAPIRoundTripInIsolatedTempDirectory(t *testing.T) {
	root := t.TempDir()
	s, e := NewStore(root, credentials.System{})
	if e != nil {
		t.Fatal(e)
	}
	d := mustCreate(t, s, "DPAPI fixture")
	if _, e := s.Patch(d.ID, map[string]any{"config": map[string]any{"name": "remote-fixture", "script": "dpapi-secret-script"}}); e != nil {
		t.Fatal(e)
	}
	r, e := s.StartRun(d.ID, "apply")
	if e != nil {
		t.Fatal(e)
	}
	if e := s.FinishRun(d.ID, r.ID, "error", "dpapi-secret-log", "dpapi-secret-error"); e != nil {
		t.Fatal(e)
	}
	if e := filepath.WalkDir(root, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		for _, secret := range []string{"dpapi-secret-script", "dpapi-secret-log", "dpapi-secret-error"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("plaintext %q on disk", secret)
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	reopened, e := NewStore(root, credentials.System{})
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Get(d.ID)
	if e != nil || got.Config["script"] != "dpapi-secret-script" {
		t.Fatalf("config: %+v %v", got, e)
	}
	log, e := reopened.ReadRunLog(d.ID, r.ID)
	if e != nil || log != "dpapi-secret-log" {
		t.Fatalf("log=%q err=%v", log, e)
	}
}
