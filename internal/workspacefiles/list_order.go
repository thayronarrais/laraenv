package workspacefiles

import (
	"bytes"
	"container/heap"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const (
	pageLimit     = 500
	listReadBatch = 256
	cursorVersion = 2
)

type pageCursor struct {
	Version        int    `json:"version"`
	RootKey        string `json:"rootKey"`
	RelativePath   string `json:"relativePath"`
	Identity       string `json:"identity"`
	Modified       int64  `json:"modified"`
	LastKind       string `json:"lastKind"`
	LastFoldedName string `json:"lastFoldedName"`
	LastName       string `json:"lastName"`
}

// staleDirectoryError identifies a valid listing that can no longer be
// continued. Malformed cursors and unsafe paths must not use this condition.
type staleDirectoryError string

func (e staleDirectoryError) Error() string { return string(e) }

type orderKey struct {
	kind, foldedName, name string
}

func entryOrderKey(entry DirectoryEntry) orderKey {
	kind := "file"
	if entry.Kind == "directory" {
		kind = "directory"
	}
	return orderKey{kind: kind, foldedName: strings.ToLower(entry.Name), name: entry.Name}
}

func (a orderKey) less(b orderKey) bool {
	if a.kind != b.kind {
		return a.kind == "directory"
	}
	if a.foldedName != b.foldedName {
		return a.foldedName < b.foldedName
	}
	return a.name < b.name
}

func decodePageCursor(encoded string, current pageCursor) (orderKey, error) {
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return orderKey{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return orderKey{}, err
	}
	if len(fields) != 8 {
		return orderKey{}, errors.New("invalid directory cursor keys")
	}
	for _, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return orderKey{}, errors.New("invalid directory cursor value")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var requested pageCursor
	if err := decoder.Decode(&requested); err != nil {
		return orderKey{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return orderKey{}, errors.New("invalid directory cursor")
	}
	if requested.Version < 1 || requested.RootKey == "" || requested.Identity == "" || !validRelative(requested.RelativePath, true) ||
		(requested.LastKind != "directory" && requested.LastKind != "file") || requested.LastName == "" || requested.LastName == "." || requested.LastName == ".." || strings.Contains(requested.LastName, "/") ||
		requested.LastFoldedName != strings.ToLower(requested.LastName) {
		return orderKey{}, errors.New("invalid directory cursor")
	}
	if requested.Version != cursorVersion || requested.RootKey != current.RootKey || requested.RelativePath != current.RelativePath || requested.Identity != current.Identity || requested.Modified != current.Modified {
		return orderKey{}, staleDirectoryError("stale directory cursor")
	}
	// Blocked entries may have names rejected by validRelative. A continuation
	// names only an ordering boundary; it is never used to open a path.
	return orderKey{kind: requested.LastKind, foldedName: requested.LastFoldedName, name: requested.LastName}, nil
}

type orderedCandidate struct {
	entry DirectoryEntry
	key   orderKey
}

// Keep the largest retained key at the root so each smaller candidate replaces
// it without ever retaining more than one page and its lookahead entry.
type candidateHeap []orderedCandidate

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[j].key.less(h[i].key) }
func (h candidateHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *candidateHeap) Push(value any)    { *h = append(*h, value.(orderedCandidate)) }
func (h *candidateHeap) Pop() any {
	last := len(*h) - 1
	value := (*h)[last]
	*h = (*h)[:last]
	return value
}

func (h *candidateHeap) offer(candidate orderedCandidate) {
	if len(*h) < pageLimit+1 {
		heap.Push(h, candidate)
	} else if candidate.key.less((*h)[0].key) {
		(*h)[0] = candidate
		heap.Fix(h, 0)
	}
}

func (h candidateHeap) ordered() []orderedCandidate {
	sort.Slice(h, func(i, j int) bool { return h[i].key.less(h[j].key) })
	return h
}

type directoryReader interface {
	ReadDir(int) ([]os.DirEntry, error)
	Stat() (os.FileInfo, error)
}

func scanOrderedDirectory(root *os.Root, relative string, reader directoryReader, before os.FileInfo, after *orderKey) ([]orderedCandidate, Result) {
	var candidates candidateHeap
	for {
		batch, err := reader.ReadDir(listReadBatch)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, problem("io_error", err)
		}
		for _, entry := range batch {
			entryPath := path.Join(relative, entry.Name())
			kind := "blocked"
			if validRelative(entryPath, false) {
				if st, statErr := root.Lstat(entryPath); statErr == nil && !linked(st) {
					switch {
					case st.IsDir():
						kind = "directory"
					case st.Mode().IsRegular():
						kind = "file"
					}
				}
			}
			candidate := orderedCandidate{entry: DirectoryEntry{Name: entry.Name(), RelativePath: entryPath, Kind: kind}}
			candidate.key = entryOrderKey(candidate.entry)
			if after == nil || after.less(candidate.key) {
				candidates.offer(candidate)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if len(batch) == 0 {
			return nil, problem("io_error", io.ErrNoProgress)
		}
	}
	current, err := reader.Stat()
	if err != nil {
		return nil, problem("io_error", err)
	}
	if !os.SameFile(before, current) || !before.ModTime().Equal(current.ModTime()) {
		return nil, problem("invalid_path", staleDirectoryError("directory changed during scan"))
	}
	return candidates.ordered(), Result{}
}
