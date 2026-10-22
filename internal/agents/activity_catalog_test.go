package agents

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActivityCatalogLookupDoesNotWaitForDetection(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	catalog := newCatalog(func(string) (string, error) { return "C:/synthetic/codex.exe", nil }, func(context.Context, string) (string, error) { <-release; return "synthetic", nil }, catalogDirs{})
	finished := make(chan struct{})
	go func() { catalog.mu.Lock(); close(entered); <-release; catalog.mu.Unlock(); close(finished) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := catalog.CachedExecutable(ctx, AgentCodex); done <- err }()
	blocked := false
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		blocked = true
	}
	close(release)
	<-finished
	if blocked {
		<-done
		t.Fatal("activity lookup waited for catalog detection mutex past its deadline")
	}
	cancel()
	if _, err := catalog.CachedExecutable(ctx, AgentCodex); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled lookup: %v", err)
	}
}

func TestActivityCatalogUsesImmutableCompletedCacheWithoutProbing(t *testing.T) {
	probes := 0
	catalog := newCatalog(func(string) (string, error) { return "C:/synthetic/codex.exe", nil }, func(context.Context, string) (string, error) { probes++; return "synthetic", nil }, catalogDirs{})
	if path, err := catalog.CachedExecutable(context.Background(), AgentCodex); err != nil || path != "" || probes != 0 {
		t.Fatal("uncached lookup performed provider detection")
	}
	profiles := catalog.Profiles(context.Background())
	profiles[1].Executable = "mutated"
	catalog.mu.Lock()
	path, err := catalog.CachedExecutable(context.Background(), AgentCodex)
	catalog.mu.Unlock()
	if err != nil || path != "C:/synthetic/codex.exe" || probes != 4 {
		t.Fatalf("immutable cached lookup: %q %v probes=%d", path, err, probes)
	}
}
