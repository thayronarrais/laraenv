package sourcecontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"lara4ever/internal/workspacefiles"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Service struct {
	open             WorkspaceOpener
	emit             OperationEmitter
	run              commandRunner
	mu               sync.Mutex
	operations       map[string]*operationRecord
	active           map[string]string
	sequence         uint64
	lifetime         context.Context
	stop             context.CancelFunc
	jobs             sync.WaitGroup
	shutdownReserved bool
	closed           bool
}

func NewService(open WorkspaceOpener, emit OperationEmitter) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{open: open, emit: emit, run: run, operations: make(map[string]*operationRecord), active: make(map[string]string), lifetime: ctx, stop: cancel}
}

// authority retains the physical objects throughout a request. All external Git
// paths originate in discovery from the trusted context, never in a frontend DTO.
type authority struct {
	root                              *os.Root
	marker                            *os.File
	info                              workspacefiles.RootInfo
	ref                               RepositoryRef
	git, common                       *os.Root
	gitID, commonID                   workspacefiles.DirectoryIdentity
	markerID, markerTarget, indexPath string
}

func (a *authority) close() {
	if a.marker != nil {
		a.marker.Close()
	}
	if a.git != nil {
		a.git.Close()
	}
	if a.common != nil {
		a.common.Close()
	}
	if a.root != nil {
		a.root.Close()
	}
}
func digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:", len(p))
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}
func validKey(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func issue(code string, err error) Result {
	message := ""
	if err != nil {
		message = redact(err.Error())
	}
	if len(message) > consoleLimit {
		message = message[:consoleLimit]
	}
	if os.IsPermission(err) {
		code = "permission_denied"
	}
	return Result{Code: code, Message: message}
}
func commandIssue(r CommandResult) Result {
	if r.Overflow {
		return issue("too_large", errors.New("Git response exceeds the structured output limit"))
	}
	if errors.Is(r.Err, exec.ErrNotFound) {
		return issue("git_missing", r.Err)
	}
	if r.Err != nil || r.ExitCode != 0 || r.Cancelled || r.TimedOut {
		message := r.Stderr
		if message == "" {
			message = "Git command failed"
		}
		return issue("git_error", errors.New(message))
	}
	return Result{}
}
func markerIdentity(root *os.Root) (*os.File, string, string, error) {
	f, err := root.Open(".git")
	if err != nil {
		return nil, "", "", err
	}
	_, id, err := workspacefiles.OpenedFileIdentity(f)
	if err != nil {
		f.Close()
		return nil, "", "", err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, "", "", err
	}
	target := "directory"
	if !info.IsDir() {
		if !info.Mode().IsRegular() {
			f.Close()
			return nil, "", "", errors.New("unsupported Git marker")
		}
		data, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil || len(data) > 65536 {
			f.Close()
			return nil, "", "", errors.New("invalid Git marker")
		}
		target = string(data)
	}
	return f, id, target, nil
}
func (s *Service) discover(ctx context.Context, source workspacefiles.ContextRef, expected *RepositoryRef) (a *authority, result Result) {
	if s == nil || s.open == nil {
		return nil, issue("unavailable", errors.New("workspace source unavailable"))
	}
	key := ""
	if expected != nil {
		if !validKey(expected.RootKey) || !validKey(expected.RepositoryKey) {
			return nil, issue("invalid_path", errors.New("invalid repository reference"))
		}
		key = expected.RootKey
	}
	root, info, r := s.open(source, key)
	if r.Code != "" && r.Code != "ok" {
		code := r.Code
		if expected != nil && code == "unavailable" {
			code = "repository_changed"
		}
		return nil, issue(code, errors.New(r.Message))
	}
	a = &authority{root: root, info: *info}
	success := false
	defer func() {
		if !success {
			a.close()
			a = nil
		}
	}()
	a.marker, a.markerID, a.markerTarget, _ = markerIdentity(root)
	if a.marker == nil {
		code := "unavailable"
		if expected != nil {
			code = "repository_changed"
		}
		return a, issue(code, errors.New("context has no Git marker"))
	}
	query := func(args ...string) (string, Result) {
		r := s.run(ctx, info.CanonicalRoot, args, nil, structuredLimit, nil)
		if problem := commandIssue(r); problem.Code != "" {
			return "", problem
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(r.Stdout), "\n"), "\r"), Result{}
	}
	top, problem := query("rev-parse", "--show-toplevel")
	if problem.Code != "" {
		return a, problem
	}
	topRoot, topID, err := workspacefiles.OpenDirectoryIdentity(top)
	if err != nil {
		return a, issue("repository_changed", err)
	}
	topRoot.Close()
	ownRoot, ownID, err := workspacefiles.OpenDirectoryIdentity(info.CanonicalRoot)
	if err != nil {
		return a, issue("repository_changed", err)
	}
	ownRoot.Close()
	if topID != ownID {
		return a, issue("unavailable", errors.New("workspace root must equal repository root"))
	}
	super, problem := query("rev-parse", "--show-superproject-working-tree")
	if problem.Code != "" {
		return a, problem
	}
	if super != "" {
		return a, issue("unavailable", errors.New("submodule contexts are unsupported"))
	}
	gitPath, problem := query("rev-parse", "--absolute-git-dir")
	if problem.Code != "" {
		return a, problem
	}
	commonPath, problem := query("rev-parse", "--path-format=absolute", "--git-common-dir")
	if problem.Code != "" {
		return a, problem
	}
	a.git, a.gitID, err = workspacefiles.OpenDirectoryIdentity(gitPath)
	if err != nil {
		return a, issue("repository_changed", err)
	}
	a.common, a.commonID, err = workspacefiles.OpenDirectoryIdentity(commonPath)
	if err != nil {
		return a, issue("repository_changed", err)
	}
	indexPath, problem := query("rev-parse", "--path-format=absolute", "--git-path", "index")
	if problem.Code != "" {
		return a, problem
	}
	if !filepath.IsAbs(indexPath) {
		return a, issue("unsupported_format", errors.New("Git returned a relative index path"))
	}
	a.indexPath = filepath.Clean(indexPath)
	a.ref = RepositoryRef{Context: source, RootKey: info.RootKey, RepositoryKey: digest(info.RootKey, a.gitID.CanonicalPath, a.gitID.PhysicalID, a.commonID.CanonicalPath, a.commonID.PhysicalID, a.markerID, a.markerTarget, a.indexPath)}
	if expected != nil && a.ref.RepositoryKey != expected.RepositoryKey {
		return a, issue("repository_changed", errors.New("repository identity changed"))
	}
	if err = s.verify(a); err != nil {
		return a, issue("repository_changed", err)
	}
	success = true
	return a, Result{}
}
func (s *Service) verify(a *authority) error {
	r, info, problem := s.open(a.ref.Context, a.info.RootKey)
	if problem.Code != "" && problem.Code != "ok" {
		return errors.New("workspace root changed")
	}
	defer r.Close()
	if info.RootKey != a.info.RootKey {
		return errors.New("workspace root changed")
	}
	marker, id, target, err := markerIdentity(r)
	if err != nil {
		return err
	}
	marker.Close()
	if id != a.markerID || target != a.markerTarget {
		return errors.New("Git marker changed")
	}
	for _, expected := range []workspacefiles.DirectoryIdentity{a.gitID, a.commonID} {
		root, id, err := workspacefiles.OpenDirectoryIdentity(expected.CanonicalPath)
		if err != nil {
			return err
		}
		root.Close()
		if id != expected {
			return errors.New("Git directory changed")
		}
	}
	return nil
}
func (s *Service) command(ctx context.Context, a *authority, args ...string) (CommandResult, Result) {
	if err := s.verify(a); err != nil {
		return CommandResult{}, issue("repository_changed", err)
	}
	all := append([]string{"--git-dir=" + a.gitID.CanonicalPath, "--work-tree=" + a.info.CanonicalRoot}, args...)
	r := s.run(ctx, a.info.CanonicalRoot, all, nil, structuredLimit, nil)
	if err := s.verify(a); err != nil {
		return r, issue("repository_changed", err)
	}
	return r, commandIssue(r)
}
func (s *Service) Inspect(ctx context.Context, source workspacefiles.ContextRef) Result {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	a, r := s.discover(ctx, source, nil)
	if r.Code != "" {
		return r
	}
	defer a.close()
	return s.status(ctx, a)
}
func (s *Service) Status(ctx context.Context, ref RepositoryRef) Result {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	a, r := s.discover(ctx, ref.Context, &ref)
	if r.Code != "" {
		return r
	}
	defer a.close()
	return s.status(ctx, a)
}
