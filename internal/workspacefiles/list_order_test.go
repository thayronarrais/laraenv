package workspacefiles

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestListOrderedCursorRequiresAllKeys(t *testing.T) {
	current := pageCursor{Version: 2, RootKey: "root", Identity: "identity", Modified: 1}
	// Empty relativePath is still an explicitly required key for root listings.
	for _, payload := range []string{
		`{"version":2,"rootKey":"root","identity":"identity","modified":1,"lastKind":"file","lastFoldedName":"alpha","lastName":"alpha"}`,
		`{"version":2,"rootKey":"root","relativePath":null,"identity":"identity","modified":1,"lastKind":"file","lastFoldedName":"alpha","lastName":"alpha"}`,
	} {
		if _, err := decodePageCursor(base64.RawURLEncoding.EncodeToString([]byte(payload)), current); err == nil {
			t.Fatalf("accepted missing/null cursor key: %s", payload)
		}
	}
}

func TestListOrderOriginalNameTieAndBlockedGroup(t *testing.T) {
	entries := []DirectoryEntry{
		{Name: "alpha", Kind: "file"}, {Name: "Zulu", Kind: "directory"},
		{Name: "Écran", Kind: "file"}, {Name: "Alpha", Kind: "blocked"},
		{Name: "écran", Kind: "file"}, {Name: "beta", Kind: "blocked"},
	}
	sort.Slice(entries, func(i, j int) bool { return entryOrderKey(entries[i]).less(entryOrderKey(entries[j])) })
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	want := []string{"Zulu", "Alpha", "alpha", "beta", "Écran", "écran"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("case ties / blocked group: %q, want %q", names, want)
	}
}

type observedDirectory struct {
	*os.File
	maxRead int
	reads   int
	onRead  func()
	err     error
}

func (d *observedDirectory) ReadDir(n int) ([]os.DirEntry, error) {
	d.maxRead = max(d.maxRead, n)
	d.reads++
	if d.err != nil {
		return nil, d.err
	}
	batch, err := d.File.ReadDir(n)
	if d.onRead != nil {
		d.onRead()
		d.onRead = nil
	}
	return batch, err
}

func TestListOrderBoundedCandidates(t *testing.T) {
	var candidates candidateHeap
	highWater := 0
	// Reverse input forces replacement of the heap's largest retained item.
	for i := 1999; i >= 0; i-- {
		entry := DirectoryEntry{Name: fmt.Sprintf("%04d.txt", i), Kind: "file"}
		candidates.offer(orderedCandidate{entry: entry, key: entryOrderKey(entry)})
		highWater = max(highWater, len(candidates))
	}
	if highWater > 501 || len(candidates) != 501 {
		t.Fatalf("candidate high-water=%d retained=%d, want <=501 and 501", highWater, len(candidates))
	}
	ordered := candidates.ordered()
	for i, entry := range ordered {
		if entry.entry.Name != fmt.Sprintf("%04d.txt", i) {
			t.Fatalf("candidate %d=%q", i, entry.entry.Name)
		}
	}
	_, _, rootPath := fixtureService(t)
	for i := 0; i < 620; i++ {
		if err := os.WriteFile(filepath.Join(rootPath, fmt.Sprintf("%04d.txt", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := &observedDirectory{File: f}
	got, issue := scanOrderedDirectory(root, "", d, before, nil)
	if issue.Code != "" || len(got) != 501 || d.maxRead > 256 || d.reads < 3 {
		t.Fatalf("scan: entries=%d issue=%#v maxRead=%d reads=%d", len(got), issue, d.maxRead, d.reads)
	}
	for i, entry := range got {
		if entry.entry.Name != fmt.Sprintf("%04d.txt", i) {
			t.Fatalf("scan candidate %d=%q", i, entry.entry.Name)
		}
	}
}

func TestListOrderedDirectoryChangesDuringScan(t *testing.T) {
	_, _, rootPath := fixtureService(t)
	if err := os.WriteFile(filepath.Join(rootPath, "initial.txt"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := &observedDirectory{File: f, onRead: func() {
		if err := os.WriteFile(filepath.Join(rootPath, "new.txt"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		changed := before.ModTime().Add(-10 * time.Second)
		if err := os.Chtimes(rootPath, changed, changed); err != nil {
			t.Fatal(err)
		}
	}}
	got, issue := scanOrderedDirectory(root, "", d, before, nil)
	if got != nil || issue.Code != "stale_cursor" {
		t.Fatalf("mutated directory: candidates=%d issue=%#v", len(got), issue)
	}
}

func TestListOrderReadErrors(t *testing.T) {
	_, _, rootPath := fixtureService(t)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		err  error
		code string
	}{{errors.New("read failure"), "io_error"}, {os.ErrPermission, "permission_denied"}} {
		got, issue := scanOrderedDirectory(root, "", &observedDirectory{File: f, err: tc.err}, before, nil)
		if got != nil || issue.Code != tc.code {
			t.Fatalf("read error %v: candidates=%d issue=%#v", tc.err, len(got), issue)
		}
	}
	got, issue := scanOrderedDirectory(root, "", &observedDirectory{File: f, err: io.EOF}, before, nil)
	if issue.Code != "" || len(got) != 0 {
		t.Fatalf("empty directory: candidates=%d issue=%#v", len(got), issue)
	}
}
