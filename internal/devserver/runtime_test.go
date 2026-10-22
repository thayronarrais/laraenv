package devserver

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeRejectionSkipsDevserverSpawnAndLegacyFallback(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(nil, func(string, string) (string, string, error) {
		t.Error("package manager resolved after rejection")
		return "", "", nil
	}, func(string) []string { t.Error("legacy fallback used"); return nil }, nil)
	failure := errors.New("Node 24 required by worktree")
	var got []string
	manager.SetRuntimePathResolver(func(project, cwd string) ([]string, error) { got = []string{project, cwd}; return nil, failure })
	_, err := manager.StartInstance("worktree", "demo", root, "", "", 0, "echo spawned > spawned.txt")
	if !errors.Is(err, failure) || !reflect.DeepEqual(got, []string{"demo", root}) {
		t.Fatalf("args=%v error=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "spawned.txt")); !os.IsNotExist(err) {
		t.Fatalf("command spawned: %v", err)
	}
	if manager.Status("worktree").Running {
		t.Fatal("rejected process registered")
	}
}

func TestRuntimeResolverPrependsActualWorktreePath(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(nil, nil, func(string) []string { t.Error("legacy resolver used"); return nil }, nil)
	manager.SetRuntimePathResolver(func(project, cwd string) ([]string, error) {
		if project != "demo" || cwd != root {
			t.Errorf("project=%q cwd=%q", project, cwd)
		}
		return []string{`C:\worktree-php`, `C:\worktree-node`}, nil
	})
	_, err := manager.StartInstance("worktree", "demo", root, "", "", 0, "echo %PATH% > runtime-path.txt")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "runtime-path.txt")
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			if !strings.HasPrefix(string(data), `C:\worktree-php;C:\worktree-node;`) {
				t.Fatalf("PATH=%s", data)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child output did not arrive: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPackageManagerUsesRuntimePathSnapshot(t *testing.T) {
	root, fixtureExe := serverFixture(t)
	nodeA := filepath.Join(root, "node-a")
	if err := os.Mkdir(nodeA, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(fixtureExe)
	if err != nil {
		t.Fatal(err)
	}
	binA := filepath.Join(nodeA, "npm.exe")
	if err := os.WriteFile(binA, data, 0700); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(nil, func(string, string) (string, string, error) {
		t.Error("legacy package manager resolver used")
		return "", "", errors.New("unexpected legacy resolver")
	}, nil, nil)
	resolutions := 0
	manager.SetRuntimePathResolver(func(project, cwd string) ([]string, error) {
		resolutions++
		if project != "demo" || cwd != root {
			t.Errorf("project=%q cwd=%q", project, cwd)
		}
		// An editor changes the declaration after this launch has selected A.
		if err := os.WriteFile(filepath.Join(root, ".node-version"), []byte("24"), 0600); err != nil {
			t.Fatal(err)
		}
		return []string{nodeA}, nil
	})
	manager.SetProjectPackageManagerResolver(func(project, cwd, prefer string, dirs []string) (string, string, error) {
		if project != "demo" || cwd != root || prefer != "npm" || !reflect.DeepEqual(dirs, []string{nodeA}) {
			t.Fatalf("package manager args: %q %q %q %v", project, cwd, prefer, dirs)
		}
		return filepath.Join(dirs[0], "npm.exe"), "npm", nil
	})
	pid, err := manager.StartInstance("worktree", "demo", root, "", "npm", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	killServerHelper(t, pid)
	manager.mu.Lock()
	command := manager.procs["worktree"].cmd
	manager.mu.Unlock()
	var selectedPath string
	for _, entry := range command.Env {
		if strings.HasPrefix(strings.ToUpper(entry), "PATH=") {
			selectedPath = entry[len("PATH="):]
		}
	}
	if resolutions != 1 || command.Path != binA || !strings.HasPrefix(selectedPath, nodeA+string(os.PathListSeparator)) {
		t.Fatalf("resolutions=%d executable=%q PATH=%q", resolutions, command.Path, selectedPath)
	}
}

func TestPackageManagerRejectionSkipsSpawnAndLegacyFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"dev":"fixture"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(nil, func(string, string) (string, string, error) {
		t.Error("legacy package manager resolver used after rejection")
		return "", "", nil
	}, nil, nil)
	manager.SetRuntimePathResolver(func(string, string) ([]string, error) { return []string{root}, nil })
	failure := errors.New("selected Node has no package manager")
	manager.SetProjectPackageManagerResolver(func(string, string, string, []string) (string, string, error) {
		return "", "", failure
	})
	if _, err := manager.StartInstance("worktree", "demo", root, "", "npm", 0, ""); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	if manager.Status("worktree").Running || len(manager.procs) != 0 {
		t.Fatal("rejected process spawned or registered")
	}
}
