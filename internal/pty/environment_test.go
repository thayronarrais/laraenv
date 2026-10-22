package pty

import (
	"errors"
	"testing"
)

func TestEnvironmentResolverBlocksPTYBeforeSpawn(t *testing.T) {
	r := NewRegistry()
	failure := errors.New("local CA is invalid")
	r.SetEnvironmentResolver(func(cwd string, env []string) ([]string, error) {
		if cwd != "project-checkout" {
			t.Fatalf("wrong checkout: %q", cwd)
		}
		return nil, failure
	})
	if _, _, err := r.New("missing-shell", 80, 24, nil, "project-checkout"); !errors.Is(err, failure) {
		t.Fatalf("shell error=%v", err)
	}
	if _, _, err := r.NewCommand("missing-agent", nil, 80, 24, nil, "project-checkout", nil); !errors.Is(err, failure) {
		t.Fatalf("agent error=%v", err)
	}
}
