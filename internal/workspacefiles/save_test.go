package workspacefiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSavePreservesTextMetadata(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after []byte
	}{
		{"bom-crlf", []byte("\xef\xbb\xbfold\r\n"), []byte("\xef\xbb\xbfnew\r\n")},
		{"lf", []byte("old\n"), []byte("new\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, ref, dir := fixtureService(t)
			key := mustKey(t, svc, ref)
			file := filepath.Join(dir, "note.md")
			if err := os.WriteFile(file, tc.before, 0640); err != nil {
				t.Fatal(err)
			}
			beforeInfo, _ := os.Stat(file)
			read := svc.Read(ref, key, "note.md")
			wantCode(t, read, "ok")
			saved := svc.Save(ref, key, "note.md", "new\n", read.Document.Revision)
			wantCode(t, saved, "ok")
			actual, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, tc.after) {
				t.Fatalf("saved %q, want %q", actual, tc.after)
			}
			afterInfo, _ := os.Stat(file)
			if afterInfo.Mode().Perm() != beforeInfo.Mode().Perm() {
				t.Fatal("permissions changed")
			}
			if saved.Document == nil || saved.Document.Revision == read.Document.Revision || saved.Document.Text != "new\n" {
				t.Fatalf("saved=%#v", saved)
			}
		})
	}
}

func TestSaveConcurrentRevision(t *testing.T) {
	svc, ref, dir := fixtureService(t)
	key := mustKey(t, svc, ref)
	file := filepath.Join(dir, "note.md")
	if err := os.WriteFile(file, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first := svc.Read(ref, key, "note.md")
	wantCode(t, first, "ok")
	if err := os.WriteFile(file, []byte("external\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conflict := svc.Save(ref, key, "note.md", "mine\n", first.Document.Revision)
	wantCode(t, conflict, "conflict")
	if conflict.Document == nil || conflict.Document.Text != "external\n" {
		t.Fatalf("missing latest: %#v", conflict)
	}
	disk, _ := os.ReadFile(file)
	if string(disk) != "external\n" {
		t.Fatalf("clobbered: %q", disk)
	}
	latest := svc.Read(ref, key, "note.md")
	results := make(chan Result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, text := range []string{"one\n", "two\n"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			<-start
			results <- svc.Save(ref, key, "note.md", text, latest.Document.Revision)
		}(text)
	}
	close(start)
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for result := range results {
		switch result.Code {
		case "ok":
			ok++
		case "conflict":
			conflicts++
		default:
			t.Fatalf("unexpected %#v", result)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("ok=%d conflicts=%d", ok, conflicts)
	}
	// This serializes LaraEnv saves only. An external writer between the final
	// comparison and rename is not covered by a filesystem compare-and-swap.
}

func TestSaveRefusesInvalidOrReadonly(t *testing.T) {
	for _, tc := range []struct {
		name, old, content, code string
		readonly                 bool
	}{
		{"mixed", "a\r\nb\n", "new\n", "unsupported_format", false},
		{"nul", "old", "a\x00b", "unsupported_format", false},
		{"utf8", "old", string([]byte{255}), "unsupported_format", false},
		{"cr", "old", "a\rb", "unsupported_format", false},
		{"crlf-limit", "\xef\xbb\xbfold\r\n", strings.Repeat("x\n", 1<<20), "too_large", false},
		{"bom-limit", "\xef\xbb\xbfold", strings.Repeat("x", 2<<20), "too_large", false},
		{"readonly", "old", "new", "permission_denied", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, ref, dir := fixtureService(t)
			key := mustKey(t, svc, ref)
			file := filepath.Join(dir, "note.md")
			if err := os.WriteFile(file, []byte(tc.old), 0600); err != nil {
				t.Fatal(err)
			}
			read := svc.Read(ref, key, "note.md")
			wantCode(t, read, "ok")
			if tc.readonly {
				if err := os.Chmod(file, 0400); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(file, 0600) })
			}
			wantCode(t, svc.Save(ref, key, "note.md", tc.content, read.Document.Revision), tc.code)
			disk, _ := os.ReadFile(file)
			if string(disk) != tc.old {
				t.Fatalf("modified: %q", disk)
			}
		})
	}
	svc, ref, _ := fixtureService(t)
	key := mustKey(t, svc, ref)
	wantCode(t, svc.Save(ref, key, "missing.md", "new", "anything"), "unavailable")
	wantCode(t, svc.Save(ref, key, "../escape", "new", "anything"), "invalid_path")
	wantCode(t, svc.Save(ref, "", "note.md", "new", "anything"), "invalid_path")
}
