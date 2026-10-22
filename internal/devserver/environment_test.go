package devserver

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentResolverBlocksDevServerBeforeSpawn(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(nil, nil, nil, nil)
	failure := errors.New("local CA is invalid")
	m.SetEnvironmentResolver(func(name, cwd string, env []string) ([]string, error) {
		if name != "demo" || cwd != dir {
			t.Fatalf("wrong checkout: %q %q", name, cwd)
		}
		return nil, failure
	})
	if _, err := m.Start("demo", dir, "", "", 0, "echo unwanted > spawned.txt"); !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "spawned.txt")); !os.IsNotExist(err) {
		t.Fatal("invalid trust environment launched a process")
	}
}
