package agents

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestCatalogDeterministicOrderAndPathDetection(t *testing.T) {
	paths := map[string]string{
		"claude.exe":   "C:\\tools\\claude.exe",
		"codex.exe":    "C:\\tools\\codex.exe",
		"kimi.exe":     "C:\\tools\\kimi.exe",
		"opencode.exe": "C:\\tools\\opencode.exe",
	}
	lookup := func(name string) (string, error) {
		if path, ok := paths[name]; ok {
			return path, nil
		}
		return "", errors.New("not found")
	}
	probe := func(_ context.Context, executable string) (string, error) {
		return "version:" + executable, nil
	}
	catalog := newCatalog(lookup, probe, catalogDirs{})

	profiles := catalog.Profiles(context.Background())
	got := make([]AgentID, 0, len(profiles))
	for _, profile := range profiles {
		got = append(got, profile.ID)
		if !profile.Available || profile.Version == "" {
			t.Fatalf("expected available profile with version: %#v", profile)
		}
		if profile.Capabilities.AdditionalDirectories != (profile.ID != AgentOpenCode) {
			t.Fatalf("unexpected additional-directory support: %#v", profile)
		}
	}
	want := []AgentID{AgentClaude, AgentCodex, AgentKimi, AgentOpenCode}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profile order = %v, want %v", got, want)
	}
}

func TestCatalogReturnsUnavailableProfiles(t *testing.T) {
	catalog := newCatalog(
		func(string) (string, error) { return "", errors.New("not found") },
		func(context.Context, string) (string, error) { return "", nil },
		catalogDirs{},
	)

	profiles := catalog.Profiles(context.Background())
	if len(profiles) != 4 {
		t.Fatalf("got %d profiles, want 4", len(profiles))
	}
	for _, profile := range profiles {
		if profile.Available || profile.Reason == "" {
			t.Fatalf("expected unavailable profile with reason: %#v", profile)
		}
	}
}

func TestCatalogCachesUntilRefresh(t *testing.T) {
	lookups := 0
	catalog := newCatalog(
		func(name string) (string, error) {
			lookups++
			return "C:\\tools\\" + name, nil
		},
		func(context.Context, string) (string, error) { return "1.0", nil },
		catalogDirs{},
	)

	catalog.Profiles(context.Background())
	firstLookups := lookups
	catalog.Profiles(context.Background())
	if lookups != firstLookups {
		t.Fatalf("cached lookup count changed from %d to %d", firstLookups, lookups)
	}
	catalog.Refresh(context.Background())
	if lookups <= firstLookups {
		t.Fatal("refresh did not perform a new detection pass")
	}
}

func TestCatalogHandlesVersionTimeout(t *testing.T) {
	catalog := newCatalog(
		func(string) (string, error) { return "C:\\tools\\agent.exe", nil },
		func(context.Context, string) (string, error) { return "", context.DeadlineExceeded },
		catalogDirs{},
	)

	profile := catalog.Profiles(context.Background())[0]
	if !profile.Available {
		t.Fatal("an executable remains launchable when only the version probe fails")
	}
	if profile.Version != "" || profile.Reason != "version probe timed out" {
		t.Fatalf("unexpected timeout result: %#v", profile)
	}
}
