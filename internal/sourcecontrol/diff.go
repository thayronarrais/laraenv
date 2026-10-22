package sourcecontrol

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

const diffTextLimit int64 = 2 << 20

func matchesExpectation(snap *Snapshot, expected Expectation) bool {
	return expected.SnapshotRevision != "" && expected.IndexRevision != "" && snap.SnapshotRevision == expected.SnapshotRevision && snap.IndexRevision == expected.IndexRevision && snap.HeadOID == expected.HeadOID && snap.Branch == expected.Branch
}

// ReadDiff resolves membership from fresh status. The request never supplies a
// Git revision expression, blob path, or absolute disk path.
func (s *Service) ReadDiff(ctx context.Context, req DiffRequest) Result {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	if req.Layer != "index" && req.Layer != "worktree" {
		return issue("invalid_path", errors.New("Invalid comparison layer"))
	}
	a, problem := s.discover(ctx, req.Repository.Context, &req.Repository)
	if problem.Code != "" {
		return problem
	}
	defer a.close()
	status := s.status(ctx, a)
	if status.Code != "ok" {
		return status
	}
	if !matchesExpectation(status.Snapshot, req.Expected) {
		return issue("stale_snapshot", errors.New("Source control snapshot changed"))
	}
	var selected *Entry
	for i := range status.Snapshot.Entries {
		if status.Snapshot.Entries[i].ID == req.EntryID {
			selected = &status.Snapshot.Entries[i]
			break
		}
	}
	if selected == nil {
		return issue("stale_snapshot", errors.New("Entry is no longer in this snapshot"))
	}
	e := *selected
	d := &Diff{Repository: a.ref, EntryID: e.ID, RelativePath: e.RelativePath, OriginalPath: e.OriginalPath, Layer: req.Layer, Kind: "text"}
	var disk *diskPath
	var diskSide DiffSide
	diskKind := "unsupported"
	if e.Kind == "gitlink" {
		d.Kind = "gitlink"
	} else if e.Kind == "blocked" || e.Kind == "conflict" || unsupportedMode(e.HeadMode) || unsupportedMode(e.IndexMode) || unsupportedMode(e.WorktreeMode) {
		d.Kind = "unsupported"
	} else {
		var err error
		disk, err = openDiskPath(a.root, e.RelativePath)
		if err != nil {
			if os.IsPermission(err) {
				return issue("permission_denied", err)
			}
			d.Kind = "unsupported"
		} else {
			defer disk.close()
			diskSide, diskKind, err = disk.read()
			if err != nil {
				return issue("stale_snapshot", err)
			}
			d.CanOpenWorktree = !disk.missing && diskKind == "text"
		}
	}
	if d.Kind == "text" {
		originalOID, originalMode := e.HeadOID, e.HeadMode
		if req.Layer == "worktree" {
			originalOID, originalMode = e.IndexOID, e.IndexMode
		}
		original, kind, problem := s.blobSide(ctx, a, originalOID, originalMode)
		if problem.Code != "" {
			return problem
		}
		d.Original = original
		d.Kind = kind
		modified, modifiedKind := diskSide, diskKind
		if req.Layer == "index" {
			modified, modifiedKind, problem = s.blobSide(ctx, a, e.IndexOID, e.IndexMode)
			if problem.Code != "" {
				return problem
			}
		}
		d.Modified = modified
		if modifiedKind != "text" {
			d.Kind = modifiedKind
		}
	}
	// Recheck both Git state and retained physical parents after all reads.
	after := s.status(ctx, a)
	if after.Code != "ok" {
		return after
	}
	if !matchesExpectation(after.Snapshot, req.Expected) {
		return issue("stale_snapshot", errors.New("Source control snapshot changed during comparison"))
	}
	if disk != nil {
		if err := disk.validate(); err != nil {
			return issue("stale_snapshot", err)
		}
	}
	if d.Kind != "text" {
		d.Original.Text, d.Modified.Text = "", ""
		switch d.Kind {
		case "binary":
			d.Message = "Binary or invalid UTF-8 content cannot be shown as text"
		case "too_large":
			d.Message = "A comparison side exceeds the 2 MiB text limit"
		case "gitlink":
			d.Message = "Submodule comparisons are unsupported"
		default:
			d.Message = "This path or text format cannot be compared safely"
		}
	}
	d.ID = digest(a.ref.RootKey, a.ref.RepositoryKey, e.ID, req.Layer, req.Expected.SnapshotRevision, d.Original.Revision, d.Modified.Revision, d.Kind)
	r := Result{Code: "ok", Diff: d}
	encoded, err := json.Marshal(r)
	if err != nil {
		return issue("unsupported_format", err)
	}
	if int64(len(encoded)) > structuredLimit {
		return issue("too_large", errors.New("Comparison exceeds the structured output limit"))
	}
	return r
}

func unsupportedMode(mode string) bool {
	return mode != "" && mode != "000000" && mode != "100644" && mode != "100755"
}

func (s *Service) blobSide(ctx context.Context, a *authority, oid, mode string) (DiffSide, string, Result) {
	if mode == "" || mode == "000000" {
		side, kind := decodeDiffText(nil)
		return side, kind, Result{}
	}
	if !validOID(oid) || strings.Trim(oid, "0") == "" {
		return DiffSide{}, "", issue("unsupported_format", errors.New("Invalid comparison blob"))
	}
	r, problem := s.command(ctx, a, "cat-file", "-s", oid)
	if problem.Code != "" {
		return DiffSide{}, "", problem
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(r.Stdout)), 10, 64)
	if err != nil || size < 0 {
		return DiffSide{}, "", issue("unsupported_format", errors.New("Invalid blob size"))
	}
	side := DiffSide{OID: oid, Revision: oid, Size: size}
	if size > diffTextLimit {
		return side, "too_large", Result{}
	}
	// Raw cat-file never invokes external diff, textconv or smudge filters.
	if err := s.verify(a); err != nil {
		return DiffSide{}, "", issue("repository_changed", err)
	}
	args := []string{"--git-dir=" + a.gitID.CanonicalPath, "--work-tree=" + a.info.CanonicalRoot, "cat-file", "blob", oid}
	r = s.run(ctx, a.info.CanonicalRoot, args, nil, diffTextLimit+1, nil)
	if err := s.verify(a); err != nil {
		return DiffSide{}, "", issue("repository_changed", err)
	}
	if r.Overflow || int64(len(r.Stdout)) > diffTextLimit {
		return side, "too_large", Result{}
	}
	if problem := commandIssue(r); problem.Code != "" {
		return DiffSide{}, "", problem
	}
	if int64(len(r.Stdout)) != size {
		return DiffSide{}, "", issue("unsupported_format", errors.New("Incomplete blob content"))
	}
	decoded, kind := decodeDiffText(r.Stdout)
	decoded.OID, decoded.Revision = oid, oid
	return decoded, kind, Result{}
}

func decodeDiffText(data []byte) (DiffSide, string) {
	sum := sha256.Sum256(data)
	side := DiffSide{Revision: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		side.BOM = true
		data = data[3:]
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return side, "binary"
	}
	text := string(data)
	crlf := strings.Count(text, "\r\n")
	other := strings.ReplaceAll(text, "\r\n", "")
	if strings.Contains(other, "\r") {
		return side, "unsupported"
	}
	switch {
	case crlf > 0 && strings.Contains(other, "\n"):
		side.EOL = "mixed"
	case crlf > 0:
		side.EOL = "crlf"
	default:
		side.EOL = "lf"
	}
	side.Text = strings.ReplaceAll(text, "\r\n", "\n")
	return side, "text"
}
