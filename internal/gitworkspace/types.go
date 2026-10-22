package gitworkspace

type Mode string

const (
	ModeCurrent  Mode = "current"
	ModeBranch   Mode = "branch"
	ModeWorktree Mode = "worktree"
)

type ResolveRequest struct {
	Mode         Mode
	Branch       string
	CreateBranch bool
	BaseRef      string
	WorktreeName string
}

type Resolved struct {
	Path         string `json:"path"`
	Branch       string `json:"branch,omitempty"`
	BaseRef      string `json:"baseRef,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
}

type Branch struct {
	Name   string `json:"name"`
	Remote bool   `json:"remote"`
}

type Worktree struct {
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
}

type RepositoryInfo struct {
	Repository    bool       `json:"repository"`
	Root          string     `json:"root,omitempty"`
	CurrentBranch string     `json:"currentBranch,omitempty"`
	Dirty         bool       `json:"dirty"`
	Branches      []Branch   `json:"branches"`
	Worktrees     []Worktree `json:"worktrees"`
	Reason        string     `json:"reason,omitempty"`
}
