package workspacefiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceIdentityExports(t *testing.T) {
	dir := t.TempDir()
	r, id, err := OpenDirectoryIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if id.CanonicalPath == "" || id.PhysicalID == "" {
		t.Fatalf("%#v", id)
	}
	f, err := os.Create(filepath.Join(dir, "file"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, fid, err := OpenedFileIdentity(f)
	if err != nil || fid == "" {
		t.Fatalf("%q %v", fid, err)
	}
	if ValidRelativePath("../file", false) || !ValidRelativePath("file", false) {
		t.Fatal("unsafe path validation")
	}
}
