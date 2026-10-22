package deployments

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Losing persisted running state or recovering completed/imported rows breaks recovery.
func TestStartedRunIsDurableAndOnlyIncompleteKnownRunsAreInterrupted(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "app")
	run, e := s.StartRun(d.ID, "apply")
	if e != nil {
		t.Fatal(e)
	}
	if run.ID == "" || run.Status != "running" || run.StartedAt == "" {
		t.Fatalf("run: %+v", run)
	}
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Get(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(got.Runs) != 1 || got.Runs[0].ID != run.ID || got.Runs[0].Status != "interrupted" || !got.Runs[0].Interrupted || got.Runs[0].FinishedAt == "" || got.LastStatus != "interrupted" {
		t.Fatalf("recovered: %+v", got)
	}
	if _, e := s.StartRun(d.ID, "../../escape"); !errors.Is(e, ErrInvalidData) {
		t.Fatalf("invalid kind: %v", e)
	}
}

// Plain logs/errors, missing truncation, and overwritten final status are observable regressions.
func TestFinishRunProtectsSeparateLogAndPreservesTruncationAcrossRestart(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "app")
	r, e := s.StartRun(d.ID, "deploy")
	if e != nil {
		t.Fatal(e)
	}
	log := "sensitive-script-token\n" + strings.Repeat("x", MaxLogBytes)
	if e := s.FinishRun(d.ID, r.ID, "success", log, "sensitive-error-token"); e != nil {
		t.Fatal(e)
	}
	got, e := s.Get(d.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.LastStatus != "success" || !got.Runs[0].Truncated || !got.Runs[0].HasLog || got.Runs[0].Status != "success" || got.Runs[0].ErrorMessage != "sensitive-error-token" {
		t.Fatalf("finished: %+v", got)
	}
	body, e := s.ReadRunLog(d.ID, r.ID)
	if e != nil || len(body) != MaxLogBytes || !strings.HasPrefix(body, "sensitive-script-token") {
		t.Fatalf("log bytes=%d err=%v", len(body), e)
	}
	if e := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "sensitive-script-token") || strings.Contains(string(raw), "sensitive-error-token") {
			t.Errorf("plaintext at %s", path)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	reopened, e := NewStore(root, p)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := reopened.Get(d.ID)
	if e != nil || saved.Runs[0].Status != "success" || !saved.Runs[0].Truncated {
		t.Fatalf("reopened: %+v %v", saved, e)
	}
	p.failUnprotect = true
	if _, e := reopened.ReadRunLog(d.ID, r.ID); !errors.Is(e, ErrSecretsUnavailable) {
		t.Fatalf("locked log: %v", e)
	}
}

// Publishing a log before failed index publication must not mark completion or prevent retry of persistence.
func TestFinishRunIndexFailureRollsBackRecordAndLog(t *testing.T) {
	s, root, _ := testStore(t)
	d := mustCreate(t, s, "app")
	r, e := s.StartRun(d.ID, "destroy")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "index.json")
	if e := os.Rename(path, path+".saved"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := s.FinishRun(d.ID, r.ID, "success", "remote succeeded", "error"); e == nil {
		t.Fatal("failed index publication accepted")
	}
	got, e := s.Get(d.ID)
	if e != nil || got.Runs[0].Status != "running" || got.Runs[0].HasLog {
		t.Fatalf("memory not rolled back: %+v %v", got, e)
	}
	if _, e := os.Stat(filepath.Join(root, "logs", d.ID, r.ID+".json")); !os.IsNotExist(e) {
		t.Fatalf("uncommitted log survived: %v", e)
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(path+".saved", path); e != nil {
		t.Fatal(e)
	}
	if e := s.FinishRun(d.ID, r.ID, "success", "remote succeeded", ""); e != nil {
		t.Fatalf("persistence retry: %v", e)
	}
	if e := s.FinishRun(d.ID, r.ID, "error", "replacement", ""); !errors.Is(e, ErrConflict) {
		t.Fatalf("finalized run overwritten: %v", e)
	}
}

// A log path obstacle must leave a durable running record, with no partial final status.
func TestFinishRunLogWriteFailurePreservesRunningRecord(t *testing.T) {
	s, root, _ := testStore(t)
	d := mustCreate(t, s, "app")
	r, e := s.StartRun(d.ID, "apply")
	if e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "logs"), []byte("obstacle"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.FinishRun(d.ID, r.ID, "error", "secret log", "secret error"); e == nil {
		t.Fatal("log write failure accepted")
	}
	got, e := s.Get(d.ID)
	if e != nil || got.Runs[0].Status != "running" || got.Runs[0].ErrorMessage != "" {
		t.Fatalf("partial finalization: %+v %v", got, e)
	}
}

// Incorrect retention would keep unbounded summaries/logs or evict the newest run.
func TestRunHistoryRetainsNewestHundredAndRemovesEvictedLogs(t *testing.T) {
	s, root, _ := testStore(t)
	d := mustCreate(t, s, "app")
	var first, last string
	for i := 0; i < 101; i++ {
		r, e := s.StartRun(d.ID, "detect")
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = r.ID
		}
		last = r.ID
		if e := s.FinishRun(d.ID, r.ID, "success", "bounded log", ""); e != nil {
			t.Fatal(e)
		}
	}
	got, e := s.Get(d.ID)
	if e != nil || len(got.Runs) != 100 || got.Runs[99].ID != last {
		t.Fatalf("retention: %+v %v", got, e)
	}
	if _, e := s.ReadRunLog(d.ID, first); !errors.Is(e, ErrNotFound) {
		t.Fatalf("evicted run returned log: %v", e)
	}
	if _, e := os.Stat(filepath.Join(root, "logs", d.ID, first+".json")); !os.IsNotExist(e) {
		t.Fatalf("evicted log exists: %v", e)
	}
}

