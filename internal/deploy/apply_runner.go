package deploy

import (
	"bufio"
	"io"
	"strings"
	"sync"

	"lara4ever/internal/i18n"

	xssh "golang.org/x/crypto/ssh"
)

// RunApply executes the plan against the remote SSH client. Each step opens
// a fresh session and streams stdout+stderr line-by-line via `onLine` so the
// caller (TUI / desktop UI) can render progress in real time — `apt install`
// + `composer install` are minutes-long operations and a "frozen" UI is the
// #1 deploy-tui complaint.
//
// `onStep` (optional, may be nil) fires twice per step:
//   - status="running" before the command runs,
//   - status="ok" / "error" / "ignored" after it finishes.
//
// The desktop Apply UI uses this to drive the step-list checkmarks /
// progress bar; deploy-tui can keep using its log-prefix parser too.
//
// Stops on the first non-Ignore step that returns a non-zero exit. Returns
// the full combined log so the caller can encrypt and persist it.
func RunApply(
	client *xssh.Client,
	steps []Step,
	onLine func(string),
	onStep func(idx, total int, title, status string),
) (string, error) {
	if onLine == nil {
		onLine = func(string) {}
	}
	if onStep == nil {
		onStep = func(int, int, string, string) {}
	}
	var (
		logBuf strings.Builder
		// Lines may arrive concurrently from stdout + stderr scanners — guard
		// the shared buffer + the onLine callback (which writes to a channel
		// in deploy-tui that the bubbletea Update reads).
		mu sync.Mutex
	)
	emit := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		onLine(s)
		logBuf.WriteString(s)
		logBuf.WriteByte('\n')
	}

	total := len(steps)
	for i, st := range steps {
		onStep(i, total, st.Title, "running")
		// st.Title itself stays plain English: Step is shared with
		// cmd/deploy-tui (deploy.BuildPlan), which has no loaded catalogue —
		// only this function's own wrapper text is translated, matching how
		// internal/vcruntime keeps RedistURL and internal/installer keeps
		// git/tool output untranslated inside a translated frame.
		emit(i18n.T("backend.cloud.stepHeader", map[string]any{"idx": i + 1, "total": total, "title": st.Title}))

		cmd := st.Cmd
		if st.Sudo {
			// `sudo -n` = non-interactive (won't prompt). If sudoers isn't
			// configured for passwordless use, this fails fast.
			cmd = "sudo -n bash -lc " + shQuote(cmd)
		} else {
			// Wrap in `bash -lc` for consistent env (PATH, locale).
			cmd = "bash -lc " + shQuote(cmd)
		}

		emit("$ " + st.Cmd)

		sess, err := client.NewSession()
		if err != nil {
			return logBuf.String(), i18n.Wrapf(err, "errors.cloud.openSessionFailed", map[string]any{"step": st.Title})
		}

		stdout, err := sess.StdoutPipe()
		if err != nil {
			_ = sess.Close()
			return logBuf.String(), i18n.Wrapf(err, "errors.cloud.stdoutPipeFailed", map[string]any{"step": st.Title})
		}
		stderr, err := sess.StderrPipe()
		if err != nil {
			_ = sess.Close()
			return logBuf.String(), i18n.Wrapf(err, "errors.cloud.stderrPipeFailed", map[string]any{"step": st.Title})
		}

		if err := sess.Start(cmd); err != nil {
			_ = sess.Close()
			return logBuf.String(), i18n.Wrapf(err, "errors.cloud.stepStartFailed", map[string]any{"step": st.Title})
		}

		// Pump both pipes concurrently. The 1MB scanner buffer accommodates
		// the rare line that exceeds the bufio default (composer JSON output,
		// some certbot warnings, etc).
		var wg sync.WaitGroup
		// Track tail separately so an error message can quote the last bit.
		tail := make([]string, 0, 64)
		var tailMu sync.Mutex
		appendTail := func(line string) {
			tailMu.Lock()
			defer tailMu.Unlock()
			tail = append(tail, line)
			if len(tail) > 64 {
				tail = tail[len(tail)-64:]
			}
		}
		pump := func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for sc.Scan() {
				line := sc.Text()
				emit(line)
				appendTail(line)
			}
		}
		wg.Add(2)
		go pump(stdout)
		go pump(stderr)

		runErr := sess.Wait()
		wg.Wait()
		_ = sess.Close()

		if runErr != nil {
			if st.Ignore {
				emit(i18n.T("backend.cloud.nonZeroExitIgnored", map[string]any{"error": runErr}))
				onStep(i, total, st.Title, "ignored")
				continue
			}
			tailMu.Lock()
			tailStr := strings.Join(tail, "\n")
			tailMu.Unlock()
			onStep(i, total, st.Title, "error")
			return logBuf.String(), i18n.Errorf("errors.cloud.stepFailed", map[string]any{"step": st.Title, "error": runErr.Error(), "tail": tailStr})
		}
		onStep(i, total, st.Title, "ok")
	}

	emit("=== All steps completed successfully ===")
	return logBuf.String(), nil
}
