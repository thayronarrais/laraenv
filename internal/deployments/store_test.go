package deployments

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testProtector struct{ failProtect, failUnprotect bool }

func (p *testProtector) Protect(v string) (string, error) {
	if p.failProtect {
		return "", errors.New("protection failed")
	}
	return "sealed:" + base64.StdEncoding.EncodeToString([]byte(v)), nil
}
func (p *testProtector) Unprotect(v string) (string, error) {
	if p.failUnprotect {
		return "", errors.New("different account")
	}
	if !strings.HasPrefix(v, "sealed:") {
		return "", errors.New("bad envelope")
	}
	b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(v, "sealed:"))
	return string(b), e
}
func testStore(t *testing.T) (*Store, string, *testProtector) {
	t.Helper()
	root := t.TempDir()
	p := &testProtector{}
	s, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	return s, root, p
}
func mustCreate(t *testing.T, s *Store, name string) *Deployment {
	t.Helper()
	d, e := s.Create(name, "host-1")
	if e != nil {
		t.Fatal(e)
	}
	return d
}

// Removing disk persistence, encrypting only config, or returning aliased maps must fail.
func TestCRUDReopensProtectedRecordsAndReturnsIndependentValues(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "production-secret-name")
	got, e := s.Patch(d.ID, map[string]any{"os_family": "debian", "os_version": "12", "project_kind": "laravel", "config": map[string]any{"name": "remote-original", "env": map[string]any{"PAT": "secret-token"}}})
	if e != nil {
		t.Fatal(e)
	}
	got.Config["env"].(map[string]any)["PAT"] = "mutated"
	list, e := s.List()
	if e != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, e)
	}
	list[0].Name = "mutated"
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := reopened.Get(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Name != "production-secret-name" || saved.OSFamily != "debian" || saved.Config["name"] != "remote-original" || saved.Config["env"].(map[string]any)["PAT"] != "secret-token" {
		t.Fatalf("record changed: %+v", saved)
	}
	raw, e := os.ReadFile(filepath.Join(root, "index.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"secret-token", "remote-original", "production-secret-name"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("plaintext %q on disk", secret)
		}
	}
	if e := reopened.Delete(d.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := reopened.Get(d.ID); !errors.Is(e, ErrNotFound) {
		t.Fatalf("deleted: %v", e)
	}
}

// A failed protect/write must not leave uncommitted memory or erase old bytes.
func TestProtectionAndWriteFailuresRollbackMemoryAndDisk(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "original")
	before, _ := os.ReadFile(filepath.Join(root, "index.json"))
	p.failProtect = true
	if _, e := s.Patch(d.ID, map[string]any{"name": "lost"}); e == nil {
		t.Fatal("protect failure accepted")
	}
	p.failProtect = false
	after, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if string(before) != string(after) {
		t.Fatal("failed protection changed disk")
	}
	if e := os.Rename(filepath.Join(root, "index.json"), filepath.Join(root, "saved.json")); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(root, "index.json"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Patch(d.ID, map[string]any{"name": "lost"}); e == nil {
		t.Fatal("write failure accepted")
	}
	got, e := s.Get(d.ID)
	if e != nil || got.Name != "original" {
		t.Fatalf("memory rollback: %+v %v", got, e)
	}
}

// Another account may see unavailable IDs but cannot overwrite their opaque payload.
func TestUnavailableEnvelopeIsPreservedAcrossOtherRecordMutation(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "secret")
	before, _ := os.ReadFile(filepath.Join(root, "index.json"))
	p.failUnprotect = true
	locked, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	stub, e := locked.Get(d.ID)
	if !errors.Is(e, ErrSecretsUnavailable) || stub == nil || !stub.SecretsUnavailable {
		t.Fatalf("locked: %+v %v", stub, e)
	}
	if _, e := locked.Patch(d.ID, map[string]any{"name": "replacement"}); !errors.Is(e, ErrSecretsUnavailable) {
		t.Fatalf("locked mutation: %v", e)
	}
	if e := locked.Delete(d.ID); !errors.Is(e, ErrSecretsUnavailable) {
		t.Fatalf("locked deletion: %v", e)
	}
	after, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if string(before) != string(after) {
		t.Fatal("opaque envelope modified")
	}
	p.failUnprotect = false
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Get(d.ID)
	if e != nil || got.Name != "secret" {
		t.Fatalf("unlock: %+v %v", got, e)
	}
}

// Invalid/future state must fail closed without overwriting its contents.
func TestMalformedAndFutureIndexesAreNeverReplaced(t *testing.T) {
	for _, raw := range []string{`{`, `{"version":2,"records":[]}`, `{"version":1,"records":[{"id":"../../escape","payload":"sealed:e30="}]}`} {
		t.Run(raw, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "index.json")
			if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := NewStore(root, &testProtector{}); !errors.Is(e, ErrInvalidData) {
				t.Fatalf("invalid state accepted: %v", e)
			}
			got, _ := os.ReadFile(path)
			if string(got) != raw {
				t.Fatal("invalid state changed")
			}
		})
	}
}

