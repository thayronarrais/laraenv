package terminal

import (
	"errors"
	"testing"
)

func TestEnvironmentResolverBlocksExternalTerminalBeforeSpawn(t *testing.T) {
	failure := errors.New("local public CA is invalid")
	err := OpenWithCommand(Profile{Available: true, Kind: KindCmd, Path: "missing-shell.exe"}, "project-checkout", "demo", "demo", "", nil, func(cwd string, env []string) ([]string, error) {
		if cwd != "project-checkout" {
			t.Fatalf("wrong checkout: %q", cwd)
		}
		return nil, failure
	})
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}
