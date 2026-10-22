package workspacefiles

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// A filesystem's enumeration order must not determine page membership.
func TestListDirectoriesBeforeFilesAcrossPages(t *testing.T) {
	svc, ref, root := fixtureService(t)
	key := mustKey(t, svc, ref)
	want := make([]DirectoryEntry, 0, 620)
	for i := 0; i < 510; i++ {
		name := fmt.Sprintf("z-dir-%03d", i)
		want = append(want, DirectoryEntry{Name: name, RelativePath: name, Kind: "directory"})
	}
	for i := 0; i < 110; i++ {
		name := fmt.Sprintf("a-file-%03d.txt", i)
		want = append(want, DirectoryEntry{Name: name, RelativePath: name, Kind: "file"})
	}
	for _, i := range rand.New(rand.NewSource(42)).Perm(len(want)) {
		entry := want[i]
		var err error
		if entry.Kind == "directory" {
			err = os.Mkdir(filepath.Join(root, entry.Name), 0700)
		} else {
			err = os.WriteFile(filepath.Join(root, entry.Name), []byte("ok"), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	first := svc.List(ref, key, "", "")
	wantCode(t, first, "ok")
	if len(first.Directory.Entries) != 500 || first.Directory.NextCursor == "" {
		t.Fatalf("first page: %#v", first.Directory)
	}
	second := svc.List(ref, key, "", first.Directory.NextCursor)
	wantCode(t, second, "ok")
	if len(second.Directory.Entries) != 120 || second.Directory.NextCursor != "" {
		t.Fatalf("second page: %#v", second.Directory)
	}
	got := append(first.Directory.Entries, second.Directory.Entries...)
	if !reflect.DeepEqual(got, want) {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("entry %d = %#v, want %#v", i, got[i], want[i])
			}
		}
	}
}

func TestListOrderCaseUnicode(t *testing.T) {
	svc, ref, root := fixtureService(t)
	key := mustKey(t, svc, ref)
	for _, name := range []string{"λ号.txt", "Écran.md", "Zebra.txt", "beta.txt", "alpha.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Zfolder", "afolder"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	got := svc.List(ref, key, "", "")
	wantCode(t, got, "ok")
	names := []string{}
	for _, entry := range got.Directory.Entries {
		names = append(names, entry.Name)
	}
	want := []string{"afolder", "Zfolder", "alpha.txt", "beta.txt", "Zebra.txt", "Écran.md", "λ号.txt"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %q, want %q", names, want)
	}
}

func TestListOrderedCursorChanges(t *testing.T) {
	svc, ref, root := fixtureService(t)
	key := mustKey(t, svc, ref)
	if err := os.Mkdir(filepath.Join(root, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 505; i++ {
		if err := os.WriteFile(filepath.Join(root, "child", fmt.Sprintf("%03d.txt", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	first := svc.List(ref, key, "child", "")
	wantCode(t, first, "ok")
	data, err := base64.RawURLEncoding.DecodeString(first.Directory.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state["version"] != json.Number("2") || state["lastKind"] != "file" || state["lastName"] != "499.txt" || state["lastFoldedName"] != "499.txt" {
		t.Fatalf("cursor is not an ordered v2 continuation: %s", data)
	}
	second := svc.List(ref, key, "child", first.Directory.NextCursor)
	wantCode(t, second, "ok")
	if len(second.Directory.Entries) != 5 || second.Directory.Entries[0].RelativePath != "child/500.txt" || second.Directory.NextCursor != "" {
		t.Fatalf("child continuation: %#v", second.Directory)
	}
	for _, tc := range []struct {
		name, field string
		value       any
		code        string
	}{
		{"version", "version", 1, "stale_cursor"}, {"root", "rootKey", "other", "stale_cursor"}, {"path", "relativePath", "", "stale_cursor"},
		{"identity", "identity", "other", "stale_cursor"}, {"mtime", "modified", 1, "stale_cursor"}, {"kind", "lastKind", "blocked", "invalid_path"},
		{"folded", "lastFoldedName", "500.txt", "invalid_path"}, {"name", "lastName", "../499.txt", "invalid_path"},
		{"unknown", "offset", 500, "invalid_path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := make(map[string]any, len(state))
			for k, v := range state {
				changed[k] = v
			}
			changed[tc.field] = tc.value
			encoded, _ := json.Marshal(changed)
			wantCode(t, svc.List(ref, key, "child", base64.RawURLEncoding.EncodeToString(encoded)), tc.code)
		})
	}
	old := map[string]any{"rootKey": key, "relativePath": "child", "identity": state["identity"], "modified": state["modified"], "offset": 500}
	encoded, _ := json.Marshal(old)
	wantCode(t, svc.List(ref, key, "child", base64.RawURLEncoding.EncodeToString(encoded)), "invalid_path")
	wantCode(t, svc.List(ref, key, "", first.Directory.NextCursor), "stale_cursor")
	// Force a distinct mtime without depending on the filesystem clock granularity.
	st, err := os.Stat(filepath.Join(root, "child"))
	if err != nil {
		t.Fatal(err)
	}
	changedTime := st.ModTime().Add(-10 * time.Second)
	if err := os.Chtimes(filepath.Join(root, "child"), changedTime, changedTime); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.List(ref, key, "child", first.Directory.NextCursor), "stale_cursor")
	// Replacement with the original timestamp must still invalidate identity.
	if err := os.Rename(filepath.Join(root, "child"), filepath.Join(root, "old-child")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "child"), st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	wantCode(t, svc.List(ref, key, "child", first.Directory.NextCursor), "stale_cursor")
}

func TestListOrderedRevalidatesAuthorityAfterScan(t *testing.T) {
	_, ref, original := fixtureService(t)
	replacement := t.TempDir()
	duringList := false
	resolves := 0
	svc := NewService(func(got ContextRef) (string, string, error) {
		if got != ref {
			return "", "", os.ErrNotExist
		}
		if duringList {
			resolves++
			if resolves == 2 {
				return replacement, "replacement", nil
			}
		}
		return original, "original", nil
	})
	key := mustKey(t, svc, ref)
	duringList = true
	result := svc.List(ref, key, "", "")
	wantCode(t, result, "unavailable")
	if result.Directory != nil {
		t.Fatalf("authority changed but returned a page: %#v", result.Directory)
	}
}

func TestReadFormatAndBounds(t *testing.T) {
	svc, ref, path := fixtureService(t)
	key := mustKey(t, svc, ref)
	cases := []struct {
		name            string
		data            []byte
		code, text, eol string
		bom, readOnly   bool
	}{
		{"bom-crlf", append([]byte{0xef, 0xbb, 0xbf}, []byte("a\r\nb\r\n")...), "ok", "a\nb\n", "crlf", true, false},
		{"lf", []byte("a\nb\n"), "ok", "a\nb\n", "lf", false, false},
		{"mixed", []byte("a\r\nb\n"), "ok", "a\nb\n", "mixed", false, true},
		{"binary", []byte{'a', 0, 'b'}, "unsupported_format", "", "", false, false},
		{"invalid-utf8", []byte{0xff}, "unsupported_format", "", "", false, false},
		{"limit", bytes.Repeat([]byte{'a'}, 2<<20), "ok", strings.Repeat("a", 2<<20), "lf", false, false},
		{"over-limit", bytes.Repeat([]byte{'a'}, (2<<20)+1), "too_large", "", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.name + ".txt"
			if err := os.WriteFile(filepath.Join(path, name), tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			got := svc.Read(ref, key, name)
			wantCode(t, got, tc.code)
			if tc.code == "ok" {
				if got.Document == nil || got.Document.Text != tc.text || got.Document.EOL != tc.eol || got.Document.BOM != tc.bom || got.Document.ReadOnly != tc.readOnly || got.Document.Size != int64(len(tc.data)) || got.Document.Revision == "" {
					t.Fatalf("snapshot = %#v", got.Document)
				}
			}
		})
	}
	before := svc.Read(ref, key, "lf.txt")
	wantCode(t, svc.Check(ref, key, "lf.txt", before.Document.Revision), "unchanged")
	if err := os.WriteFile(filepath.Join(path, "lf.txt"), []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := svc.Check(ref, key, "lf.txt", before.Document.Revision)
	wantCode(t, changed, "changed")
	if changed.Document == nil || changed.Document.Text != "new\n" {
		t.Fatalf("changed = %#v", changed)
	}
}

func TestReadBoundedAfterFileGrowsSinceStat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "growing.txt")
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 1 {
		t.Fatalf("initial size=%d", st.Size())
	}
	appendFile, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := appendFile.Write(bytes.Repeat([]byte{'a'}, 2<<20)); err != nil {
		t.Fatal(err)
	}
	if err := appendFile.Close(); err != nil {
		t.Fatal(err)
	}
	data, issue := readBounded(f, 2<<20)
	if issue.Code != "too_large" || data != nil {
		t.Fatalf("grown file: bytes=%d issue=%#v", len(data), issue)
	}
}

func TestCheckRejectsNewlyInvalidFormat(t *testing.T) {
	svc, ref, root := fixtureService(t)
	key := mustKey(t, svc, ref)
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("valid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first := svc.Read(ref, key, "note.md")
	wantCode(t, first, "ok")
	if err := os.WriteFile(path, []byte{0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	got := svc.Check(ref, key, "note.md", first.Document.Revision)
	wantCode(t, got, "unsupported_format")
	if got.Document != nil {
		t.Fatalf("invalid format returned a snapshot: %#v", got.Document)
	}
}
