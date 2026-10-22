// Package vcruntime detects when a Windows binary can't launch because the
// Microsoft Visual C++ Redistributable isn't installed system-wide.
//
// Background: PHP, MySQL, Apache, PostgreSQL and friends are all built
// against MSVC and dynamically link to vcruntime/msvcp DLLs. The zips on
// windows.php.net / MariaDB / Apache Lounge don't ship them — they assume
// the system has the redist installed. On a clean Windows without it,
// every launch fails with exit code 0xc0000135 (STATUS_DLL_NOT_FOUND),
// which surfaces as a cryptic "exit status 0xc0000135" anywhere we exec
// the binary.
//
// We deliberately don't try to fetch/extract/install the redistributable
// ourselves: every approach (download + /layout + msiexec /a, embedded
// DLLs, bundling the redist in our MSI) has its own failure modes and
// fragility. Instead, this package smoke-tests the binary and, when it
// fails for the loader-error reason specifically, returns an error that
// names the problem and points the user at the official download URL.
// The user installs it once, system-wide, and every MSVC binary on the
// machine works from then on.
package vcruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"lara4ever/internal/i18n"
)

// createNoWindow (0x08000000) suppresses the conhost console allocation when
// spawning a console-subsystem binary on Windows. Without it, the smoke probe
// below flashes a black console window every time we start (or auto-restart)
// a service — which is exactly what the user notices as "janelas piscando".
const createNoWindow = 0x08000000

// statusDLLNotFound is the Windows kernel exit code raised when the loader
// can't find a dependency DLL. ProcessState.ExitCode returns this as a
// positive int on 64-bit Go.
const statusDLLNotFound = 0xc0000135

// RedistURL is Microsoft's evergreen URL for the latest VS 2015–2022 x64
// redistributable. Versions are forward-compatible: VS17's VCRUNTIME140
// also satisfies binaries linked against VS16/VS15. This is the default
// used by Check when the caller doesn't name a different one.
const RedistURL = "https://aka.ms/vs/17/release/vc_redist.x64.exe"

// RedistURLVS18 is Microsoft's evergreen URL for the VS 2026 (vs18) x64
// redistributable. It is a genuinely different binary from RedistURL, not
// an alias — vs18 builds (PHP 8.6+) need it specifically; the vs17
// redistributable does not satisfy their loader dependencies. Both URLs
// live in this one package so there is a single place they're maintained.
const RedistURLVS18 = "https://aka.ms/vs/18/release/vc_redist.x64.exe"

// Check verifies that the binary at exePath can launch — i.e. that the
// Windows loader can resolve its MSVC dependencies, AND that any MSVC
// runtime it did find is actually new enough for how the binary was built.
// redistURL names the specific Visual C++ Redistributable this binary needs
// (e.g. RedistURLVS18 for a vs18 build); pass "" to use the default
// RedistURL. Check never infers the toolchain itself — from exePath or
// otherwise — because that's exactly the kind of guess that hands the user
// the wrong download link; the caller knows which PHP/binary it launched
// and must say so explicitly.
// Returns:
//   - nil if the binary doesn't exist (caller passed a path for something
//     not yet installed).
//   - nil if the binary loads cleanly (system has the redist).
//   - nil if the binary fails for some non-loader reason (a wrong CLI flag,
//     a missing config, etc.) — that's not ours to diagnose.
//   - a *RedistError pointing at the resolved redistURL when the failure is
//     STATUS_DLL_NOT_FOUND (the DLL is missing entirely), OR when the
//     process printed PHP's "not compatible with this PHP build linked
//     with" diagnostic and then failed (the DLL is present but older than
//     the toolchain the binary was linked against — e.g. a vs17 redist next
//     to a vs18 PHP build). Either way the caller surfaces it to the user —
//     as text via Error(), or as a typed URL via URLFromError/errors.As.
func Check(exePath, redistURL string) error {
	return CheckContext(context.Background(), exePath, redistURL)
}

