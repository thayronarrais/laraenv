package workspacefiles

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
)

func (s *Service) List(ref ContextRef, key, relative, cursor string) Result {
	root, _, issue := s.openContext(ref, key)
	if issue.Code != "" {
		return issue
	}
	defer root.Close()
	if key == "" {
		return problem("invalid_path", errors.New("root key required"))
	}
	dirInfo, issue := inspectPath(root, relative, true)
	if issue.Code != "" {
		return issue
	}
	name := relative
	if name == "" {
		name = "."
	}
	state := pageCursor{Version: cursorVersion, RootKey: key, RelativePath: relative, Modified: dirInfo.ModTime().UnixNano()}
	f, err := root.Open(name)
	if err != nil {
		return problem("unavailable", err)
	}
	defer f.Close()
	_, state.Identity, err = handleIdentity(f)
	if err != nil {
		return problem("io_error", err)
	}
	opened, err := f.Stat()
	if err != nil {
		return problem("io_error", err)
	}
	if !os.SameFile(dirInfo, opened) || !dirInfo.ModTime().Equal(opened.ModTime()) {
		return problem("invalid_path", staleDirectoryError("directory changed during open"))
	}
	var after *orderKey
	if cursor != "" {
		requested, err := decodePageCursor(cursor, state)
		if err != nil {
			return problem("invalid_path", err)
		}
		after = &requested
	}
	candidates, issue := scanOrderedDirectory(root, relative, f, opened, after)
	if issue.Code != "" {
		return issue
	}
	// Re-resolve authority and the path after the scan: a still-open handle
	// alone cannot prove the directory is still the context's current path.
	freshRoot, _, issue := s.openContext(ref, key)
	if issue.Code != "" {
		return issue
	}
	defer freshRoot.Close()
	current, issue := inspectPath(freshRoot, relative, true)
	if issue.Code != "" {
		if issue.Code == "permission_denied" || issue.Code == "io_error" {
			return issue
		}
		return problem("invalid_path", staleDirectoryError("directory changed during scan"))
	}
	fresh, err := freshRoot.Open(name)
	if err != nil {
		if os.IsPermission(err) {
			return problem("permission_denied", err)
		}
		return problem("invalid_path", staleDirectoryError("directory changed during scan"))
	}
	defer fresh.Close()
	_, identity, err := handleIdentity(fresh)
	if err != nil {
		return problem("io_error", err)
	}
	freshInfo, err := fresh.Stat()
	if err != nil {
		return problem("io_error", err)
	}
	if identity != state.Identity || !os.SameFile(opened, current) || current.ModTime().UnixNano() != state.Modified || freshInfo.ModTime().UnixNano() != state.Modified {
		return problem("invalid_path", staleDirectoryError("directory changed during scan"))
	}
	hasNext := len(candidates) > pageLimit
	if hasNext {
		candidates = candidates[:pageLimit]
	}
	page := &DirectoryPage{Entries: make([]DirectoryEntry, 0, len(candidates))}
	for _, candidate := range candidates {
		page.Entries = append(page.Entries, candidate.entry)
	}
	if hasNext {
		last := candidates[len(candidates)-1].key
		state.LastKind, state.LastFoldedName, state.LastName = last.kind, last.foldedName, last.name
		data, _ := json.Marshal(state)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return Result{Code: "ok", Directory: page}
}

func (s *Service) readBytes(ref ContextRef, key, relative string, limit int64) ([]byte, string, Result) {
	root, info, issue := s.openContext(ref, key)
	if issue.Code != "" {
		return nil, "", issue
	}
	defer root.Close()
	if key == "" {
		return nil, "", problem("invalid_path", errors.New("root key required"))
	}
	st, issue := inspectPath(root, relative, false)
	if issue.Code != "" {
		return nil, "", issue
	}
	if st.Size() > limit {
		return nil, "", problem("too_large", errors.New("file exceeds size limit"))
	}
	f, err := root.Open(relative)
	if err != nil {
		return nil, "", problem("unavailable", err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, "", problem("io_error", err)
	}
	if !opened.Mode().IsRegular() {
		return nil, "", problem("unsupported_format", errors.New("not a regular file"))
	}
	canonical, err := openedCanonical(f, canonicalFile(info, relative))
	if err != nil {
		return nil, "", problem("invalid_path", err)
	}
	data, issue := readBounded(f, limit)
	if issue.Code != "" {
		return nil, "", issue
	}
	return data, canonical, Result{}
}

// readBounded enforces the limit against bytes actually read. The caller's
// earlier stat is only a fast rejection; the file may grow before this read.
func readBounded(f *os.File, limit int64) ([]byte, Result) {
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, problem("io_error", err)
	}
	if int64(len(data)) > limit {
		return nil, problem("too_large", errors.New("file exceeds size limit"))
	}
	return data, Result{}
}

func (s *Service) Read(ref ContextRef, key, relative string) Result {
	data, canonical, issue := s.readBytes(ref, key, relative, 2<<20)
	if issue.Code != "" {
		return issue
	}
	snapshot, err := decodeText(data, canonical)
	if err != nil {
		return problem("unsupported_format", err)
	}
	return Result{Code: "ok", Document: snapshot}
}

func (s *Service) Check(ref ContextRef, key, relative, revision string) Result {
	current := s.Read(ref, key, relative)
	if current.Code != "ok" {
		return current
	}
	if current.Document.Revision == revision {
		return Result{Code: "unchanged"}
	}
	current.Code = "changed"
	return current
}

// ResolveFile revalidates an existing regular file for a trusted caller such
// as the external-editor adapter. It never accepts a caller-supplied root.
func (s *Service) ResolveFile(ref ContextRef, key, relative string) (string, Result) {
	root, info, issue := s.openContext(ref, key)
	if issue.Code != "" {
		return "", issue
	}
	defer root.Close()
	if key == "" {
		return "", problem("invalid_path", errors.New("root key required"))
	}
	_, issue = inspectPath(root, relative, false)
	if issue.Code != "" {
		return "", issue
	}
	f, err := root.Open(relative)
	if err != nil {
		return "", problem("unavailable", err)
	}
	defer f.Close()
	canonical, err := openedCanonical(f, canonicalFile(info, relative))
	if err != nil {
		return "", problem("invalid_path", err)
	}
	return canonical, Result{Code: "ok"}
}
