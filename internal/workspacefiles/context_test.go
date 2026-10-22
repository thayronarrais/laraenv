package workspacefiles

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureService(t *testing.T) (*Service, ContextRef, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	ref := ContextRef{Kind: "agent", SessionID: "session-1"}
	svc := NewService(func(got ContextRef) (string, string, error) {
		if got != ref {
			return "", "", os.ErrNotExist
		}
		return path, "Agent checkout", nil
	})
	return svc, ref, path
}

func mustKey(t *testing.T, svc *Service, ref ContextRef) string {
	t.Helper()
	r := svc.Context(ref)
	if r.Code != "ok" || r.Context == nil || r.Context.RootKey == "" {
		t.Fatalf("context = %#v", r)
	}
	return r.Context.RootKey
}

func wantCode(t *testing.T, got Result, code string) {
	t.Helper()
	if got.Code != code {
		t.Fatalf("code = %q, want %q (message %q)", got.Code, code, got.Message)
	}
}

func TestContextRootReplacementAndMissingAgent(t *testing.T) {
	svc, ref, path := fixtureService(t)
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	oldKey := mustKey(t, svc, ref)
	if err := os.Rename(path, path+"-old"); err != nil {
		// Some restricted Windows environments prohibit directory rename even
		// after closing the root handle; removal still exercises replacement.
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "README.md"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.Read(ref, oldKey, "README.md"), "unavailable")
	if newKey := mustKey(t, svc, ref); newKey == oldKey {
		t.Fatal("recreated directory reused root identity")
	}
	wantCode(t, svc.Context(ContextRef{Kind: "agent", SessionID: "missing"}), "unavailable")
	wantCode(t, svc.Context(ContextRef{Kind: "agent", SessionID: "session-1", ProjectName: "other"}), "invalid_path")
}
