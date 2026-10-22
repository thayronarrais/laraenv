package gitworkspace

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lara4ever/internal/i18n"
)

const gitTimeout = 15 * time.Second

var workspaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type gitRunner func(context.Context, string, ...string) ([]byte, error)

type Manager struct {
	root string
	run  gitRunner
}

func NewManager(root string) *Manager {
	return &Manager{root: root, run: runGit}
}

func (m *Manager) Inspect(ctx context.Context, projectPath string) RepositoryInfo {
	projectPath = filepath.Clean(projectPath)
	rootOutput, err := m.call(ctx, projectPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return RepositoryInfo{Repository: false, Branches: []Branch{}, Worktrees: []Worktree{}, Reason: i18n.T("backend.gitworkspace.notAGitRepository", nil)}
	}
	root := filepath.Clean(strings.TrimSpace(string(rootOutput)))
	info := RepositoryInfo{Repository: true, Root: root, Branches: []Branch{}, Worktrees: []Worktree{}}
	if output, branchErr := m.call(ctx, root, "branch", "--show-current"); branchErr == nil {
		info.CurrentBranch = strings.TrimSpace(string(output))
	}
	if output, statusErr := m.call(ctx, root, "status", "--porcelain"); statusErr == nil {
		info.Dirty = strings.TrimSpace(string(output)) != ""
	}
	seenBranches := map[string]bool{}
	appendBranches := func(output []byte, remote bool) {
		for _, name := range splitLines(string(output)) {
			if name == "" || strings.HasSuffix(name, "/HEAD") || seenBranches[name] {
				continue
			}
			seenBranches[name] = true
			info.Branches = append(info.Branches, Branch{Name: name, Remote: remote})
		}
	}
	if output, branchesErr := m.call(ctx, root, "for-each-ref", "--format=%(refname:short)", "refs/heads"); branchesErr == nil {
		appendBranches(output, false)
	}
	if output, branchesErr := m.call(ctx, root, "for-each-ref", "--format=%(refname:short)", "refs/remotes"); branchesErr == nil {
		appendBranches(output, true)
	}
	sort.Slice(info.Branches, func(i, j int) bool { return info.Branches[i].Name < info.Branches[j].Name })
	if output, worktreeErr := m.call(ctx, root, "worktree", "list", "--porcelain"); worktreeErr == nil {
		info.Worktrees = parseWorktrees(string(output))
	}
	return info
}

func (m *Manager) Resolve(ctx context.Context, projectPath string, request ResolveRequest) (Resolved, error) {
	projectPath = filepath.Clean(projectPath)
	if _, err := os.Stat(projectPath); err != nil {
		return Resolved{}, i18n.Wrapf(err, "errors.gitworkspace.projectFolderFailed", nil)
	}
	if request.Mode == "" {
		request.Mode = ModeCurrent
	}
	if request.Mode == ModeCurrent {
		info := m.Inspect(ctx, projectPath)
		return Resolved{Path: projectPath, Branch: info.CurrentBranch}, nil
	}

	info := m.Inspect(ctx, projectPath)
	if !info.Repository {
		return Resolved{}, i18n.Errorf("errors.gitworkspace.branchWorktreeRequiresRepo", nil)
	}
	branch := strings.TrimSpace(request.Branch)
	if err := m.validateBranch(ctx, info.Root, branch); err != nil {
		return Resolved{}, err
	}

	switch request.Mode {
	case ModeBranch:
		if info.Dirty {
			return Resolved{}, i18n.Errorf("errors.gitworkspace.mainCheckoutDirty", nil)
		}
		if err := m.switchBranch(ctx, info.Root, request); err != nil {
			return Resolved{}, err
		}
		return Resolved{Path: info.Root, Branch: branch, BaseRef: strings.TrimSpace(request.BaseRef)}, nil
	case ModeWorktree:
		return m.resolveWorktree(ctx, info, request)
	default:
		return Resolved{}, i18n.Errorf("errors.gitworkspace.unknownWorkspaceMode", map[string]any{"mode": request.Mode})
	}
}

