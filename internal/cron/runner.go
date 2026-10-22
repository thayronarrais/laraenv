package cron

import (
	"encoding/json"
	"os"
	"time"

	rcron "github.com/robfig/cron/v3"

	"lara4ever/internal/i18n"
)

// RunOnceFromConfig is the entry point for the `--cron-run <jobID>` CLI
// subcommand. It reads the persisted job list from configPath, finds the
// matching job, and executes it iff the schedule says the current minute
// is a firing minute.
//
// This decouples Task Scheduler (which fires every minute) from the cron
// expression: the per-minute heartbeat is cheap, and only matching jobs
// actually run. Output is written back to config.json under the same
// LastRun/LastOutput/LastError fields the in-process scheduler uses, so
// the UI shows results regardless of which path executed the job.
//
// pathFor / workDirFor mirror the in-process resolvers; both may be nil
// (the runner has no access to project metadata in the trimmed-down CLI
// startup).
func RunOnceFromConfig(configPath, jobID string, pathFor PathResolver, workDirFor WorkDirResolver, validators ...RuntimeValidator) error {
	var validator RuntimeValidator
	if len(validators) > 0 {
		validator = validators[0]
	}
	return RunOnceWithRuntimeFromConfig(configPath, jobID, pathFor, workDirFor, validator, nil)
}

// RunOnceWithRuntimeFromConfig also supplies checkout-aware runtime preflight
// and PATH resolution for background jobs launched by Task Scheduler.
func RunOnceWithRuntimeFromConfig(configPath, jobID string, pathFor PathResolver, workDirFor WorkDirResolver, validateRuntime RuntimeValidator, runtimePathFor RuntimePathResolver, environmentResolvers ...func(string, string, []string) ([]string, error)) error {
	if jobID == "" {
		return i18n.Errorf("errors.cron.jobIdRequired", nil)
	}
	job, err := loadJob(configPath, jobID)
	if err != nil {
		return err
	}
	if !job.Enabled || !job.BackgroundMode {
		// Defensive: if the user disabled the job or flipped off background
		// mode but Task Scheduler still has the task, just no-op. We don't
		// auto-remove the schtasks entry from here to avoid races with the
		// running app.
		return nil
	}

	parser := rcron.NewParser(
		rcron.SecondOptional | rcron.Minute | rcron.Hour | rcron.Dom |
			rcron.Month | rcron.Dow | rcron.Descriptor,
	)
	sched, err := parser.Parse(job.Schedule)
	if err != nil {
		return i18n.Wrapf(err, "errors.cron.parseScheduleFailed", nil)
	}
	now := time.Now()
	// "Should we run this minute?" — compute the next firing from the
	// start of the previous minute. If it lands inside the current minute
	// window, the cron line says fire-now.
	prev := now.Add(-1 * time.Minute).Truncate(time.Minute)
	next := sched.Next(prev)
	if next.Before(now.Truncate(time.Minute)) || next.After(now.Truncate(time.Minute).Add(time.Minute)) {
		return nil
	}

	// Build a one-off Manager with no engine, just to reuse execute().
	m := &Manager{
		jobs:       map[string]*Job{job.ID: &job},
		entries:    map[string]rcron.EntryID{},
		pathFor:    pathFor,
		workDirFor: workDirFor,
		persist: func(jobs []Job) error {
			return saveJobs(configPath, jobs)
		},
	}
	m.SetRuntimeValidator(validateRuntime)
	m.SetRuntimePathResolver(runtimePathFor)
	if len(environmentResolvers) > 0 {
		m.SetEnvironmentResolver(environmentResolvers[0])
	}
	m.execute(&job)
	return nil
}

// loadJob extracts a single job by ID from the persisted config. We
// intentionally don't reuse internal/config here to keep the CLI runner
// startup fast and free of side-effects (config.NewStore touches a lot of
// other state).
func loadJob(configPath, jobID string) (Job, error) {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return Job{}, i18n.Wrapf(err, "errors.cron.readConfigFailed", nil)
	}
	var raw struct {
		Crons []Job `json:"crons"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return Job{}, i18n.Wrapf(err, "errors.cron.parseConfigFailed", nil)
	}
	for _, j := range raw.Crons {
		if j.ID == jobID {
			return j, nil
		}
	}
	return Job{}, i18n.Errorf("errors.cron.jobNotFoundInConfig", map[string]any{"id": jobID, "path": configPath})
}

// saveJobs writes the cron list back to config.json without disturbing
// any other fields. Read-modify-write — the running app may also be
// editing config; the worst case is a missed update on a concurrent
// write, which the next persist will pick up.
func saveJobs(configPath string, jobs []Job) error {
	b, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	raw["crons"] = jobs
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0644)
}
