package deployments

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func importFixture() ImportBundle {
	return ImportBundle{Version: 1, Source: ImportSource{ServiceURL: "https://cloud.example.test", AccountID: "account-42"}, HostMappings: map[string]string{"cloud-host": "local-host"}, Deployments: []Deployment{{ID: "42", Name: "Imported display name", SSHHostExternalID: "cloud-host", Config: map[string]any{"name": "original-remote-name", "script": "sensitive-import-script"}, OSFamily: "ubuntu", LastStatus: "success", Runs: []RunSummary{{ID: "5", Kind: "apply", Status: "success", StartedAt: "2026-01-01T00:00:00Z", HasLog: true}}}}}
}

// Duplicate imports, lost remote apply names, or source IDs leaking into local IDs must fail.
func TestImportMapsHostsAndOriginIdempotentlyWithoutLogsOrExecution(t *testing.T) {
	s, root, p := testStore(t)
	bundle := importFixture()
	result, e := s.Import(bundle)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Inserted) != 1 || len(result.Skipped) != 0 || len(result.Conflicts) != 0 {
		t.Fatalf("import: %+v", result)
	}
	d, e := s.Get(result.Inserted[0])
	if e != nil {
		t.Fatal(e)
	}
	if d.ID == "42" || !validUUID(d.ID) || d.SSHHostExternalID != "local-host" || d.Config["name"] != "original-remote-name" || d.Origin == nil || d.Origin.ID != "42" || d.Origin.Source.AccountID != "account-42" || len(d.Runs) != 1 || d.Runs[0].HasLog || !validUUID(d.Runs[0].ID) {
		t.Fatalf("imported: %+v", d)
	}
	if _, e := os.Stat(filepath.Join(root, "logs")); !os.IsNotExist(e) {
		t.Fatalf("import created/executed logs: %v", e)
	}
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	again, e := reopened.Import(bundle)
	if e != nil || len(again.Skipped) != 1 || again.Skipped[0] != d.ID || len(again.Inserted) != 0 {
		t.Fatalf("repeat: %+v %v", again, e)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if strings.Contains(string(raw), "sensitive-import-script") {
		t.Fatal("import saved plaintext")
	}
}

// Divergent local or source data must produce reviewable conflicts rather than overwrite.
func TestImportConflictsPreserveLocalEditsAndScopeOrigins(t *testing.T) {
	s, _, _ := testStore(t)
	bundle := importFixture()
	first, e := s.Import(bundle)
	if e != nil {
		t.Fatal(e)
	}
	id := first.Inserted[0]
	if _, e := s.Patch(id, map[string]any{"name": "local edit"}); e != nil {
		t.Fatal(e)
	}
	conflict, e := s.Import(bundle)
	if e != nil || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].ExistingID != id || len(conflict.Inserted) != 0 {
		t.Fatalf("local divergence: %+v %v", conflict, e)
	}
	got, e := s.Get(id)
	if e != nil || got.Name != "local edit" {
		t.Fatalf("edit overwritten: %+v %v", got, e)
	}
	bundle.Source.AccountID = "different-account"
	other, e := s.Import(bundle)
	if e != nil || len(other.Inserted) != 1 || other.Inserted[0] == id {
		t.Fatalf("account scope: %+v %v", other, e)
	}
	bundle.Deployments[0].Config["name"] = "source changed"
	changed, e := s.Import(bundle)
	if e != nil || len(changed.Conflicts) != 1 {
		t.Fatalf("source divergence: %+v %v", changed, e)
	}
}

// Validation must cover the entire bundle before any first record becomes visible.
func TestImportRejectsInvalidBundleAtomically(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ImportBundle)
	}{
		{"future version", func(b *ImportBundle) { b.Version = 2 }},
		{"missing mapping", func(b *ImportBundle) { b.HostMappings = nil }},
		{"path ID", func(b *ImportBundle) {
			b.Deployments = append(b.Deployments, Deployment{ID: "../escape", Name: "bad", SSHHostExternalID: "cloud-host"})
		}},
		{"malformed ID", func(b *ImportBundle) { b.Deployments[0].ID = "not-an-id" }},
		{"duplicate source ID", func(b *ImportBundle) { b.Deployments = append(b.Deployments, b.Deployments[0]) }},
		{"bad mapping", func(b *ImportBundle) { b.HostMappings["cloud-host"] = "../escape" }},
		{"missing account scope", func(b *ImportBundle) { b.Source.AccountID = "" }},
		{"credential URL", func(b *ImportBundle) { b.Source.ServiceURL = "https://secret:password@cloud.example.test" }},
		{"bad run ID", func(b *ImportBundle) { b.Deployments[0].Runs[0].ID = "../../escape" }},
		{"oversize", func(b *ImportBundle) { b.Deployments[0].Config["script"] = strings.Repeat("x", MaxImportBytes) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, root, _ := testStore(t)
			b := importFixture()
			tc.mutate(&b)
			if _, e := s.Import(b); !errors.Is(e, ErrInvalidData) {
				t.Fatalf("accepted: %v", e)
			}
			list, e := s.List()
			if e != nil || len(list) != 0 {
				t.Fatalf("partial import: %+v %v", list, e)
			}
			if _, e := os.Stat(filepath.Join(root, "index.json")); !os.IsNotExist(e) {
				t.Fatalf("invalid import wrote index: %v", e)
			}
		})
	}
}

// A failed final write or protection cannot publish any imported deployment.
func TestImportWriteAndProtectionFailuresLeaveOldState(t *testing.T) {
	for _, failure := range []string{"protect", "write"} {
		t.Run(failure, func(t *testing.T) {
			s, root, p := testStore(t)
			existing := mustCreate(t, s, "existing")
			if failure == "protect" {
				p.failProtect = true
			} else {
				path := filepath.Join(root, "index.json")
				if e := os.Rename(path, path+".saved"); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := s.Import(importFixture()); e == nil {
				t.Fatal("failure accepted")
			}
			p.failProtect = false
			list, e := s.List()
			if e != nil || len(list) != 1 || list[0].ID != existing.ID {
				t.Fatalf("partial publication: %+v %v", list, e)
			}
		})
	}
}

// Imported running summaries are historical, and must not be mistaken for a local active run.
func TestImportedRunningSummaryIsNotRecoveredAsLocalRun(t *testing.T) {
	s, root, p := testStore(t)
	b := importFixture()
	b.Deployments[0].Runs[0].Status = "running"
	b.Deployments[0].Runs[0].FinishedAt = ""
	result, e := s.Import(b)
	if e != nil {
		t.Fatal(e)
	}
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Get(result.Inserted[0])
	if e != nil || got.Runs[0].Status != "running" || got.Runs[0].Interrupted {
		t.Fatalf("invented interrupted execution: %+v %v", got, e)
	}
	var parsed ImportBundle
	if e := json.Unmarshal([]byte(`{"version":1,"source":"offline-export-2026","deployments":[],"host_mappings":{}}`), &parsed); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Import(parsed); e != nil {
		t.Fatalf("explicit string source: %v", e)
	}
}
