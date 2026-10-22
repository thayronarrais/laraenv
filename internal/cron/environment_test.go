package cron

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvironmentResolverBlocksCronBeforeSpawn(t *testing.T) {
	dir := t.TempDir()
	j := Job{ID: "tls", ProjectName: "demo", WorkingDir: dir, Command: "echo unwanted > spawned.txt"}
	m := NewManager(nil, nil, nil)
	m.jobs[j.ID] = &j
	m.SetEnvironmentResolver(func(name, cwd string, env []string) ([]string, error) {
		if name != "demo" || cwd != dir {
			t.Fatalf("wrong checkout: %q %q", name, cwd)
		}
		return nil, errors.New("local CA is invalid")
	})
	m.execute(&j)
	if j.LastStatus != "error" || j.LastError != "local CA is invalid" {
		t.Fatalf("job status = %s %s", j.LastStatus, j.LastError)
	}
	if _, err := os.Stat(filepath.Join(dir, "spawned.txt")); !os.IsNotExist(err) {
		t.Fatal("cron launched despite invalid environment")
	}
}

func TestEnvironmentResolverReachesCronChild(t *testing.T) {
	dir := t.TempDir()
	j := Job{ID: "tls", ProjectName: "demo", WorkingDir: dir, Command: "echo %LARAENV_TLS_FIXTURE%"}
	m := NewManager(nil, nil, nil)
	m.jobs[j.ID] = &j
	m.SetEnvironmentResolver(func(_, _ string, env []string) ([]string, error) {
		return append(env, "LARAENV_TLS_FIXTURE=public-ca-path"), nil
	})
	m.execute(&j)
	if j.LastStatus != "ok" || strings.TrimSpace(j.LastOutput) != "public-ca-path" {
		t.Fatalf("child environment did not reach cron: %s %q", j.LastStatus, j.LastOutput)
	}
}