// Remove deletes one LaraEnv-managed Git worktree. It deliberately does not
// use --force: uncommitted files must be handled by the user before cleanup.
func (m *Manager) Remove(ctx context.Context, projectPath, worktreePath string) error {
	projectPath = filepath.Clean(projectPath)
	target, err := filepath.Abs(strings.TrimSpace(worktreePath))
	if err != nil {
		return err
	}
	if err := ensureContained(m.root, target); err != nil {
		return err
	}
	info := m.Inspect(ctx, projectPath)
	if !info.Repository {
		return i18n.Errorf("errors.gitworkspace.notAGitRepository", nil)
	}
	if samePath(info.Root, target) {
		return i18n.Errorf("errors.gitworkspace.mainCheckoutCannotBeRemoved", nil)
	}
	registered := false
	for _, worktree := range info.Worktrees {
		if samePath(worktree.Path, target) {
			registered = true
			break
		}
	}
	if !registered {
		return i18n.Errorf("errors.gitworkspace.worktreeNotRegistered", nil)
	}
	if _, err := m.call(ctx, info.Root, "worktree", "remove", target); err != nil {
		return i18n.Wrapf(err, "errors.gitworkspace.removeWorktreeFailed", nil)
	}
	_, _ = m.call(ctx, info.Root, "worktree", "prune")
	return nil
}

func (m *Manager) switchBranch(ctx context.Context, repoRoot string, request ResolveRequest) error {
	branch := strings.TrimSpace(request.Branch)
	if request.CreateBranch {
		base := strings.TrimSpace(request.BaseRef)
		if base == "" {
			return i18n.Errorf("errors.gitworkspace.baseRefRequiredForBranch", nil)
		}
		if m.refExists(ctx, repoRoot, "refs/heads/"+branch) {
			return i18n.Errorf("errors.gitworkspace.localBranchAlreadyExists", map[string]any{"branch": branch})
		}
		if err := m.validateCommit(ctx, repoRoot, base); err != nil {
			return err
		}
		if _, err := m.call(ctx, repoRoot, "switch", "-c", branch, base); err != nil {
			return i18n.Wrapf(err, "errors.gitworkspace.createSwitchBranchFailed", map[string]any{"branch": branch})
		}
		return nil
	}
	if err := m.validateCommit(ctx, repoRoot, "refs/heads/"+branch); err != nil {
		return i18n.Errorf("errors.gitworkspace.localBranchNotFound", map[string]any{"branch": branch})
	}
	if _, err := m.call(ctx, repoRoot, "switch", branch); err != nil {
		return i18n.Wrapf(err, "errors.gitworkspace.switchBranchFailed", map[string]any{"branch": branch})
	}
	return nil
}

