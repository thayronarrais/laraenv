package workspacefiles

import (
	"errors"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"lara4ever/internal/atomicfile"
)

// All Service instances share locks: two validated sources may name the same
// canonical file. Entries disappear when the last holder/waiter leaves.
var documentSaves = struct {
	sync.Mutex
	paths map[string]*saveLock
}{paths: make(map[string]*saveLock)}

type saveLock struct {
	sync.Mutex
	users int
}

func lockDocument(path string) func() {
	documentSaves.Lock()
	entry := documentSaves.paths[path]
	if entry == nil {
		entry = &saveLock{}
		documentSaves.paths[path] = entry
	}
	entry.users++
	documentSaves.Unlock()
	entry.Lock()
	return func() {
		entry.Unlock()
		documentSaves.Lock()
		entry.users--
		if entry.users == 0 {
			delete(documentSaves.paths, path)
		}
		documentSaves.Unlock()
	}
}

type saveProblem struct{ result Result }

func (e *saveProblem) Error() string { return e.result.Message }

// Save accepts editor text and a raw-byte revision. Text metadata is always
// reconstructed from the matching disk snapshot, never from frontend claims.
func (s *Service) Save(ref ContextRef, key, relative, content, expectedRevision string) Result {
	if key == "" || !validRelative(relative, false) {
		return problem("invalid_path", errors.New("root key and valid relative path required"))
	}
	root, context, issue := s.openContext(ref, key)
	if issue.Code != "" {
		return issue
	}
	defer root.Close()
	canonical := canonicalFile(context, relative)
	unlock := lockDocument(canonical)
	defer unlock()
	// Revalidate authority after waiting for another save to finish.
	fresh, _, issue := s.openContext(ref, key)
	if issue.Code != "" {
		return issue
	}
	fresh.Close()
	_, issue = inspectPath(root, relative, false)
	if issue.Code != "" {
		return issue
	}
	file, err := root.Open(relative)
	if err != nil {
		return problem("unavailable", err)
	}
	defer file.Close()
	originalInfo, err := file.Stat()
	if err != nil {
		return problem("io_error", err)
	}
	if !originalInfo.Mode().IsRegular() {
		return problem("unsupported_format", errors.New("not a regular file"))
	}
	canonical, err = openedCanonical(file, canonical)
	if err != nil {
		return problem("invalid_path", err)
	}
	raw, issue := readBounded(file, 2<<20)
	if issue.Code != "" {
		return issue
	}
	original, err := decodeText(raw, canonical)
	if err != nil {
		return problem("unsupported_format", err)
	}
	if original.Revision != expectedRevision {
		return Result{Code: "conflict", Message: "file changed on disk", Document: original}
	}
	if original.ReadOnly {
		return problem("unsupported_format", errors.New("mixed line endings are read-only"))
	}
	if originalInfo.Mode().Perm()&0222 == 0 {
		return problem("permission_denied", os.ErrPermission)
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return problem("unsupported_format", errors.New("content is not UTF-8 text"))
	}
	// Monaco may hand us CRLF; normalize supported line endings before applying
	// the disk's convention. Bare CR remains unsupported.
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if strings.ContainsRune(content, '\r') {
		return problem("unsupported_format", errors.New("unsupported line ending"))
	}
	if len(content) > 2<<20 {
		return problem("too_large", errors.New("file exceeds size limit"))
	}
	if original.EOL == "crlf" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
	if original.BOM {
		content = "\xef\xbb\xbf" + content
	}
	data := []byte(content)
	if len(data) > 2<<20 {
		return problem("too_large", errors.New("file exceeds size limit"))
	}
	err = atomicfile.ReplaceInRoot(root, relative, data, func(parent *os.Root, leaf string) error {
		checkRoot, _, issue := s.openContext(ref, key)
		if issue.Code != "" {
			return &saveProblem{issue}
		}
		checkRoot.Close()
		st, issue := inspectPath(parent, leaf, false)
		if issue.Code != "" {
			return &saveProblem{issue}
		}
		current, err := parent.Open(leaf)
		if err != nil {
			return err
		}
		defer current.Close()
		opened, err := current.Stat()
		if err != nil {
			return err
		}
		if !opened.Mode().IsRegular() || !os.SameFile(st, opened) {
			return atomicfile.ErrChanged
		}
		bytes, issue := readBounded(current, 2<<20)
		if issue.Code != "" {
			return &saveProblem{issue}
		}
		latest, err := decodeText(bytes, canonical)
		if err != nil {
			return &saveProblem{problem("unsupported_format", err)}
		}
		if !os.SameFile(originalInfo, opened) || latest.Revision != expectedRevision {
			return &saveProblem{Result{Code: "conflict", Message: "file changed on disk", Document: latest}}
		}
		return nil
	})
	if err != nil {
		var detail *saveProblem
		if errors.As(err, &detail) {
			return detail.result
		}
		if errors.Is(err, os.ErrPermission) {
			return problem("permission_denied", err)
		}
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, atomicfile.ErrChanged) {
			return problem("unavailable", err)
		}
		return problem("io_error", err)
	}
	saved, err := decodeText(data, canonical)
	if err != nil {
		return problem("io_error", err)
	}
	return Result{Code: "ok", Document: saved}
}
