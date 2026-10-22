package editor

import (
	"path/filepath"
	"testing"
)

func TestOpenFileUsesParentDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notes.md")
	cmd, err := openFileCommand(Profile{Kind: KindVSCode, Available: true, Path: "code.exe"}, file, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "code.exe" || cmd.Args[1] != file || cmd.Dir != filepath.Dir(file) {
		t.Fatalf("argv=%q cwd=%q", cmd.Args, cmd.Dir)
	}
}