// CheckContext cancels the temporary smoke probe without binding an eventual
// service process to the caller's startup context.
func CheckContext(ctx context.Context, exePath, redistURL string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(exePath); err != nil {
		return nil
	}
	code, output, err := smokeRunContext(ctx, exePath)
	if err != nil {
		return err
	}
	if !smokeIndicatesBrokenRuntime(code, output) {
		return nil
	}
	return redistError(filepath.Base(exePath), redistURL)
}

// smokeIndicatesBrokenRuntime decides, from a smoke run's exit code and
// captured output alone, whether the failure is the redistributable
// problem this package exists to catch. Split out from Check so the
// decision is unit-testable against fabricated (code, output) pairs
// without spawning a real process. Two cases:
//
//   - code == statusDLLNotFound: the loader couldn't find a dependency DLL
//     at all (STATUS_DLL_NOT_FOUND) — the classic missing-redist case.
//   - code != 0 and the output carries PHP's "not compatible with this PHP
//     build linked with" diagnostic: the DLL is present but older than the
//     toolchain the binary was linked against (e.g. a vs17 redist next to
//     a vs18 PHP build) — present, but too old.
//
// A warning in the output with a clean exit (code == 0) is deliberately
// NOT treated as broken: that binary ran fine despite the warning, and
// reporting it would send users off to install a redistributable they
// don't need. Only a build that warns and then fails counts.
func smokeIndicatesBrokenRuntime(code int, output string) bool {
	if code == statusDLLNotFound {
		return true
	}
	return code != 0 && isIncompatibleRuntimeWarning(output)
}

// RedistError is the error Check returns when a binary fails to launch
// because the Windows loader can't resolve its MSVC dependencies. RedistURL
// is a typed field, not just text baked into Error() — it is the single
// source of truth for "which redistributable does this failure need",
// meant to be read back out with errors.As (or URLFromError) and handed to
// the UI directly. Nothing downstream should ever need to parse the URL
// back out of the message string; that's how the wrong link ends up on
// screen.
type RedistError struct {
	// Binary is the base name of the executable that failed to launch
	// (e.g. "php.exe").
	Binary string
	// RedistURL is the resolved redistributable URL — the caller-supplied
	// one, or RedistURL (the vs17 default) when the caller didn't name one.
	// Always non-empty.
	RedistURL string
}

// Error renders via i18n.T rather than i18n.Errorf, because RedistError must
// stay this concrete type — URLFromError's errors.As depends on it — while
// i18n.Errorf would return a plain *i18n.Error and lose the typed RedistURL
// field.
//
// DO NOT translate the phrase "Visual C++ Redistributable" in either
// langs/en.json or langs/pt-BR.json, and do not change the "<binary> can't
// launch: ..." shape. The frontend matches on this text directly, not on a
// typed error code:
//   - frontend/src/components/NewProjectModal.tsx (REDIST_PROBE check) and
//     frontend/src/pages/Dashboard.tsx do
//     `e.message.includes("Visual C++ Redistributable")` to detect this
//     specific failure and offer the "install redistributable" prompt;
//   - Dashboard.tsx then runs `e.message.match(/^([^:]+\.exe)/)` to pull the
//     binary name back out of the front of the message.
//
// If "Visual C++ Redistributable" is ever translated in pt-BR, the prompt
// silently stops appearing for Portuguese-speaking users — precisely when a
// project can't start without it. errors.vcruntime.redistNotInstalled is
// listed in langs/.do-not-translate.json (the value must keep this exact
// English substring in every locale — the rest of the sentence translates
// normally), and tools/langs/check.mjs enforces that on every PR against
// either repo — the comment above is a courtesy for readers of this file,
// not the enforcement mechanism itself.
func (e *RedistError) Error() string {
	return i18n.T("errors.vcruntime.redistNotInstalled", map[string]any{"binary": e.Binary, "url": e.RedistURL})
}

