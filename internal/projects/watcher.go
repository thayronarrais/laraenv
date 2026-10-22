package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const projectWatchDebounce = 500 * time.Millisecond

// Watcher monitors project directories and the runtime manifests inside them.
// Registered worktree paths share the owning project's runtime callback.
type Watcher struct {
	root         string
	mu           sync.Mutex
	fw           *fsnotify.Watcher
	runtimePaths map[string]string // absolute checkout path -> owning project
}

func NewWatcher(root string) *Watcher {
	return &Watcher{root: root, runtimePaths: make(map[string]string)}
}

// AddRuntimePath watches a checkout, including worktrees outside the projects
// root. It may be called before or during Run, concurrently with other callers.
func (w *Watcher) AddRuntimePath(projectName, path string) error {
	if strings.TrimSpace(projectName) == "" || strings.TrimSpace(path) == "" {
		return fmt.Errorf("runtime watch requires a project and directory")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !isDir(path) {
		return fmt.Errorf("runtime watch directory does not exist: %s", path)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.runtimePaths == nil {
		w.runtimePaths = make(map[string]string)
	}
	if owner, ok := w.runtimePaths[path]; ok && owner != projectName {
		return fmt.Errorf("runtime watch directory already belongs to project %s", owner)
	}
	if w.fw != nil {
		if err := w.fw.Add(path); err != nil {
			return err
		}
	}
	w.runtimePaths[path] = projectName
	return nil
}

// Run blocks until ctx is cancelled. onAdd/onRemove receive the folder name
// (basename only, not full path). The optional callback receives the owning
// project when a runtime manifest changes. Existing three-argument callers
// continue to receive only project lifecycle events.
func (w *Watcher) Run(ctx context.Context, onAdd, onRemove func(name string), onRuntime ...func(name string)) error {
	if ctx.Err() != nil {
		return nil
	}
	root, err := filepath.Abs(w.root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()
	if err := fw.Add(root); err != nil {
		return err
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	known := make(map[string]bool)
	w.mu.Lock()
	if w.fw != nil {
		w.mu.Unlock()
		return fmt.Errorf("project watcher is already running")
	}
	if w.runtimePaths == nil {
		w.runtimePaths = make(map[string]string)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			known[entry.Name()] = true
			w.runtimePaths[filepath.Join(root, entry.Name())] = entry.Name()
		}
	}
	for path := range w.runtimePaths {
		if !isDir(path) {
			continue
		}
		if err := fw.Add(path); err != nil {
			w.mu.Unlock()
			return err
		}
	}
	w.fw = fw
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.fw = nil
		w.mu.Unlock()
	}()

	type pendingChange struct {
		deadline  time.Time
		path      string
		lifecycle bool
		runtime   bool
	}
	pending := make(map[string]*pendingChange)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	var timerC <-chan time.Time
	resetTimer := func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerC = nil
		var next time.Time
		for _, change := range pending {
			if next.IsZero() || change.deadline.Before(next) {
				next = change.deadline
			}
		}
		if !next.IsZero() {
			timer.Reset(time.Until(next))
			timerC = timer.C
		}
	}
	schedule := func(name, path string, lifecycle bool) {
		change := pending[name]
		if change == nil {
			change = &pendingChange{path: path}
			pending[name] = change
		}
		change.deadline = time.Now().Add(projectWatchDebounce)
		if lifecycle {
			change.lifecycle = true
			change.path = path
		} else {
			change.runtime = true
			if !change.lifecycle {
				change.path = path
			}
		}
		resetTimer()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-timerC:
			for name, change := range pending {
				if change.deadline.After(now) {
					continue
				}
				delete(pending, name)
				if ctx.Err() != nil {
					return nil
				}
				present := isDir(change.path)
				if change.lifecycle {
					if present && !known[name] {
						if err := w.AddRuntimePath(name, change.path); err != nil {
							continue
						}
						known[name] = true
						if onAdd != nil {
							onAdd(name)
						}
					} else if !present && known[name] {
						delete(known, name)
						w.mu.Lock()
						delete(w.runtimePaths, change.path)
						_ = fw.Remove(change.path)
						w.mu.Unlock()
						if onRemove != nil {
							onRemove(name)
						}
					}
				}
				// A removed external checkout still invalidates its owner's
				// snapshot. Only a root project removal suppresses this callback.
				if change.runtime && (!change.lifecycle || present) && ctx.Err() == nil && len(onRuntime) > 0 && onRuntime[0] != nil {
					onRuntime[0](name)
				}
			}
			resetTimer()
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			path := filepath.Clean(ev.Name)
			name := filepath.Base(path)
			if filepath.Dir(path) == root {
				if ev.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				if isDir(path) {
					// Attach immediately so manifests created during scaffolding
					// are observed before the lifecycle debounce expires.
					if err := w.AddRuntimePath(name, path); err == nil {
						schedule(name, path, true)
					}
				} else if known[name] || (pending[name] != nil && pending[name].lifecycle) {
					schedule(name, path, true)
				}
				continue
			}
			if !isRuntimeManifest(name) || ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			w.mu.Lock()
			owner := w.runtimePaths[filepath.Dir(path)]
			w.mu.Unlock()
			if owner != "" {
				schedule(owner, filepath.Dir(path), false)
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			_ = err // swallow; logging would be noise
		}
	}
}

func isRuntimeManifest(name string) bool {
	switch strings.ToLower(name) {
	case ".php-version", "composer.json", ".nvmrc", ".node-version", "package.json",
		"package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb":
		return true
	default:
		return false
	}
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
