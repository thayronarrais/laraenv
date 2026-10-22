package queueworker

import (
	"errors"
	"testing"
)

func TestEnvironmentResolverBlocksWorkerBeforeSpawn(t *testing.T) {
	dir, exe := workerFixture(t)
	m := NewManager(nil, func(string) string { return exe })
	failure := errors.New("local CA is invalid")
	m.SetEnvironmentResolver(func(name, cwd string, env []string) ([]string, error) {
		if name != "demo" || cwd != dir {
			t.Fatalf("wrong checkout: %q %q", name, cwd)
		}
		return nil, failure
	})
	if _, err := m.Start("demo", dir, nil); !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
	if m.Status("demo").Running {
		t.Fatal("worker launched despite invalid environment")
	}
}
