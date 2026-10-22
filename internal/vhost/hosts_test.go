package vhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostsRemoveEntryMatchesExactHostname(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	content := "127.0.0.1 demo.test # lara4ever\n127.0.0.1 feature.demo.test # lara4ever\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	editor := &HostsEditor{path: path}
	if err := editor.RemoveEntry("demo.test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), " demo.test ") || !strings.Contains(string(data), " feature.demo.test ") {
		t.Fatalf("exact host removal failed: %q", data)
	}
}

func TestHostsSnapshotRestore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	original := []byte("127.0.0.1 demo.test # lara4ever\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	editor := &HostsEditor{path: path}
	snapshot, err := editor.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := editor.AddEntry("127.0.0.1", "preview.demo.test"); err != nil {
		t.Fatal(err)
	}
	if err := editor.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatalf("hosts restore = %q, want %q", data, original)
	}
}

func TestHostsAddEntrySeparatesFileWithoutTrailingNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost"), 0644); err != nil {
		t.Fatal(err)
	}
	editor := &HostsEditor{path: path}
	if err := editor.AddEntry("127.0.0.1", "demo.test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "localhost\n127.0.0.1 demo.test") {
		t.Fatalf("entry was appended to the previous line: %q", data)
	}
}

func TestHostsRestoreEntriesPreservesUnrelatedConcurrentChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	before := []byte("127.0.0.1 alpha.test # lara4ever\r\n127.0.0.1 beta.test # lara4ever\r\n")
	if err := os.WriteFile(path, before, 0644); err != nil {
		t.Fatal(err)
	}
	editor := NewHostsEditorAt(path)
	if err := editor.RemoveEntry("alpha.test"); err != nil {
		t.Fatal(err)
	}
	if err := editor.AddEntry("127.0.0.9", "alpha.test"); err != nil {
		t.Fatal(err)
	}
	// Independent edits occur between the transaction's snapshot and rollback.
	done := make(chan error, 1)
	go func() { done <- editor.AddEntry("127.0.0.2", "other.test") }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unmanaged := "10.0.0.8\tpersonal.test # keep spacing\r\n"
	if err := os.WriteFile(path, append(current, []byte(unmanaged)...), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := editor.RestoreEntries(before, []string{"alpha.test"})
	if err != nil || !changed {
		t.Fatalf("scoped restore: changed=%v, error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	want := "127.0.0.1 beta.test # lara4ever\r\n127.0.0.2 other.test # lara4ever\n" + unmanaged + "127.0.0.1 alpha.test # lara4ever\r\n"
	if err != nil || string(got) != want {
		t.Fatalf("scoped restore changed unrelated entries: %v\ngot  %q\nwant %q", err, got, want)
	}
}

func TestHostsRestoreEntriesPreservesAliasesAndUnmanagedMatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	before := []byte("127.0.0.1 alpha.test # lara4ever\r\n")
	current := " 127.0.0.9\talpha.test  beta.test # lara4ever\r\n10.0.0.8 alpha.test # personal\r\n"
	if err := os.WriteFile(path, []byte(current), 0644); err != nil {
		t.Fatal(err)
	}
	editor := NewHostsEditorAt(path)
	if _, err := editor.RestoreEntries(before, []string{"ALPHA.test"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	want := " 127.0.0.9  beta.test # lara4ever\r\n10.0.0.8 alpha.test # personal\r\n127.0.0.1 alpha.test # lara4ever\r\n"
	if err != nil || string(got) != want {
		t.Fatalf("scoped restore lost aliases/unmanaged entry: %v\ngot  %q\nwant %q", err, got, want)
	}
}

func TestHostsRestoreEntriesSkipsWriteWhenOnlyUnrelatedEntriesChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	before := []byte("127.0.0.1 alpha.test # lara4ever\n")
	current := append(append([]byte{}, before...), []byte("10.0.0.9 beta.test # lara4ever\n")...)
	if err := os.WriteFile(path, current, 0644); err != nil {
		t.Fatal(err)
	}
	editor := NewHostsEditorAt(path)
	changed, err := editor.RestoreEntries(before, []string{"alpha.test"})
	if err != nil || changed {
		t.Fatalf("unchanged targets should not cause a write: changed=%v, error=%v", changed, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(current) {
		t.Fatalf("unrelated update was reverted: %v; %q", err, got)
	}
}
