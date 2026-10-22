package projectroutes

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-routes.json")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	routes := []Route{{ID: "canonical", ProjectName: "demo", Hostname: "demo.test", Canonical: true}}
	if err := store.ReplaceProject("demo", routes); err != nil {
		t.Fatal(err)
	}
	loaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.List("DEMO"); len(got) != 1 || got[0].Hostname != "demo.test" {
		t.Fatalf("unexpected round trip: %#v", got)
	}
}

func TestStorePreservesMalformedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-routes.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0644); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	notice := store.Notice()
	if notice == nil || notice.PreservedPath == "" {
		t.Fatal("malformed state did not produce a recovery notice")
	}
	if _, err := os.Stat(notice.PreservedPath); err != nil {
		t.Fatalf("malformed state was not preserved: %v", err)
	}
}

func TestStoreMigratesLegacyCanonicalIDPerProject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-routes.json")
	data := []byte(`{"version":1,"routes":[{"id":"canonical","projectName":"one","canonical":true},{"id":"canonical","projectName":"two","canonical":true}]}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.All(); len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatalf("legacy canonical IDs were not migrated: %#v", got)
	}
}
