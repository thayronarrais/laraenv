package projects

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type watcherEvent struct {
	kind string
	name string
}

func runWatcher(t *testing.T, watcher *Watcher) (chan watcherEvent, context.CancelFunc, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan watcherEvent, 32)
	done := make(chan error, 1)
	go func() {
		done <- watcher.Run(ctx,
			func(name string) { events <- watcherEvent{"add", name} },
			func(name string) { events <- watcherEvent{"remove", name} },
			func(name string) { events <- watcherEvent{"runtime", name} },
		)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		watcher.mu.Lock()
		ready := watcher.fw != nil
		watcher.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("watcher did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("watcher stopped with %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("watcher did not stop")
		}
	})
	return events, cancel, done
}

func expectWatcherEvent(t *testing.T, events <-chan watcherEvent, want watcherEvent) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("event=%+v, want %+v", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("missing event %+v", want)
	}
}

func expectNoWatcherEvent(t *testing.T, events <-chan watcherEvent) {
	t.Helper()
	select {
	case got := <-events:
		t.Fatalf("unexpected event %+v", got)
	case <-time.After(650 * time.Millisecond):
	}
}

func TestWatcherExistingProjectManifestsDebounceWithoutLifecycleEvents(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "demo")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	events, _, _ := runWatcher(t, NewWatcher(root))
	for _, name := range []string{".php-version", "composer.json", ".nvmrc", ".node-version", "package.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb", "package-lock.json", "npm-shrinkwrap.json"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	expectNoWatcherEvent(t, events)
}

func TestWatcherIgnoresUnrelatedFilesAndRootFiles(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "demo")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	events, _, _ := runWatcher(t, NewWatcher(root))
	for _, path := range []string{filepath.Join(project, "index.php"), filepath.Join(root, "package.json")} {
		if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	expectNoWatcherEvent(t, events)
}

func TestWatcherHandlesManifestWriteAtomicReplaceAndRemove(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "demo")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(project, "composer.json")
	if err := os.WriteFile(manifest, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	events, _, _ := runWatcher(t, NewWatcher(root))
	if err := os.WriteFile(manifest, []byte("{\"require\":{}}"), 0600); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	tmp := filepath.Join(project, "composer.json.tmp")
	if err := os.WriteFile(tmp, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(manifest, manifest+".backup"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, manifest); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	expectNoWatcherEvent(t, events)
}

func TestWatcherAddsAndRemovesOnlyProjectDirectories(t *testing.T) {
	root := t.TempDir()
	events, _, _ := runWatcher(t, NewWatcher(root))
	project := filepath.Join(root, "added")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"add", "added"})
	if err := os.WriteFile(filepath.Join(project, ".nvmrc"), []byte("22"), 0600); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "added"})
	if err := os.RemoveAll(project); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"remove", "added"})
	expectNoWatcherEvent(t, events)
}

func TestWatcherTracksWorktreesRegisteredBeforeAndDuringRun(t *testing.T) {
	watcher := NewWatcher(t.TempDir())
	before := t.TempDir()
	if err := watcher.AddRuntimePath("demo", before); err != nil {
		t.Fatal(err)
	}
	events, _, _ := runWatcher(t, watcher)
	if err := os.WriteFile(filepath.Join(before, ".php-version"), []byte("8.4"), 0600); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	after := t.TempDir()
	if err := watcher.AddRuntimePath("other", after); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(after, ".node-version"), []byte("24"), 0600); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "other"})
}

func TestWatcherCancellationDiscardsDebouncedCallbacks(t *testing.T) {
	root := t.TempDir()
	events, cancel, _ := runWatcher(t, NewWatcher(root))
	if err := os.Mkdir(filepath.Join(root, "pending"), 0700); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	expectNoWatcherEvent(t, events)
}

func TestWatcherDebouncesAcrossWorktreesWhenFirstCheckoutIsRemoved(t *testing.T) {
	root, first, second := t.TempDir(), t.TempDir(), t.TempDir()
	watcher := NewWatcher(root)
	if err := watcher.AddRuntimePath("demo", first); err != nil {
		t.Fatal(err)
	}
	if err := watcher.AddRuntimePath("demo", second); err != nil {
		t.Fatal(err)
	}
	events, _, _ := runWatcher(t, watcher)
	if err := os.WriteFile(filepath.Join(first, ".nvmrc"), []byte("22"), 0600); err != nil {
		t.Fatal(err)
	}
	// The first checkout has already queued a runtime update, but has not
	// reached the debounce deadline when a different checkout changes.
	time.Sleep(100 * time.Millisecond)
	if err := os.RemoveAll(first); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, ".nvmrc"), []byte("24"), 0600); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	expectNoWatcherEvent(t, events)
}

func TestWatcherRemovedExternalCheckoutStillInvalidatesOwner(t *testing.T) {
	root, checkout := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(checkout, ".php-version"), []byte("8.3"), 0600); err != nil {
		t.Fatal(err)
	}
	watcher := NewWatcher(root)
	if err := watcher.AddRuntimePath("demo", checkout); err != nil {
		t.Fatal(err)
	}
	events, _, _ := runWatcher(t, watcher)
	if err := os.RemoveAll(checkout); err != nil {
		t.Fatal(err)
	}
	expectWatcherEvent(t, events, watcherEvent{"runtime", "demo"})
	expectNoWatcherEvent(t, events)
}
