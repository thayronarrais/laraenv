// Package mcpbridge exposes the running GUI's backend through authenticated
// loopback MCP and a stdio proxy. It never constructs application managers.
package mcpbridge

import (
	"context"
	"time"
)

type Args struct {
	Action         string   `json:"action"`
	Project        string   `json:"project,omitempty"`
	Service        string   `json:"service,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	ID             string   `json:"id,omitempty"`
	Version        string   `json:"version,omitempty"`
	Enabled        *bool    `json:"enabled,omitempty"`
	Port           int      `json:"port,omitempty"`
	Command        string   `json:"command,omitempty"`
	Args           []string `json:"args,omitempty"`
	Lines          int      `json:"lines,omitempty"`
	Engine         string   `json:"engine,omitempty"`
	Database       string   `json:"database,omitempty"`
	Confirm        bool     `json:"confirm,omitempty"`
	TimeoutSeconds int      `json:"timeoutSeconds,omitempty"`
}

type Backend interface {
	Call(context.Context, string, Args, func(string)) (any, error)
}

// AuditEvent deliberately excludes commands, arguments, output and error text:
// those can contain credentials. The callback runs once per authenticated tool
// call, including schema failures (without copying their rejected input fields).
type AuditEvent struct {
	ID         string    `json:"id"`
	Group      string    `json:"group"`
	Action     string    `json:"action"`
	Project    string    `json:"project,omitempty"`
	Service    string    `json:"service,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	Database   string    `json:"database,omitempty"`
	Engine     string    `json:"engine,omitempty"`
	JobID      string    `json:"jobId,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	DurationMS int64     `json:"durationMs"`
	Success    bool      `json:"success"`
	Cancelled  bool      `json:"cancelled"`
}

type Options struct {
	Backend        Backend
	DescriptorPath string
	Version        string
	Audit          func(AuditEvent)
}

type Status struct {
	Running        bool   `json:"running"`
	URL            string `json:"url,omitempty"`
	DescriptorPath string `json:"descriptorPath"`
}