// redistError builds the "can't launch" error naming the right
// redistributable: the caller-supplied redistURL when it's non-empty (e.g.
// RedistURLVS18 for a vs18 build), or the app-wide default RedistURL
// otherwise. Split out from Check so this selection logic — the actual
// point of this package's rework for PHP 8.6 — is unit-testable without
// spawning a process.
func redistError(bin, redistURL string) error {
	if redistURL == "" {
		redistURL = RedistURL
	}
	return &RedistError{Binary: bin, RedistURL: redistURL}
}

// URLFromError extracts the resolved redistributable URL from an error
// returned (directly or wrapped) by Check. Returns "" when err is nil or
// isn't a *RedistError — callers should treat "" as "no redist URL to
// report" and let the UI fall back to its own default rather than guess.
// This is the one place that knows how to pull the URL back out of an
// error, so callers (services.Manager, app.go's project-create path, …)
// never need their own errors.As boilerplate or, worse, a message-parsing
// regex.
func URLFromError(err error) string {
	var re *RedistError
	if errors.As(err, &re) {
		return re.RedistURL
	}
	return ""
}

// smokeRun runs `<exe> --version` and returns the process exit code plus
// its combined stdout+stderr output. `--version` is the most
// universally-supported quick-exit argument across the binaries we care
// about (php, mysqld, httpd, nginx, postgres, redis-server). When the
// binary's MSVC deps are unresolved entirely, the arg never gets a chance
// to be parsed: the OS loader fails first with STATUS_DLL_NOT_FOUND. When
// deps resolve but are too old for the toolchain the binary was linked
// against, the process starts, PHP prints its own "not compatible with
// this PHP build linked with" warning, and then exits non-zero — no
// special OS exit code, just PHP's own diagnostic in the output. When deps
// are fine and the arg isn't recognized, the binary exits non-zero with
// neither of those signals, which we treat as "not our problem" upstream.
//
// On Windows, even a missing-DLL launch normally produces a ProcessState
// (the OS loader sets the exit code to STATUS_DLL_NOT_FOUND). The unusual
// case where Run errors AND ProcessState is nil — process couldn't be
// kicked off at all — is collapsed onto STATUS_DLL_NOT_FOUND so the
// caller errs on the side of surfacing the redist hint. The alternative
// — treating "couldn't tell" as "binary works" — masks the worst class
// of failures.
func smokeRun(exePath string) (int, string) {
	code, output, _ := smokeRunContext(context.Background(), exePath)
	return code, output
}

func smokeRunContext(ctx context.Context, exePath string) (int, string, error) {
	cmd := exec.CommandContext(ctx, exePath, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		killCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		kill := exec.CommandContext(killCtx, "taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid))
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		_ = kill.Run()
		_ = cmd.Process.Kill()
		return nil
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return 0, out.String(), ctx.Err()
	}
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode(), out.String(), nil
	}
	if err != nil {
		return statusDLLNotFound, out.String(), nil
	}
	return 0, out.String(), nil
}

// incompatibleRuntimeMarker is the stable, version-independent substring of
// PHP's own diagnostic when a loaded MSVC runtime DLL is older than the
// toolchain PHP was built with, e.g.:
//
//	PHP Warning:  'C:\Windows\SYSTEM32\VCRUNTIME140.dll' 14.44 is not
//	compatible with this PHP build linked with 14.51 in Unknown on line 0
//
// Deliberately excludes the DLL name and both version numbers — those
// change with every toolchain bump (VCRUNTIME140 vs VCRUNTIME140_1 vs
// MSVCP140, 14.44 vs 14.51, …) while PHP's own wording for this specific
// diagnostic has stayed stable across releases.
const incompatibleRuntimeMarker = "is not compatible with this PHP build linked with"

// isIncompatibleRuntimeWarning reports whether smoke-run output contains
// PHP's incompatible-runtime diagnostic. Split out from Check/smokeRun so
// the parsing is unit-testable against captured output strings without
// spawning a real PHP process.
func isIncompatibleRuntimeWarning(output string) bool {
	return strings.Contains(output, incompatibleRuntimeMarker)
}
