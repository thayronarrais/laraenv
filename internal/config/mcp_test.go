package config

import (
	"path/filepath"
	"testing"
)

func TestMCPPreferenceAndProjectPausePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s, e := NewStore(path)
	if e != nil {
		t.Fatal(e)
	}
	if s.Get().Experimental.MCPEnabled {
		t.Fatal("MCP must be opt in")
	}
	if e = s.SetMCPEnabled(true); e != nil {
		t.Fatal(e)
	}
	if e = s.SetProjectPaused("demo", true); e != nil {
		t.Fatal(e)
	}
	s, e = NewStore(path)
	if e != nil {
		t.Fatal(e)
	}
	if !s.Get().Experimental.MCPEnabled || !s.Get().Projects.Paused["demo"] {
		t.Fatal("state not persisted")
	}
	if e = s.SetProjectPaused("demo", false); e != nil {
		t.Fatal(e)
	}
	if s.Get().Projects.Paused["demo"] {
		t.Fatal("resume not persisted")
	}
}

func TestGetReturnsIndependentSnapshot(t *testing.T) {
	s := newTestStore(t)
	s.SetProjectPhp("demo", "8.3")
	s.SetProjectTags("demo", []string{"a"})
	got := s.Get()
	got.Projects.PhpVersions["demo"] = "bad"
	got.Projects.ProjectTags["demo"][0] = "bad"
	got.Stack.Enabled["nginx"] = false
	after := s.Get()
	if after.Projects.PhpVersions["demo"] != "8.3" || after.Projects.ProjectTags["demo"][0] != "a" || !after.Stack.Enabled["nginx"] {
		t.Fatal("snapshot mutated shared config")
	}
}
