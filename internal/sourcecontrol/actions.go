package sourcecontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
)

func (s *Service) StartStage(ctx context.Context, req FilesRequest) Result {
	return s.startFiles(ctx, req, "stage")
}
func (s *Service) StartUnstage(ctx context.Context, req FilesRequest) Result {
	return s.startFiles(ctx, req, "unstage")
}
func (s *Service) startFiles(ctx context.Context, req FilesRequest, kind string) Result {
	req.Entries = append([]EntrySelection(nil), req.Entries...)
	if len(req.Entries) == 0 {
		return issue("invalid_path", errors.New("Select at least one file"))
	}
	return s.startOperation(ctx, req.Repository, kind, 60*time.Second, func(ctx context.Context, a *authority, emit func(string, string)) (CommandResult, Result) {
		return s.mutateFiles(ctx, a, req, kind, emit)
	})
}

func (s *Service) mutateFiles(ctx context.Context, a *authority, req FilesRequest, kind string, emit func(string, string)) (CommandResult, Result) {
	fail := func(code string, err error) (CommandResult, Result) { return CommandResult{}, issue(code, err) }
	status := s.status(ctx, a)
	if status.Code != "ok" {
		return CommandResult{}, status
	}
	snap := status.Snapshot
	if !matchesExpectation(snap, req.Expected) {
		return fail("stale_snapshot", errors.New("Source control snapshot changed"))
	}
	if snap.HasConflicts || snap.GitOperation != "" {
		return fail("conflict", errors.New("Resolve the current Git operation before changing the index"))
	}
	entries := map[string]Entry{}
	for _, entry := range snap.Entries {
		entries[entry.ID] = entry
	}
	// Index membership identifies already-removed rename sources. An absent source
	// is validated but cannot be given to git add, which rejects unmatched paths.
	index, problem := s.command(ctx, a, "ls-files", "--stage", "-z")
	if problem.Code != "" {
		return index, problem
	}
	if err := validateIndex(index.Stdout); err != nil {
		return fail("unsupported_format", err)
	}
	if digest(string(index.Stdout)) != snap.IndexRevision {
		return fail("stale_snapshot", errors.New("Index changed"))
	}
	indexed := map[string]bool{}
	for _, record := range strings.Split(string(index.Stdout), "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		if ok && strings.HasSuffix(meta, " 0") {
			indexed[path] = true
		}
	}
	var paths []*diskPath
	defer func() {
		for _, path := range paths {
			path.close()
		}
	}()
	var input strings.Builder
	seen := map[string]bool{}
	for _, selection := range req.Entries {
		e, ok := entries[selection.EntryID]
		if !ok {
			return fail("stale_snapshot", errors.New("Entry is no longer in this snapshot"))
		}
		if e.Kind == "gitlink" || e.Kind == "blocked" || e.Kind == "conflict" || unsupportedMode(e.HeadMode) || unsupportedMode(e.IndexMode) || unsupportedMode(e.WorktreeMode) {
			return fail("invalid_path", errors.New("Selected entry cannot be changed safely"))
		}
		if (kind == "stage" && !e.CanStage) || (kind == "unstage" && !e.CanUnstage) {
			return fail("stale_snapshot", errors.New("Selected entry has no applicable changes"))
		}
		for _, name := range []string{e.RelativePath, e.OriginalPath} {
			if name == "" {
				continue
			}
			disk, err := openDiskPath(a.root, name)
			if err != nil {
				return fail("invalid_path", err)
			}
			paths = append(paths, disk)
			if name == e.RelativePath && selection.DiskRevision != "" {
				if !validKey(selection.DiskRevision) {
					return fail("stale_snapshot", errors.New("Invalid reviewed disk revision"))
				}
				h := sha256.New()
				if !disk.missing {
					if _, err := io.Copy(h, &contextReader{ctx: ctx, reader: disk.file}); err != nil {
						return fail("stale_snapshot", err)
					}
				}
				if hex.EncodeToString(h.Sum(nil)) != selection.DiskRevision {
					return fail("stale_snapshot", errors.New("Reviewed disk contents changed"))
				}
			}
			if kind == "stage" && name == e.OriginalPath && disk.missing && !indexed[name] {
				continue
			}
			if kind == "unstage" && snap.Unborn && !indexed[name] {
				continue
			}
			if !seen[name] {
				input.WriteString(name)
				input.WriteByte(0)
				seen[name] = true
			}
		}
	}
	if input.Len() == 0 {
		return fail("stale_snapshot", errors.New("No selected index paths remain"))
	}
	after := s.status(ctx, a)
	if after.Code != "ok" {
		return CommandResult{}, after
	}
	if !matchesExpectation(after.Snapshot, req.Expected) {
		return fail("stale_snapshot", errors.New("Source control changed before dispatch"))
	}
	for _, path := range paths {
		if err := path.validate(); err != nil {
			return fail("stale_snapshot", err)
		}
	}
	if err := s.verify(a); err != nil {
		return fail("repository_changed", err)
	}
	args := []string{"--git-dir=" + a.gitID.CanonicalPath, "--work-tree=" + a.info.CanonicalRoot, "--literal-pathspecs"}
	switch {
	case kind == "stage":
		args = append(args, "add", "-A")
	case snap.Unborn:
		args = append(args, "rm", "--cached", "-f")
	default:
		args = append(args, "restore", "--staged")
	}
	args = append(args, "--pathspec-from-file=-", "--pathspec-file-nul")
	result := s.run(ctx, a.info.CanonicalRoot, args, []byte(input.String()), structuredLimit, emit)
	if err := s.verify(a); err != nil {
		problem := issue("repository_changed", err)
		problem.Snapshot = snap
		return result, problem
	}
	problem = commandIssue(result)
	// This private worker result carries the pre-dispatch snapshot so recovery
	// can report a partial mutation even when cancellation or Git failure occurs.
	problem.Snapshot = snap
	return result, problem
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Commit consumes the complete shared index, including externally staged entries.
func (s *Service) StartCommit(ctx context.Context, req CommitRequest) Result {
	if strings.TrimSpace(req.Message) == "" {
		return issue("invalid_path", errors.New("Enter a commit message"))
	}
	return s.startOperation(ctx, req.Repository, "commit", 120*time.Second, func(ctx context.Context, a *authority, emit func(string, string)) (CommandResult, Result) {
		status := s.status(ctx, a)
		if status.Code != "ok" {
			return CommandResult{}, status
		}
		snap := status.Snapshot
		if !matchesExpectation(snap, req.Expected) {
			return CommandResult{}, issue("stale_snapshot", errors.New("Reviewed HEAD or index changed"))
		}
		if snap.HasConflicts || snap.GitOperation != "" {
			return CommandResult{}, issue("conflict", errors.New("Resolve the current Git operation before committing"))
		}
		staged := false
		for _, entry := range snap.Entries {
			if entry.IndexCode != "." && entry.IndexCode != "?" {
				staged = true
			}
		}
		if !staged {
			return CommandResult{}, issue("invalid_path", errors.New("Stage changes before committing"))
		}
		return s.dispatchReviewed(ctx, a, snap, []string{"commit", "--file=-"}, []byte(req.Message), emit)
	})
}

