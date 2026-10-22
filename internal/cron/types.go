package cron

import "time"

// Job is one scheduled command. ProjectName is optional — empty means a
// global cron that isn't tied to a specific project (used for system tasks).
//
// Schedule accepts 5-field ("* * * * *") or 6-field cron expressions, plus
// descriptors like "@every 1m", "@hourly", "@daily". See robfig/cron docs.
type Job struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProjectName string    `json:"projectName,omitempty"`
	Schedule    string    `json:"schedule"`
	Command     string    `json:"command"`
	WorkingDir  string    `json:"workingDir,omitempty"` // overrides project root when set
	PhpVersion  string    `json:"phpVersion,omitempty"` // overrides project PHP when set
	Enabled     bool      `json:"enabled"`
	// BackgroundMode registers the job with Windows Task Scheduler so it
	// fires even when LaraEnv is closed. Default false (in-process only).
	// Switching this on/off re-registers the job on the next save.
	BackgroundMode bool `json:"backgroundMode,omitempty"`
	LastRun     time.Time `json:"lastRun,omitempty"`
	LastStatus  string    `json:"lastStatus,omitempty"` // "ok" | "error" | "running"
	LastOutput  string    `json:"lastOutput,omitempty"` // truncated to 8KB
	LastError   string    `json:"lastError,omitempty"`
	NextRun     time.Time `json:"nextRun,omitempty"` // computed, not persisted
}

const maxOutputBytes = 8 * 1024
