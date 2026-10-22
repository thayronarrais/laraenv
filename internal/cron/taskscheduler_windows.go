//go:build windows

package cron

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"lara4ever/internal/i18n"
)

// taskName is the canonical Windows Task Scheduler name for a LaraEnv job.
// Prefixed with "LaraEnv\" so all our tasks live in a dedicated folder
// inside Task Scheduler — easy to see at a glance and easy to wipe if
// uninstalling.
func taskName(jobID string) string {
	return `LaraEnv\` + jobID
}

// EnsureBackgroundTask registers (or refreshes) a Task Scheduler task that
// fires every minute and calls `<exePath> --cron-run <jobID>`. The runner
// itself decides — using the cron expression — whether the current minute
// actually matches the job's schedule. This keeps the schtasks→cron
// translation trivial and supports any robfig expression.
//
// /SC MINUTE /MO 1 fires once per minute, starting roughly now. /F forces
// overwrite of an existing task with the same name (idempotent).
func EnsureBackgroundTask(jobID, exePath string) error {
	if jobID == "" || exePath == "" {
		return i18n.Errorf("errors.cron.backgroundTaskArgsRequired", nil)
	}
	tr := fmt.Sprintf(`"%s" --cron-run %s`, exePath, jobID)
	cmd := schtasks(
		"/Create",
		"/TN", taskName(jobID),
		"/TR", tr,
		"/SC", "MINUTE",
		"/MO", "1",
		"/F",
		"/RL", "LIMITED",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return i18n.Wrapf(err, "errors.cron.registerSchtasksFailed", map[string]any{"task": taskName(jobID), "output": strings.TrimSpace(string(out))})
	}
	return nil
}

// RemoveBackgroundTask deletes the Task Scheduler entry for jobID. Safe
// to call on a job that was never registered — schtasks returns a non-zero
// exit code which we swallow.
func RemoveBackgroundTask(jobID string) {
	if jobID == "" {
		return
	}
	cmd := schtasks("/Delete", "/TN", taskName(jobID), "/F")
	_ = cmd.Run()
}

// HasBackgroundTask returns true when Task Scheduler has an entry for jobID.
// Useful for reconciliation on startup.
func HasBackgroundTask(jobID string) bool {
	cmd := schtasks("/Query", "/TN", taskName(jobID))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	return cmd.Run() == nil
}

func schtasks(args ...string) *exec.Cmd {
	cmd := exec.Command("schtasks.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
