package workspacefiles

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestListPagesAndUnsafePaths(t *testing.T) {
	svc, ref, path := fixtureService(t)
	key := mustKey(t, svc, ref)
	for i := 0; i < 505; i++ {
		if err := os.WriteFile(filepath.Join(path, fmt.Sprintf("%04d.txt", i)), []byte("ok"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first := svc.List(ref, key, "", "")
	wantCode(t, first, "ok")
	if first.Directory == nil || len(first.Directory.Entries) != 500 || first.Directory.NextCursor == "" {
		t.Fatalf("first page = %#v", first.Directory)
	}
	second := svc.List(ref, key, "", first.Directory.NextCursor)
	wantCode(t, second, "ok")
	if second.Directory == nil || len(second.Directory.Entries) != 5 || second.Directory.NextCursor != "" {
		t.Fatalf("second page = %#v", second.Directory)
	}
	if err := os.Mkdir(filepath.Join(path, "other"), 0700); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.List(ref, key, "other", first.Directory.NextCursor), "stale_cursor")
	wantCode(t, svc.List(ref, key, "", "bad-cursor"), "invalid_path")
	for _, name := range []string{"../outside.txt", `C:\outside.txt`, `C:outside.txt`, `\\server\share\x`, `\\?\C:\x`, `foo:bar`, `CON.txt`, `a/../b`} {
		wantCode(t, svc.Read(ref, key, name), "invalid_path")
	}
	if err := os.WriteFile(filepath.Join(path, "new.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.List(ref, key, "", first.Directory.NextCursor), "stale_cursor")
}

func TestListBlocksLinksAndReadsUnicode(t *testing.T) {
	svc, ref, path := fixtureService(t)
	key := mustKey(t, svc, ref)
	if err := os.WriteFile(filepath.Join(path, "écran.md"), []byte("bonjour"), 0600); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.Read(ref, key, "écran.md"), "ok")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(path, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	page := svc.List(ref, key, "", "")
	wantCode(t, page, "ok")
	var kind string
	for _, e := range page.Directory.Entries {
		if e.Name == "linked" {
			kind = e.Kind
		}
	}
	if kind != "blocked" {
		t.Fatalf("linked entry kind=%q", kind)
	}
	wantCode(t, svc.Read(ref, key, "linked/file.txt"), "invalid_path")
}
