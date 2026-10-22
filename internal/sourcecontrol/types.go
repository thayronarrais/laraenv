package sourcecontrol

import (
	"lara4ever/internal/workspacefiles"
	"os"
)

type WorkspaceOpener func(workspacefiles.ContextRef, string) (*os.Root, *workspacefiles.RootInfo, workspacefiles.Result)
type RepositoryRef struct {
	Context       workspacefiles.ContextRef `json:"context"`
	RootKey       string                    `json:"rootKey"`
	RepositoryKey string                    `json:"repositoryKey"`
}
type Expectation struct {
	SnapshotRevision string `json:"snapshotRevision"`
	IndexRevision    string `json:"indexRevision"`
	HeadOID          string `json:"headOID"`
	Branch           string `json:"branch"`
}
type Remote struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DisplayURL     string `json:"displayURL"`
	CanPush        bool   `json:"canPush"`
	DisabledReason string `json:"disabledReason"`
}
type Entry struct {
	ID             string `json:"id"`
	RelativePath   string `json:"relativePath"`
	OriginalPath   string `json:"originalPath"`
	DisplayPath    string `json:"displayPath"`
	IndexCode      string `json:"indexCode"`
	WorktreeCode   string `json:"worktreeCode"`
	Kind           string `json:"kind"`
	SubmoduleState string `json:"submoduleState"`
	HeadOID        string `json:"headOID"`
	IndexOID       string `json:"indexOID"`
	HeadMode       string `json:"headMode"`
	IndexMode      string `json:"indexMode"`
	WorktreeMode   string `json:"worktreeMode"`
	CanStage       bool   `json:"canStage"`
	CanUnstage     bool   `json:"canUnstage"`
	DisabledReason string `json:"disabledReason"`
}
type Snapshot struct {
	Repository       RepositoryRef           `json:"repository"`
	Root             workspacefiles.RootInfo `json:"root"`
	SnapshotRevision string                  `json:"snapshotRevision"`
	IndexRevision    string                  `json:"indexRevision"`
	HeadOID          string                  `json:"headOID"`
	Branch           string                  `json:"branch"`
	Upstream         string                  `json:"upstream"`
	GitOperation     string                  `json:"gitOperation"`
	Unborn           bool                    `json:"unborn"`
	Detached         bool                    `json:"detached"`
	CountsKnown      bool                    `json:"countsKnown"`
	HasConflicts     bool                    `json:"hasConflicts"`
	Busy             bool                    `json:"busy"`
	Ahead            int                     `json:"ahead"`
	Behind           int                     `json:"behind"`
	Entries          []Entry                 `json:"entries"`
	Remotes          []Remote                `json:"remotes"`
	OperationID      string                  `json:"operationId"`
}
type EntrySelection struct {
	EntryID      string `json:"entryId"`
	DiskRevision string `json:"diskRevision"`
}
type FilesRequest struct {
	Repository RepositoryRef    `json:"repository"`
	Expected   Expectation      `json:"expected"`
	Entries    []EntrySelection `json:"entries"`
}
type CommitRequest struct {
	Repository RepositoryRef `json:"repository"`
	Expected   Expectation   `json:"expected"`
	Message    string        `json:"message"`
}
type PushRequest struct {
	Repository   RepositoryRef `json:"repository"`
	Expected     Expectation   `json:"expected"`
	RemoteID     string        `json:"remoteId"`
	RemoteBranch string        `json:"remoteBranch"`
	SetUpstream  bool          `json:"setUpstream"`
}
type DiffRequest struct {
	Repository RepositoryRef `json:"repository"`
	Expected   Expectation   `json:"expected"`
	EntryID    string        `json:"entryId"`
	Layer      string        `json:"layer"`
}
type DiffSide struct {
	Text     string `json:"text"`
	Revision string `json:"revision"`
	OID      string `json:"oid"`
	EOL      string `json:"eol"`
	Size     int64  `json:"size"`
	BOM      bool   `json:"bom"`
}
type Diff struct {
	ID              string        `json:"id"`
	Repository      RepositoryRef `json:"repository"`
	EntryID         string        `json:"entryId"`
	RelativePath    string        `json:"relativePath"`
	OriginalPath    string        `json:"originalPath"`
	Layer           string        `json:"layer"`
	Kind            string        `json:"kind"`
	Original        DiffSide      `json:"original"`
	Modified        DiffSide      `json:"modified"`
	CanOpenWorktree bool          `json:"canOpenWorktree"`
	Message         string        `json:"message"`
}
type Operation struct {
	ID               string        `json:"id"`
	Repository       RepositoryRef `json:"repository"`
	Kind             string        `json:"kind"`
	State            string        `json:"state"`
	ResultCode       string        `json:"resultCode"`
	Output           string        `json:"output"`
	Message          string        `json:"message"`
	Sequence         uint64        `json:"sequence"`
	OutputTruncated  bool          `json:"outputTruncated"`
	HasExitCode      bool          `json:"hasExitCode"`
	ExitCode         int           `json:"exitCode"`
	StartedAt        string        `json:"startedAt"`
	FinishedAt       string        `json:"finishedAt"`
	CommitOID        string        `json:"commitOID"`
	Remote           string        `json:"remote"`
	RemoteBranch     string        `json:"remoteBranch"`
	MutationObserved bool          `json:"mutationObserved"`
	RemoteUncertain  bool          `json:"remoteUncertain"`
	AfterSnapshot    *Snapshot     `json:"afterSnapshot"`
}
type OperationEvent struct {
	OperationID string        `json:"operationId"`
	Repository  RepositoryRef `json:"repository"`
	Sequence    uint64        `json:"sequence"`
	State       string        `json:"state"`
	Stream      string        `json:"stream"`
	Chunk       string        `json:"chunk"`
}
type BusyState struct {
	Busy         bool     `json:"busy"`
	OperationIDs []string `json:"operationIds"`
}
type Result struct {
	Code      string     `json:"code"`
	Message   string     `json:"message"`
	Snapshot  *Snapshot  `json:"snapshot"`
	Diff      *Diff      `json:"diff"`
	Operation *Operation `json:"operation"`
	Busy      *BusyState `json:"busy"`
}
type OperationEmitter func(OperationEvent)