func (s *Service) StartPush(ctx context.Context, req PushRequest) Result {
	if !validKey(req.RemoteID) || !strings.HasPrefix(req.RemoteBranch, "refs/heads/") {
		return issue("invalid_path", errors.New("Choose a remote and a refs/heads branch"))
	}
	return s.startOperation(ctx, req.Repository, "push", 180*time.Second, func(ctx context.Context, a *authority, emit func(string, string)) (CommandResult, Result) {
		status := s.status(ctx, a)
		if status.Code != "ok" {
			return CommandResult{}, status
		}
		snap := status.Snapshot
		if !matchesExpectation(snap, req.Expected) {
			return CommandResult{}, issue("stale_snapshot", errors.New("Reviewed repository or remote changed"))
		}
		if snap.Detached || snap.Unborn || snap.Branch == "" {
			return CommandResult{}, issue("invalid_path", errors.New("Push requires a current named branch with a commit"))
		}
		if snap.HasConflicts || snap.GitOperation != "" {
			return CommandResult{}, issue("conflict", errors.New("Resolve the current Git operation before pushing"))
		}
		var remote *Remote
		for i := range snap.Remotes {
			if snap.Remotes[i].ID == req.RemoteID {
				remote = &snap.Remotes[i]
				break
			}
		}
		if remote == nil {
			return CommandResult{}, issue("stale_snapshot", errors.New("Reviewed remote changed"))
		}
		if !remote.CanPush {
			return CommandResult{}, issue("invalid_path", errors.New(remote.DisabledReason))
		}
		for _, ref := range []string{"refs/heads/" + snap.Branch, req.RemoteBranch} {
			if _, problem := s.command(ctx, a, "check-ref-format", ref); problem.Code != "" {
				return CommandResult{}, issue("invalid_path", errors.New("Invalid push branch"))
			}
		}
		args := []string{"-c", "remote." + remote.Name + ".mirror=false", "push", "--porcelain", "--progress", "--no-follow-tags", "--no-recurse-submodules"}
		if req.SetUpstream {
			args = append(args, "--set-upstream")
		}
		args = append(args, "--", remote.Name, "refs/heads/"+snap.Branch+":"+req.RemoteBranch)
		native, problem := s.dispatchReviewed(ctx, a, snap, args, nil, emit)
		if problem.Snapshot != nil {
			problem.Operation = &Operation{Remote: remote.Name, RemoteBranch: req.RemoteBranch}
		}
		return native, problem
	})
}

// Re-read immediately before dispatch, since validation itself invokes Git.
// Native Git owns locking/hooks/signing; no private index or retry is used.
func (s *Service) dispatchReviewed(ctx context.Context, a *authority, snap *Snapshot, args []string, input []byte, emit func(string, string)) (CommandResult, Result) {
	after := s.status(ctx, a)
	if after.Code != "ok" {
		return CommandResult{}, after
	}
	expected := Expectation{SnapshotRevision: snap.SnapshotRevision, IndexRevision: snap.IndexRevision, HeadOID: snap.HeadOID, Branch: snap.Branch}
	if !matchesExpectation(after.Snapshot, expected) {
		return CommandResult{}, issue("stale_snapshot", errors.New("Source control changed before dispatch"))
	}
	if err := s.verify(a); err != nil {
		return CommandResult{}, issue("repository_changed", err)
	}
	all := append([]string{"--git-dir=" + a.gitID.CanonicalPath, "--work-tree=" + a.info.CanonicalRoot}, args...)
	native := s.run(ctx, a.info.CanonicalRoot, all, input, structuredLimit, emit)
	problem := commandIssue(native)
	if err := s.verify(a); err != nil {
		problem = issue("repository_changed", err)
	}
	problem.Snapshot = snap
	return native, problem
}