// Appending a multibyte rune at the byte cap must not create corrupt or oversized JSON-decoded logs.
func TestLogTruncationKeepsValidUTF8AndNeverExceedsByteLimit(t *testing.T) {
	s, _, _ := testStore(t)
	d := mustCreate(t, s, "app")
	r, e := s.StartRun(d.ID, "deploy")
	if e != nil {
		t.Fatal(e)
	}
	if e := s.FinishRun(d.ID, r.ID, "success", strings.Repeat("a", MaxLogBytes-1)+"€", ""); e != nil {
		t.Fatal(e)
	}
	body, e := s.ReadRunLog(d.ID, r.ID)
	if e != nil || len(body) != MaxLogBytes-1 {
		t.Fatalf("multibyte truncation bytes=%d err=%v", len(body), e)
	}
}

// A failed start must never appear as an executable/durable operation; deletion also rolls back.
func TestStartAndDeleteFailuresLeavePriorRecordIntact(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "app")
	before, _ := os.ReadFile(filepath.Join(root, "index.json"))
	p.failProtect = true
	if _, e := s.StartRun(d.ID, "apply"); e == nil {
		t.Fatal("unpersisted run start accepted")
	}
	p.failProtect = false
	got, e := s.Get(d.ID)
	if e != nil || len(got.Runs) != 0 {
		t.Fatalf("uncommitted start visible: %+v %v", got, e)
	}
	path := filepath.Join(root, "index.json")
	if e := os.Rename(path, path+".saved"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	if e := s.Delete(d.ID); e == nil {
		t.Fatal("unpersisted deletion accepted")
	}
	got, e = s.Get(d.ID)
	if e != nil || got.Name != "app" {
		t.Fatalf("uncommitted deletion visible: %+v %v", got, e)
	}
	saved, _ := os.ReadFile(path + ".saved")
	if string(saved) != string(before) {
		t.Fatal("failure changed last durable record")
	}
}

// Protection errors must not leave log plaintext, partial completion, or changed prior bytes.
func TestFinishProtectionFailurePreservesDurableStartedState(t *testing.T) {
	s, root, p := testStore(t)
	d := mustCreate(t, s, "app")
	r, e := s.StartRun(d.ID, "apply")
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(root, "index.json"))
	p.failProtect = true
	if e := s.FinishRun(d.ID, r.ID, "success", "sensitive log", "sensitive error"); e == nil {
		t.Fatal("log protection error accepted")
	}
	p.failProtect = false
	after, _ := os.ReadFile(filepath.Join(root, "index.json"))
	if string(before) != string(after) {
		t.Fatal("protection error changed durable state")
	}
	got, e := s.Get(d.ID)
	if e != nil || got.Runs[0].Status != "running" {
		t.Fatalf("uncommitted result visible: %+v %v", got, e)
	}
	if _, e := os.Stat(filepath.Join(root, "logs", d.ID, r.ID+".json")); !os.IsNotExist(e) {
		t.Fatalf("failed protection wrote log: %v", e)
	}
}

// An opaque file left by a crash/foreign profile must never be overwritten by finishing a run.
func TestFinishNeverOverwritesExistingOpaqueLog(t *testing.T) {
	s, root, _ := testStore(t)
	d := mustCreate(t, s, "app")
	r, e := s.StartRun(d.ID, "apply")
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "logs", d.ID)
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, r.ID+".json")
	opaque := []byte(`{"version":1,"payload":"unavailable-for-this-account"}`)
	if e := os.WriteFile(path, opaque, 0600); e != nil {
		t.Fatal(e)
	}
	if e := s.FinishRun(d.ID, r.ID, "success", "new log", ""); !errors.Is(e, ErrSecretsUnavailable) {
		t.Fatalf("opaque replacement allowed: %v", e)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(opaque) {
		t.Fatal("existing opaque log overwritten")
	}
}