// JSON/schema damage inside a decryptable payload must fail without rewriting old evidence.
func TestMalformedProtectedRecordIsRejectedWithoutOverwrite(t *testing.T) {
	p := &testProtector{}
	for _, plain := range []string{`{`, `{"id":"a4c9efc6-6fe6-4c70-809d-e7228328508a","name":"bad","ssh_host_external_id":"host","runs":[{"id":"../escape"}]}`} {
		t.Run(plain, func(t *testing.T) {
			root := t.TempDir()
			payload, e := p.Protect(plain)
			if e != nil {
				t.Fatal(e)
			}
			raw := `{"version":1,"records":[{"id":"a4c9efc6-6fe6-4c70-809d-e7228328508a","payload":"` + payload + `"}]}`
			path := filepath.Join(root, "index.json")
			if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := NewStore(root, p); !errors.Is(e, ErrInvalidData) {
				t.Fatalf("protected malformed record accepted: %v", e)
			}
			after, _ := os.ReadFile(path)
			if string(after) != raw {
				t.Fatal("malformed payload overwritten")
			}
		})
	}
}

// Creating another record under a different profile must keep inaccessible payloads byte-for-byte.
func TestNewRecordCanBeSavedWithoutReplacingUnavailableOpaquePayload(t *testing.T) {
	s, root, p := testStore(t)
	old := mustCreate(t, s, "old secret")
	var before diskIndex
	raw, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if e := json.Unmarshal(raw, &before); e != nil {
		t.Fatal(e)
	}
	p.failUnprotect = true
	locked, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	newRecord := mustCreate(t, locked, "new secret")
	var after diskIndex
	raw, _ = os.ReadFile(filepath.Join(root, "index.json"))
	if e := json.Unmarshal(raw, &after); e != nil {
		t.Fatal(e)
	}
	preserved := false
	for _, entry := range after.Records {
		if entry.ID == old.ID {
			preserved = entry.Payload == before.Records[0].Payload
		}
	}
	if !preserved {
		t.Fatal("unavailable protected bytes changed")
	}
	list, e := locked.List()
	if !errors.Is(e, ErrSecretsUnavailable) || len(list) != 2 {
		t.Fatalf("partial list: %+v %v", list, e)
	}
	p.failUnprotect = false
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{old.ID, newRecord.ID} {
		d, e := reopened.Get(id)
		if e != nil || d.SecretsUnavailable {
			t.Fatalf("restored record: %+v %v", d, e)
		}
	}
}

// Losing UUID/input validation could make API calls address files outside the state directory.
func TestInvalidCRUDInputsCannotMutateExistingRecords(t *testing.T) {
	s, _, _ := testStore(t)
	d := mustCreate(t, s, "app")
	for _, id := range []string{"../index.json", "not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if _, e := s.Get(id); !errors.Is(e, ErrInvalidData) {
			t.Errorf("Get(%q): %v", id, e)
		}
		if e := s.Delete(id); !errors.Is(e, ErrInvalidData) {
			t.Errorf("Delete(%q): %v", id, e)
		}
	}
	for _, patch := range []map[string]any{{"config": "script"}, {"name": ""}, {"ssh_host_external_id": "../../host"}, {"id": "42"}, {"os_family": 7}, {"config": map[string]any{"invalid": make(chan int)}}} {
		if _, e := s.Patch(d.ID, patch); !errors.Is(e, ErrInvalidData) {
			t.Errorf("invalid patch: %v", e)
		}
	}
	if _, e := s.Create("", "host"); !errors.Is(e, ErrInvalidData) {
		t.Fatalf("empty name accepted: %v", e)
	}
	list, e := s.List()
	if e != nil || len(list) != 1 || list[0].Name != "app" {
		t.Fatalf("invalid request mutated records: %+v %v", list, e)
	}
}

// An omitted/null record collection is malformed state, not permission to replace it with a new empty index.
func TestMissingIndexRecordsAreRejectedWithoutReplacement(t *testing.T) {
	for _, raw := range []string{`{"version":1}`, `{"version":1,"records":null}`} {
		t.Run(raw, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "index.json")
			if e := os.WriteFile(path, []byte(raw), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := NewStore(root, &testProtector{}); !errors.Is(e, ErrInvalidData) {
				t.Fatalf("malformed collection accepted: %v", e)
			}
			after, _ := os.ReadFile(path)
			if string(after) != raw {
				t.Fatal("malformed state overwritten")
			}
		})
	}
}