func (m *Manager) resolveWorktree(ctx context.Context, info RepositoryInfo, request ResolveRequest) (Resolved, error) {
	name := strings.TrimSpace(request.WorktreeName)
	if !workspaceNamePattern.MatchString(name) || name == "." || name == ".." {
		return Resolved{}, i18n.Errorf("errors.gitworkspace.invalidWorktreeName", nil)
	}
	destination := filepath.Join(m.root, projectKey(info.Root), name)
	if err := ensureContained(m.root, destination); err != nil {
		return Resolved{}, err
	}
	branch := strings.TrimSpace(request.Branch)
	for _, existing := range info.Worktrees {
		if samePath(existing.Path, destination) {
			if _, err := os.Stat(destination); err != nil {
				return Resolved{}, i18n.Wrapf(err, "errors.gitworkspace.registeredWorktreeMissing", nil)
			}
			if branch != "" && existing.Branch != "" && existing.Branch != branch {
				return Resolved{}, i18n.Errorf("errors.gitworkspace.worktreeBranchMismatch", map[string]any{"name": name, "registered": existing.Branch, "requested": branch})
			}
			return Resolved{Path: destination, Branch: existing.Branch, BaseRef: strings.TrimSpace(request.BaseRef), WorktreePath: destination}, nil
		}
	}
	if _, err := os.Stat(destination); err == nil {
		return Resolved{}, i18n.Errorf("errors.gitworkspace.worktreeDestinationUnregistered", nil)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Resolved{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return Resolved{}, err
	}

	var args []string
	if request.CreateBranch {
		base := strings.TrimSpace(request.BaseRef)
		if base == "" {
			return Resolved{}, i18n.Errorf("errors.gitworkspace.baseRefRequiredForWorktreeBranch", nil)
		}
		if m.refExists(ctx, info.Root, "refs/heads/"+branch) {
			return Resolved{}, i18n.Errorf("errors.gitworkspace.localBranchAlreadyExists", map[string]any{"branch": branch})
		}
		if err := m.validateCommit(ctx, info.Root, base); err != nil {
			return Resolved{}, err
		}
		args = []string{"worktree", "add", "-b", branch, destination, base}
	} else {
		if err := m.validateCommit(ctx, info.Root, "refs/heads/"+branch); err != nil {
			return Resolved{}, i18n.Errorf("errors.gitworkspace.localBranchNotFound", map[string]any{"branch": branch})
		}
		args = []string{"worktree", "add", destination, branch}
	}
	if _, err := m.call(ctx, info.Root, args...); err != nil {
		return Resolved{}, i18n.Wrapf(err, "errors.gitworkspace.createWorktreeFailed", map[string]any{"name": name})
	}
	return Resolved{Path: destination, Branch: branch, BaseRef: strings.TrimSpace(request.BaseRef), WorktreePath: destination}, nil
}

func (m *Manager) validateBranch(ctx context.Context, repoRoot, branch string) error {
	if branch == "" {
		return i18n.Errorf("errors.gitworkspace.branchRequired", nil)
	}
	if _, err := m.call(ctx, repoRoot, "check-ref-format", "--branch", branch); err != nil {
		return i18n.Errorf("errors.gitworkspace.invalidBranch", map[string]any{"branch": branch})
	}
	return nil
}

func (m *Manager) validateCommit(ctx context.Context, repoRoot, ref string) error {
	if _, err := m.call(ctx, repoRoot, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}"); err != nil {
		return i18n.Errorf("errors.gitworkspace.baseRefNotACommit", map[string]any{"ref": ref})
	}
	return nil
}

func (m *Manager) refExists(ctx context.Context, repoRoot, ref string) bool {
	_, err := m.call(ctx, repoRoot, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

func (m *Manager) call(ctx context.Context, dir string, args ...string) ([]byte, error) {
	callCtx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	output, err := m.run(callCtx, dir, args...)
	if callCtx.Err() != nil {
		return nil, i18n.Wrapf(callCtx.Err(), "errors.gitworkspace.gitCommandTimedOut", map[string]any{"command": strings.Join(args, " ")})
	}
	return output, err
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = dir
	hideGitWindow(command)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			// Not converted to i18n.Errorf: this is git's own stderr text
			// (dynamic, arbitrary, and possibly in the system's git-CLI
			// locale), not authored prose — there is no fixed English
			// literal to key. See task-18c report for the precedent.
			return nil, errors.New(message)
		}
		return nil, err
	}
	return output, nil
}

func parseWorktrees(output string) []Worktree {
	var result []Worktree
	var current Worktree
	flush := func() {
		if current.Path != "" {
			result = append(result, current)
		}
		current = Worktree{}
	}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if line == "" {
			flush()
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			current.Path = filepath.Clean(value)
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		}
	}
	flush()
	return result
}

func splitLines(output string) []string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	return lines
}

func projectKey(repoRoot string) string {
	name := strings.ToLower(filepath.Base(filepath.Clean(repoRoot)))
	name = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(name, "-")
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(repoRoot))))
	return fmt.Sprintf("%s-%x", strings.Trim(name, "-."), sum[:4])
}

func canonicalWorktreePath(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := canonicalExistingWorktreePath(current)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// An existing dangling link is not an ordinary missing directory.
		if _, statErr := os.Lstat(current); statErr == nil {
			return "", err
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", statErr
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func ensureContained(root, candidate string) error {
	// Resolve existing ancestors even when a worktree has not been created yet.
	rootAbs, err := canonicalWorktreePath(root)
	if err != nil {
		return err
	}
	candidateAbs, err := canonicalWorktreePath(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return i18n.Errorf("errors.gitworkspace.worktreeDestinationEscapesRoot", map[string]any{"destination": candidateAbs, "root": rootAbs})
	}
	return nil
}

func samePath(left, right string) bool {
	if leftInfo, err := os.Stat(left); err == nil {
		if rightInfo, err := os.Stat(right); err == nil {
			return os.SameFile(leftInfo, rightInfo)
		}
	}
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}
