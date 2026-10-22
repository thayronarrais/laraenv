package agents

import "time"

type AgentID string

const (
	AgentClaude   AgentID = "claude"
	AgentCodex    AgentID = "codex"
	AgentKimi     AgentID = "kimi"
	AgentOpenCode AgentID = "opencode"
)

type AgentCapabilities struct {
	Launch                bool `json:"launch"`
	RichStatus            bool `json:"richStatus"`
	Resume                bool `json:"resume"`
	History               bool `json:"history"`
	Usage                 bool `json:"usage"`
	AdditionalDirectories bool `json:"additionalDirectories"`
}

type AgentProfile struct {
	ID           AgentID           `json:"id"`
	Label        string            `json:"label"`
	Executable   string            `json:"executable,omitempty"`
	Available    bool              `json:"available"`
	Version      string            `json:"version,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Capabilities AgentCapabilities `json:"capabilities"`
}

type LaunchMode string

const (
	LaunchCurrent  LaunchMode = "current"
	LaunchBranch   LaunchMode = "branch"
	LaunchWorktree LaunchMode = "worktree"
)

type AgentLaunchOptions struct {
	ProjectName           string     `json:"projectName"`
	AgentID               AgentID    `json:"agentId"`
	Title                 string     `json:"title,omitempty"`
	Mode                  LaunchMode `json:"mode"`
	Branch                string     `json:"branch,omitempty"`
	CreateBranch          bool       `json:"createBranch,omitempty"`
	BaseRef               string     `json:"baseRef,omitempty"`
	WorktreeName          string     `json:"worktreeName,omitempty"`
	AdditionalDirectories []string   `json:"additionalDirectories,omitempty"`
}

type SessionState string

const (
	SessionStarting   SessionState = "starting"
	SessionWorking    SessionState = "working"
	SessionNeedsInput SessionState = "needs-input"
	SessionDone       SessionState = "done"
	SessionIdle       SessionState = "idle"
	SessionStopped    SessionState = "stopped"
	SessionError      SessionState = "error"
)

// AgentSession contains only LaraEnv-owned metadata. Provider prompts,
// transcripts, credentials and environment variables never belong here.
type AgentSession struct {
	ID                    string       `json:"id"`
	ProjectName           string       `json:"projectName"`
	ProjectPath           string       `json:"projectPath"`
	ProjectKey            string       `json:"projectKey"`
	AdditionalDirectories []string     `json:"additionalDirectories,omitempty"`
	AgentID               AgentID      `json:"agentId"`
	LaunchMode            LaunchMode   `json:"launchMode"`
	GitBranch             string       `json:"gitBranch,omitempty"`
	BaseRef               string       `json:"baseRef,omitempty"`
	WorktreePath          string       `json:"worktreePath,omitempty"`
	TerminalID            string       `json:"terminalId,omitempty"`
	ProviderSessionID     string       `json:"providerSessionId,omitempty"`
	Title                 string       `json:"title"`
	State                 SessionState `json:"state"`
	StartedAt             time.Time    `json:"startedAt"`
	UpdatedAt             time.Time    `json:"updatedAt"`
	LastActivityAt        time.Time    `json:"lastActivityAt"`
	LastPreview           string       `json:"lastPreview,omitempty"`
	Unread                bool         `json:"unread"`
	Archived              bool         `json:"archived"`
	// Adopted marks a session LaraEnv discovered through a provider hook in one
	// of its own terminals instead of launching it from the Agents page.
	Adopted   bool   `json:"adopted,omitempty"`
	ExitError string `json:"exitError,omitempty"`
}

// ProjectLocation describes where a working directory sits inside a LaraEnv
// project: the main checkout or one of its git worktrees.
type ProjectLocation struct {
	ProjectName  string
	ProjectPath  string
	WorktreePath string
	Branch       string
	Mode         LaunchMode
}

type UsageWindow struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	UsedPct  *float64   `json:"usedPct,omitempty"`
	ResetsAt *time.Time `json:"resetsAt,omitempty"`
}

type UsageSnapshot struct {
	Provider AgentID       `json:"provider"`
	Account  string        `json:"account,omitempty"`
	Plan     string        `json:"plan,omitempty"`
	Status   string        `json:"status"`
	Message  string        `json:"message,omitempty"`
	ReadAt   time.Time     `json:"readAt"`
	Stale    bool          `json:"stale"`
	Windows  []UsageWindow `json:"windows"`
}
