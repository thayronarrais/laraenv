package projectroutes

import "time"

type TargetKind string

const (
	TargetMainCheckout TargetKind = "main-checkout"
	TargetWorktree     TargetKind = "worktree"
)

type ServeMode string

const (
	ServeDocRoot ServeMode = "docroot"
	ServeProxy   ServeMode = "proxy"
)

type Route struct {
	ID           string     `json:"id"`
	ProjectName  string     `json:"projectName"`
	Hostname     string     `json:"hostname"`
	Canonical    bool       `json:"canonical"`
	TargetKind   TargetKind `json:"targetKind"`
	TargetPath   string     `json:"targetPath"`
	WorktreePath string     `json:"worktreePath,omitempty"`
	ServeMode    ServeMode  `json:"serveMode"`
	ProxyPort    int        `json:"proxyPort,omitempty"`
	SSL          bool       `json:"ssl"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type Project struct {
	Name          string
	MainPath      string
	TLD           string
	SSL           bool
	MainProxyPort int
}

type Target struct {
	Path string
	Kind TargetKind
}

type Repair struct {
	ProjectName string `json:"projectName"`
	Message     string `json:"message"`
}
