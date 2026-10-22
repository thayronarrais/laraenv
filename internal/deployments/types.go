// Package deployments stores account-independent, protected local deployment state.
package deployments

import (
	"encoding/json"
	"errors"
)

var (
	ErrNotFound           = errors.New("deployment or run not found")
	ErrInvalidData        = errors.New("invalid deployment data")
	ErrSecretsUnavailable = errors.New("deployment secrets unavailable for this OS profile")
	ErrConflict           = errors.New("deployment conflict")
)

const (
	SchemaVersion  = 1
	MaxRuns        = 100
	MaxLogBytes    = 4 << 20
	MaxImportBytes = 16 << 20
	maxRecordBytes = 8 << 20
	maxIndexBytes  = 64 << 20
)

type Protector interface {
	Protect(string) (string, error)
	Unprotect(string) (string, error)
}

type Deployment struct {
	ID                 string         `json:"id"`
	Name               string         `json:"name"`
	SSHHostExternalID  string         `json:"ssh_host_external_id"`
	OSFamily           string         `json:"os_family,omitempty"`
	OSVersion          string         `json:"os_version,omitempty"`
	ProjectKind        string         `json:"project_kind,omitempty"`
	Config             map[string]any `json:"config,omitempty"`
	LastStatus         string         `json:"last_status,omitempty"`
	LastRunAt          string         `json:"last_run_at,omitempty"`
	Runs               []RunSummary   `json:"runs,omitempty"`
	SecretsUnavailable bool           `json:"secretsUnavailable"`
	Origin             *Origin        `json:"origin,omitempty"`
	CreatedAt          string         `json:"created_at"`
	UpdatedAt          string         `json:"updated_at"`
}

type RunSummary struct {
	ID              string `json:"id"`
	DeploymentID    string `json:"deployment_id,omitempty"`
	Kind            string `json:"kind"`
	Status          string `json:"status"`
	ErrorMessage    string `json:"error_message,omitempty"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at,omitempty"`
	HasLog          bool   `json:"has_log"`
	Truncated       bool   `json:"truncated,omitempty"`
	Interrupted     bool   `json:"interrupted,omitempty"`
	RecoveryPending bool   `json:"recovery_pending,omitempty"`
}

// ImportSource scopes numeric cloud IDs to an explicitly selected service/account.
// A string source in file imports is an explicit, opaque namespace.
type ImportSource struct {
	ServiceURL string `json:"service_url,omitempty"`
	AccountID  string `json:"account_id,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

func (s *ImportSource) UnmarshalJSON(data []byte) error {
	var namespace string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &namespace); err != nil {
			return err
		}
		*s = ImportSource{Namespace: namespace}
		return nil
	}
	type source ImportSource
	var v source
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*s = ImportSource(v)
	return nil
}

type Origin struct {
	Source ImportSource `json:"source"`
	ID     string       `json:"id"`
	// Fingerprint identifies the last imported source content, never a secret.
	Fingerprint string `json:"fingerprint,omitempty"`
}

type ImportBundle struct {
	Version     int          `json:"version"`
	Source      ImportSource `json:"source"`
	Deployments []Deployment `json:"deployments"`
	// Keys are source ssh_host_external_id values; values are explicit local host IDs.
	HostMappings map[string]string `json:"host_mappings"`
}
type ImportConflict struct {
	ID         string `json:"id"`
	ExistingID string `json:"existing_id"`
	Reason     string `json:"reason"`
}
type ImportResult struct {
	Inserted  []string         `json:"inserted"`
	Skipped   []string         `json:"skipped"`
	Conflicts []ImportConflict `json:"conflicts"`
}
