package projectroutes

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerCreatesOneDefaultCanonicalRoute(t *testing.T) {
	manager, store := testManager(t, nil)
	routes, err := manager.List(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || !routes[0].Canonical || routes[0].Hostname != "demo.test" || routes[0].TargetKind != TargetMainCheckout {
		t.Fatalf("unexpected default routes: %#v", routes)
	}
	loaded, err := NewStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.List("demo"); len(got) != 1 || !got[0].Canonical {
		t.Fatalf("default canonical route was not persisted: %#v", got)
	}
}

func TestCanonicalIDsAreUniqueAcrossProjects(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store,
		func(_ context.Context, name string) (Project, error) {
			return Project{Name: name, MainPath: filepath.Join(`C:\projects`, name), TLD: "test"}, nil
		},
		func(_ context.Context, project Project, path string) (Target, error) {
			return Target{Path: path, Kind: TargetMainCheckout}, nil
		}, nil,
	)
	for _, name := range []string{"one", "two"} {
		if _, err := manager.List(context.Background(), name); err != nil {
			t.Fatal(err)
		}
	}
	if got := store.All(); len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatalf("canonical routes collided: %#v", got)
	}
}

func TestManagerRepairsMissingCanonicalAndPreviewTargets(t *testing.T) {
	missing := map[string]bool{}
	manager, store := testManager(t, nil)
	manager.target = func(_ context.Context, project Project, path string) (Target, error) {
		if missing[path] {
			return Target{}, errors.New("missing")
		}
		kind := TargetWorktree
		if path == project.MainPath {
			kind = TargetMainCheckout
		}
		return Target{Path: path, Kind: kind}, nil
	}
	worktree := `C:\worktrees\demo\feature`
	if _, err := manager.ConfigureWorktree(context.Background(), "demo", worktree, true, "feature.demo.test", 0); err != nil {
		t.Fatal(err)
	}
	missing[worktree] = true
	repairs, err := manager.RepairMissingTargets(context.Background(), []string{"demo"})
	if err != nil {
		t.Fatal(err)
	}
	routes := store.List("demo")
	if len(repairs) != 2 || len(routes) != 1 || !routes[0].Canonical || routes[0].TargetKind != TargetMainCheckout {
		t.Fatalf("missing routes were not repaired: repairs=%#v routes=%#v", repairs, routes)
	}
}

func TestManagerMigratesCanonicalAndPreviewTLDInOneApply(t *testing.T) {
	project := Project{Name: "demo", MainPath: `C:\projects\demo`, TLD: "test"}
	applies := 0
	manager, _ := testManagerWithProject(t, project, func(_ context.Context, _ Project, _, _ []Route) error {
		applies++
		return nil
	})
	if _, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\feature`, "feature.demo.test", 0); err != nil {
		t.Fatal(err)
	}
	project.TLD = "local"
	manager.project = func(_ context.Context, _ string) (Project, error) { return project, nil }
	routes, err := manager.MigrateTLD(context.Background(), "demo", "test")
	if err != nil {
		t.Fatal(err)
	}
	if applies != 2 || routes[0].Hostname != "demo.local" || routes[1].Hostname != "feature.demo.local" {
		t.Fatalf("unexpected TLD migration: applies=%d routes=%#v", applies, routes)
	}
}

func TestManagerMovesCanonicalWithoutCreatingSecondCanonical(t *testing.T) {
	manager, _ := testManager(t, nil)
	if _, err := manager.List(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	routes, err := manager.SetCanonical(context.Background(), "demo", `C:\worktrees\demo\feature`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || !routes[0].Canonical || routes[0].TargetKind != TargetWorktree || routes[0].WorktreePath == "" {
		t.Fatalf("canonical route was not moved atomically: %#v", routes)
	}
}

func TestManagerRejectsInvalidAndDuplicatePreviewHosts(t *testing.T) {
	manager, _ := testManager(t, nil)
	if _, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\feature`, "www.demo.test", 0); err == nil {
		t.Fatal("reserved preview hostname unexpectedly succeeded")
	}
	first, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\feature`, "feature.demo.test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Canonical || first.Hostname != "feature.demo.test" {
		t.Fatalf("unexpected preview route: %#v", first)
	}
	if _, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\other`, "feature.demo.test", 0); err == nil {
		t.Fatal("duplicate preview hostname unexpectedly succeeded")
	}
}

