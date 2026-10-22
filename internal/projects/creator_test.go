package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"lara4ever/internal/i18n"
)

func TestCreatorDependencyInstallUsesGeneratedProjectNode(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "my-app")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, "package.json"), []byte(`{"engines":{"node":"^22.11.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	globalNode := filepath.Join(root, "global-node")
	projectNode := filepath.Join(root, "project node")
	for _, dir := range []string{globalNode, projectNode} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "npm.cmd"), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	c := &Creator{root: root}
	c.SetNodeResolver(func() string { return globalNode })
	c.SetProjectNodeResolver(func(name, path string) (string, error) {
		if name != "my-app" || path != projectPath {
			return "", errors.New("resolver must receive generated checkout identity")
		}
		r := ResolveNode(path, "", "24.1.0", []RuntimeCandidate{
			{Version: "24.1.0", FullVersion: "24.1.0"},
			{Version: "22.11.0", FullVersion: "22.11.0"},
		}, nil)
		if r.Version != "22.11.0" {
			return "", errors.New("generated manifest was not used")
		}
		return projectNode, nil
	})
	cmd, err := c.dependencyInstallCommand(context.Background(), "my-app", "npm")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != filepath.Join(projectNode, "npm.cmd") || cmd.Dir != projectPath {
		t.Fatalf("dependency command = %q in %q, want generated project's manager and checkout", cmd.Path, cmd.Dir)
	}
	if want := []string{filepath.Join(projectNode, "npm.cmd"), "install", "--no-fund", "--no-audit"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, want)
	}
	for _, entry := range cmd.Env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "PATH") {
			if first := strings.Split(value, string(os.PathListSeparator))[0]; first != projectNode {
				t.Fatalf("first PATH entry = %q, want selected Node %q", first, projectNode)
			}
			return
		}
	}
	t.Fatal("dependency command has no explicit Node PATH")
}

func TestCreatorDependencyResolutionErrorPreservesProject(t *testing.T) {
	root := t.TempDir()
	projectPath := filepath.Join(root, "my-app")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(projectPath, "package.json")
	if err := os.WriteFile(manifest, []byte(`{"engines":{"node":">=99"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	c := &Creator{root: root}
	wantErr := errors.New("required Node is not installed")
	c.SetProjectNodeResolver(func(string, string) (string, error) { return "", wantErr })
	cmd, err := c.dependencyInstallCommand(context.Background(), "my-app", "npm")
	if cmd != nil || !errors.Is(err, wantErr) {
		t.Fatalf("dependency selection = (%v, %v), want no command and the resolution error", cmd, err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("generated project was not preserved: %v", err)
	}
}

func TestCreatorDependencyInstallWithoutProjectResolverUsesGlobalNode(t *testing.T) {
	for _, pm := range []string{"npm", "pnpm", "yarn", "bun"} {
		t.Run(pm, func(t *testing.T) {
			nodeDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(nodeDir, pm+".cmd"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			c := &Creator{root: t.TempDir()}
			c.SetNodeResolver(func() string { return nodeDir })
			cmd, err := c.dependencyInstallCommand(context.Background(), "my-app", pm)
			if err != nil {
				t.Fatal(err)
			}
			want := []string{filepath.Join(nodeDir, pm+".cmd"), "install"}
			if pm == "npm" {
				want = append(want, "--no-fund", "--no-audit")
			}
			if !reflect.DeepEqual(cmd.Args, want) {
				t.Fatalf("args = %#v, want %#v", cmd.Args, want)
			}
		})
	}
}

func TestCreatorNextTemplateDefersDependencyInstall(t *testing.T) {
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		t.Fatal(err)
	}
	previous := i18n.Global()
	i18n.SetGlobal(set)
	t.Cleanup(func() { i18n.SetGlobal(previous) })
	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "npm.cmd"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	c := &Creator{root: t.TempDir()}
	c.SetNodeResolver(func() string { return nodeDir })
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Inspect the attempted command without launching a real template.
	var lines []string
	if err := c.createJS(ctx, CreateOptions{Name: "my-app", Kind: "next"}, func(line string) { lines = append(lines, line) }); err == nil {
		t.Fatal("canceled scaffold unexpectedly succeeded")
	}
	if len(lines) == 0 || !strings.Contains(lines[0], "next-app@latest my-app --yes -- --yes --skip-install") {
		t.Fatalf("Next template would install before project runtime resolution: %v", lines)
	}
}

// TestResolvePHPSeriesPrefersPinnedVersion covers the common case: a
// pinned phpVersion always wins, regardless of what php.exe path was
// resolved for it.
func TestResolvePHPSeriesPrefersPinnedVersion(t *testing.T) {
	got := resolvePHPSeries("8.4", `C:\laraenv\php\8.4\php.exe`)
	if got != "8.4" {
		t.Errorf("resolvePHPSeries = %q, want %q", got, "8.4")
	}
}

// TestResolvePHPSeriesReadsSeriesFromManagedPath is the regression this
// helper exists for: an unpinned request ("" — use whatever's active) must
// still resolve to the right series by reading it back out of the managed
// install path, so a redistributable lookup for that series (e.g. 8.6's
// vs18 requirement) doesn't silently fall back to the default. This is the
// person most likely to hit a missing runtime: someone who set the 8.6
// beta as their active PHP.
func TestResolvePHPSeriesReadsSeriesFromManagedPath(t *testing.T) {
	got := resolvePHPSeries("", `C:\laraenv\php\8.6\php.exe`)
	if got != "8.6" {
		t.Errorf("resolvePHPSeries = %q, want %q", got, "8.6")
	}
}

// TestResolvePHPSeriesUnmanagedPathYieldsNoMatchingSeries: an unmanaged
// system php (the LookPath fallback) doesn't live under <root>\php\<series>,
// so the derived "series" won't match any catalog entry — callers get a
// value that safely resolves to "" (the default redistributable) rather
// than a crafted/guessed one.
func TestResolvePHPSeriesUnmanagedPathYieldsNoMatchingSeries(t *testing.T) {
	got := resolvePHPSeries("", `C:\Program Files\PHP\php.exe`)
	if got == "8.4" || got == "8.5" || got == "8.6" {
		t.Errorf("resolvePHPSeries unexpectedly matched a real series: %q", got)
	}
}
