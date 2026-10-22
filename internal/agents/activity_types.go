package agents

import (
	"context"
	"time"
)

type ActivityState string

const (
	ActivityStarting      ActivityState = "starting"
	ActivityWorking       ActivityState = "working"
	ActivityNeedsInput    ActivityState = "needs-input"
	ActivityReplyFinished ActivityState = "reply-finished"
	ActivityIdle          ActivityState = "idle"
	ActivityStopped       ActivityState = "stopped"
	ActivityInterrupted   ActivityState = "interrupted"
	ActivityError         ActivityState = "error"
	ActivityUnknown       ActivityState = "unknown"
)

type ActivityStatus string

const (
	ActivityLoading     ActivityStatus = "loading"
	ActivityAvailable   ActivityStatus = "available"
	ActivityPartial     ActivityStatus = "partial"
	ActivityUnavailable ActivityStatus = "unavailable"
	ActivityStatusError ActivityStatus = "error"
)

type ActivityMessageCode string

const (
	ActivityMessageNone                ActivityMessageCode = "none"
	ActivityMessageNoProviderSession   ActivityMessageCode = "no-provider-session"
	ActivityMessageHooksUnobserved     ActivityMessageCode = "hooks-unobserved"
	ActivityMessageHooksDisabled       ActivityMessageCode = "hooks-disabled"
	ActivityMessageHookTrustRequired   ActivityMessageCode = "hook-trust-required"
	ActivityMessageUnsupportedProvider ActivityMessageCode = "unsupported-provider"
	ActivityMessageUnsupportedProtocol ActivityMessageCode = "unsupported-protocol"
	ActivityMessageObservationEnded    ActivityMessageCode = "observation-ended"
	ActivityMessageRefreshFailed       ActivityMessageCode = "refresh-failed"
	ActivityMessageOwnerUnavailable    ActivityMessageCode = "owner-unavailable"
	ActivityMessageOwnerConflict       ActivityMessageCode = "owner-conflict"
	ActivityMessageTruncated           ActivityMessageCode = "truncated"
)

type ActivityCapabilities struct {
	Children        bool `json:"children"`
	BackgroundTasks bool `json:"backgroundTasks"`
	LiveState       bool `json:"liveState"`
	Duration        bool `json:"duration"`
	Model           bool `json:"model"`
	Tokens          bool `json:"tokens"`
}

type ActivityNodeKind string

const (
	ActivityAgent              ActivityNodeKind = "agent"
	ActivityBackgroundTaskNode ActivityNodeKind = "background-task"
)

type ActivityModelKind string

const (
	ActivityModelConfigured ActivityModelKind = "configured"
	ActivityModelObserved   ActivityModelKind = "observed"
)

type ActivityModel struct {
	Name string            `json:"name"`
	Kind ActivityModelKind `json:"kind"`
}

type ActivityTokens struct {
	Input  *int64 `json:"input,omitempty"`
	Output *int64 `json:"output,omitempty"`
	Total  *int64 `json:"total,omitempty"`
}

type ActivityNode struct {
	ID              string           `json:"id"`
	ProviderNodeID  string           `json:"providerNodeId"`
	ParentID        string           `json:"parentId,omitempty"`
	Kind            ActivityNodeKind `json:"kind"`
	Label           string           `json:"label"`
	AgentType       string           `json:"agentType,omitempty"`
	State           ActivityState    `json:"state"`
	FirstObservedAt time.Time        `json:"firstObservedAt"`
	LastObservedAt  time.Time        `json:"lastObservedAt"`
	StartedAt       *time.Time       `json:"startedAt,omitempty"`
	EndedAt         *time.Time       `json:"endedAt,omitempty"`
	Model           *ActivityModel   `json:"model,omitempty"`
	Tokens          *ActivityTokens  `json:"tokens,omitempty"`
	ProjectName     string           `json:"projectName,omitempty"`
	ProjectPath     string           `json:"projectPath,omitempty"`
	WorktreePath    string           `json:"worktreePath,omitempty"`
	TerminalID      string           `json:"terminalId,omitempty"`
}