func TestManagerRestoresPersistedStateWhenApplyFails(t *testing.T) {
	applyErr := errors.New("nginx validation failed")
	manager, store := testManager(t, func(context.Context, Project, []Route, []Route) error { return applyErr })
	before, err := manager.List(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.SetCanonical(context.Background(), "demo", `C:\worktrees\demo\feature`, 0); !errors.Is(err, applyErr) {
		t.Fatalf("got error %v, want %v", err, applyErr)
	}
	after := store.List("demo")
	if len(after) != 1 || after[0].TargetPath != before[0].TargetPath || after[0].TargetKind != TargetMainCheckout {
		t.Fatalf("route state was not rolled back: before=%#v after=%#v", before, after)
	}
}

func TestManagerRequiresDedicatedPortForProxyPreview(t *testing.T) {
	manager, _ := testManagerWithProject(t, Project{Name: "demo", MainPath: `C:\projects\demo`, TLD: "test", MainProxyPort: 5173}, nil)
	if _, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\feature`, "feature.demo.test", 0); err == nil {
		t.Fatal("proxy preview without dedicated port unexpectedly succeeded")
	}
	if _, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\feature`, "feature.demo.test", 5173); err == nil {
		t.Fatal("proxy preview reused the main checkout port")
	}
	route, err := manager.CreatePreview(context.Background(), "demo", `C:\worktrees\demo\feature`, "feature.demo.test", 5174)
	if err != nil {
		t.Fatal(err)
	}
	if route.ServeMode != ServeProxy || route.ProxyPort != 5174 {
		t.Fatalf("unexpected proxy preview: %#v", route)
	}
}

func TestManagerConfiguresCanonicalAndPreviewInOneApply(t *testing.T) {
	applies := 0
	manager, _ := testManager(t, func(_ context.Context, _ Project, _, _ []Route) error {
		applies++
		return nil
	})
	routes, err := manager.ConfigureWorktree(context.Background(), "demo", `C:\worktrees\demo\feature`, true, "feature.demo.test", 0)
	if err != nil {
		t.Fatal(err)
	}
	if applies != 1 || len(routes) != 2 || routes[0].TargetKind != TargetWorktree || !routes[0].Canonical || routes[1].Canonical {
		t.Fatalf("routing was not applied as one canonical+preview transaction: applies=%d routes=%#v", applies, routes)
	}
}

func TestManagerUpdatesAllWorktreeRoutesToActualDevServerPort(t *testing.T) {
	project := Project{Name: "demo", MainPath: `C:\projects\demo`, TLD: "test", MainProxyPort: 5173}
	manager, _ := testManagerWithProject(t, project, nil)
	worktree := `C:\worktrees\demo\feature`
	if _, err := manager.ConfigureWorktree(context.Background(), "demo", worktree, true, "feature.demo.test", 5174); err != nil {
		t.Fatal(err)
	}
	routes, err := manager.UpdateWorktreeProxyPort(context.Background(), "demo", worktree, 5175)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].ProxyPort != 5175 || routes[1].ProxyPort != 5175 {
		t.Fatalf("routes did not follow the actual process port: %#v", routes)
	}
	if _, err := manager.UpdateWorktreeProxyPort(context.Background(), "demo", worktree, 5173); err == nil {
		t.Fatal("worktree was allowed to reuse the main proxy port")
	}
}

func TestManagerDetachesWorktreeRoutesInOneApply(t *testing.T) {
	applies := 0
	manager, _ := testManager(t, func(_ context.Context, _ Project, _, _ []Route) error { applies++; return nil })
	worktree := `C:\worktrees\demo\feature`
	if _, err := manager.ConfigureWorktree(context.Background(), "demo", worktree, true, "feature.demo.test", 0); err != nil {
		t.Fatal(err)
	}
	routes, err := manager.DetachWorktree(context.Background(), "demo", worktree)
	if err != nil {
		t.Fatal(err)
	}
	if applies != 2 || len(routes) != 1 || !routes[0].Canonical || routes[0].TargetKind != TargetMainCheckout {
		t.Fatalf("unexpected detached routes: applies=%d routes=%#v", applies, routes)
	}
}

func TestManagerSuggestsSanitizedPreviewHostname(t *testing.T) {
	manager, _ := testManager(t, nil)
	if got := manager.SuggestPreview("Demo", "test", "Feature/My UI"); got != "feature-my-ui.demo.test" {
		t.Fatalf("suggestion = %q", got)
	}
}

func testManager(t *testing.T, apply ApplyFunc) (*Manager, *Store) {
	t.Helper()
	return testManagerWithProject(t, Project{Name: "demo", MainPath: `C:\projects\demo`, TLD: "test"}, apply)
}

func testManagerWithProject(t *testing.T, project Project, apply ApplyFunc) (*Manager, *Store) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(
		store,
		func(_ context.Context, name string) (Project, error) {
			if name != project.Name {
				return Project{}, errors.New("project not found")
			}
			return project, nil
		},
		func(_ context.Context, got Project, path string) (Target, error) {
			if path == got.MainPath {
				return Target{Path: path, Kind: TargetMainCheckout}, nil
			}
			return Target{Path: path, Kind: TargetWorktree}, nil
		},
		apply,
	)
	manager.now = func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) }
	return manager, store
}
