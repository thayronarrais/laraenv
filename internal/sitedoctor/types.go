// Package sitedoctor inspects project prerequisites without repairing them.
// Commands and database probes are supplied by the project-aware application.
package sitedoctor

import (
	"context"

	"lara4ever/internal/projects"
)

type Finding struct {
	ID                   string `json:"id"`
	Status               string `json:"status"`
	Severity             string `json:"severity"`
	Code                 string `json:"code"`
	Detail               string `json:"detail"`
	FixID                string `json:"fixID"`
	RequiresConfirmation bool   `json:"requiresConfirmation"`
	RuntimeKind          string `json:"runtimeKind"`
	InstallVersion       string `json:"installVersion"`
}

type Report struct {
	Project     string                     `json:"project"`
	Path        string                     `json:"path"`
	Kind        string                     `json:"kind"`
	Healthy     bool                       `json:"healthy"`
	Findings    []Finding                  `json:"findings"`
	PhpRuntime  projects.RuntimeResolution `json:"phpRuntime"`
	NodeRuntime projects.RuntimeResolution `json:"nodeRuntime"`
}

type VhostCheck struct {
	Path     string
	Expected []byte
}

type Target struct {
	Project                       projects.Project
	PHPExe, NodeDir, ComposerPhar string
	Hosts                         []byte
	HostsError                    error
	Hostnames                     []string
	Vhosts                        []VhostCheck
	PHPFPMStatus                  string
	PHPFPMStatuses                map[string]string
}

type MigrationState struct {
	Pending int
}

type Dependencies struct {
	Database   func(context.Context, DatabaseConnection) (DatabaseState, error)
	Migrations func(context.Context, projects.Project) (MigrationState, error)
}