type ActivitySnapshot struct {
	SessionID       string               `json:"sessionId"`
	RunID           string               `json:"runId"`
	SourceID        string               `json:"sourceId"`
	Revision        uint64               `json:"revision"`
	RootID          string               `json:"rootId"`
	Provider        AgentID              `json:"provider"`
	ObservedSince   time.Time            `json:"observedSince"`
	ObservedAt      time.Time            `json:"observedAt"`
	Status          ActivityStatus       `json:"status"`
	MessageCode     ActivityMessageCode  `json:"messageCode"`
	Capabilities    ActivityCapabilities `json:"capabilities"`
	Nodes           []ActivityNode       `json:"nodes"`
	BackgroundTasks []ActivityNode       `json:"backgroundTasks"`
	Truncated       bool                 `json:"truncated"`
	Stale           bool                 `json:"stale"`
}

type ActivityRun struct {
	Session   AgentSession
	RunID     string
	OwnerID   string
	StartedAt time.Time
}

type ActivityEventKind string

const (
	ActivitySubagentStart       ActivityEventKind = "subagent-start"
	ActivitySubagentTurnStop    ActivityEventKind = "subagent-turn-stop"
	ActivitySubagentNeedsInput  ActivityEventKind = "subagent-needs-input"
	ActivitySubagentError       ActivityEventKind = "subagent-error"
	ActivityBackgroundInventory ActivityEventKind = "background-inventory"
)

// ActivityBackgroundTask contains only structured, allowlisted inventory data.
// ProviderNodeID is a provider identity, never an internal LaraEnv node ID.
type ActivityBackgroundTask struct {
	ProviderNodeID string        `json:"providerNodeId"`
	Label          string        `json:"label"`
	AgentType      string        `json:"agentType,omitempty"`
	State          ActivityState `json:"state"`
	StartedAt      *time.Time    `json:"startedAt,omitempty"`
	EndedAt        *time.Time    `json:"endedAt,omitempty"`
}

type ActivityEvent struct {
	OwnerID           string            `json:"ownerId"`
	TerminalID        string            `json:"terminalId"`
	SessionID         string            `json:"sessionId"`
	RunID             string            `json:"runId"`
	Provider          AgentID           `json:"provider"`
	ProviderSessionID string            `json:"providerSessionId"`
	NodeID            string            `json:"nodeId,omitempty"`
	ParentProviderID  string            `json:"parentProviderId,omitempty"`
	TurnID            string            `json:"turnId,omitempty"`
	Kind              ActivityEventKind `json:"kind"`
	AgentType         string            `json:"agentType,omitempty"`
	Model             *ActivityModel    `json:"model,omitempty"`
	ReceivedAt        time.Time         `json:"receivedAt"`
	// A pointer preserves absent inventory versus a confirmed empty [] snapshot.
	BackgroundTasks *[]ActivityBackgroundTask `json:"backgroundTasks,omitempty"`
	Truncated       bool                      `json:"truncated"`
}

type ActivityInterestPurpose string

const (
	ActivityInterestCounter ActivityInterestPurpose = "counter"
	ActivityInterestDialog  ActivityInterestPurpose = "dialog"
)

type ActivityInterest struct {
	WindowID  string                  `json:"windowId"`
	Purpose   ActivityInterestPurpose `json:"purpose"`
	SessionID string                  `json:"sessionId"`
}

// ActivityMetadataQuery never requests transcript or general history access.
// KnownParents maps proven child provider IDs to their direct provider parent.
type ActivityMetadataQuery struct {
	Executable     string
	RootProviderID string
	KnownParents   map[string]string
}

// Metadata has no state, token, or interval fields: discovery cannot prove them.
type ActivityMetadataNode struct {
	ProviderNodeID   string
	ParentProviderID string
	Label            string
	AgentType        string
	Model            *ActivityModel
	FirstObservedAt  time.Time
	LastObservedAt   time.Time
}

type ActivityMetadataResult struct {
	// Coordinator-only source validity, independent of the primary notice.
	// Metadata may revoke live coverage but cannot establish it.
	liveObservationDisabled bool
	Nodes                   []ActivityMetadataNode
	BackgroundTasks         *[]ActivityBackgroundTask
	Capabilities            ActivityCapabilities
	Status                  ActivityStatus
	MessageCode             ActivityMessageCode
	ObservedAt              time.Time
	Truncated               bool
	Stale                   bool
}

type ActivityMetadataReader interface {
	Read(context.Context, ActivityMetadataQuery) (ActivityMetadataResult, error)
}
