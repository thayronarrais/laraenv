package vcruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lara4ever/internal/i18n"
)

// TestMain loads the real catalogues before running this package's tests, so
// RedistError.Error() (which now renders via i18n.T) produces the actual
// English prose with {{binary}}/{{url}} interpolated instead of degrading to
// the bare key path "errors.vcruntime.redistNotInstalled" — which is what
// T() falls back to when Global() is nil, and would make every
// strings.Contains(msg, ...) assertion below fail even though the
// mechanism is otherwise working correctly. Same pattern as
// internal/services/manager_test.go and internal/agents/manager_test.go.
func TestMain(m *testing.M) {
	if marker := os.Getenv("LARAENV_VCRUNTIME_TEST_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte(fmt.Sprint(os.Getpid())), 0600)
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load langs:", err)
		os.Exit(1)
	}
	i18n.SetGlobal(set)
	os.Exit(m.Run())
}

// TestRedistErrorDefaultsWhenEmpty guards the existing behaviour/message
// shape: a caller that doesn't know a specific redistributable (redistURL
// == "") must still get the vs17 default, not an empty/broken URL.
func TestRedistErrorDefaultsWhenEmpty(t *testing.T) {
	err := redistError("php.exe", "")
	if err == nil {
		t.Fatal("redistError returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, RedistURL) {
		t.Errorf("message %q does not contain default RedistURL %q", msg, RedistURL)
	}
	if strings.Contains(msg, RedistURLVS18) {
		t.Errorf("message %q unexpectedly contains the vs18 URL when no redistURL was given", msg)
	}
	if !strings.Contains(msg, "php.exe") {
		t.Errorf("message %q does not name the offending binary", msg)
	}
}

// TestRedistErrorSurfacesDeclaredRedistURL is the core regression this
// package exists to prevent: a caller that knows the binary needs a
// non-default redistributable (e.g. a vs18 PHP 8.6 build) must get that
// exact URL back, not the vs17 default that would send the user to the
// wrong installer.
func TestRedistErrorSurfacesDeclaredRedistURL(t *testing.T) {
	err := redistError("php.exe", RedistURLVS18)
	if err == nil {
		t.Fatal("redistError returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, RedistURLVS18) {
		t.Errorf("message %q does not contain the declared RedistURLVS18 %q", msg, RedistURLVS18)
	}
	if strings.Contains(msg, RedistURL) {
		t.Errorf("message %q unexpectedly contains the vs17 default alongside the declared vs18 URL", msg)
	}
}

// TestURLFromError covers the other half of the typed-error contract: a
// caller holding a plain error (nil, or an unrelated error) must get "" —
// never fabricate a URL — while a *RedistError (direct, as Check returns
// it) must yield its exact RedistURL. This is what lets services.Manager
// and app.go thread the URL into an event/struct field instead of the
// frontend parsing it back out of the message.
func TestURLFromError(t *testing.T) {
	if got := URLFromError(nil); got != "" {
		t.Errorf("URLFromError(nil) = %q, want \"\"", got)
	}
	if got := URLFromError(errors.New("some unrelated failure")); got != "" {
		t.Errorf("URLFromError(unrelated) = %q, want \"\"", got)
	}
	re := &RedistError{Binary: "php.exe", RedistURL: RedistURLVS18}
	if got := URLFromError(re); got != RedistURLVS18 {
		t.Errorf("URLFromError(RedistError) = %q, want %q", got, RedistURLVS18)
	}
	// Check itself returns exactly this type — confirm the two agree.
	if got := URLFromError(redistError("php.exe", RedistURLVS18)); got != RedistURLVS18 {
		t.Errorf("URLFromError(redistError(...)) = %q, want %q", got, RedistURLVS18)
	}
}

// TestCheckMissingBinaryIsANoOp: Check must stay a no-op for a binary that
// hasn't been installed yet, regardless of which redistURL was passed —
// mirrors the existing "not yet installed" contract.
func TestCheckMissingBinaryIsANoOp(t *testing.T) {
	if err := Check(`C:\definitely\not\a\real\path\php.exe`, ""); err != nil {
		t.Errorf("Check() with empty redistURL = %v, want nil for a missing binary", err)
	}
	if err := Check(`C:\definitely\not\a\real\path\php.exe`, RedistURLVS18); err != nil {
		t.Errorf("Check() with a declared redistURL = %v, want nil for a missing binary", err)
	}
}

// TestIsIncompatibleRuntimeWarning covers the parsing logic against
// fabricated output, standing in for PHP's real diagnostic:
//
//	PHP Warning:  'C:\Windows\SYSTEM32\VCRUNTIME140.dll' 14.44 is not
//	compatible with this PHP build linked with 14.51 in Unknown on line 0
//
// The match must survive version and DLL-name churn (those change with
// every toolchain bump) while staying silent on unrelated output.
func TestIsIncompatibleRuntimeWarning(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "real PHP 8.6 field diagnostic",
			output: `PHP Warning:  'C:\Windows\SYSTEM32\VCRUNTIME140.dll' 14.44 is not compatible with this PHP build linked with 14.51 in Unknown on line 0` + "\n",
			want:   true,
		},
		{
			name:   "different DLL and version numbers — marker text is what matters",
			output: `PHP Warning:  'C:\Windows\SYSTEM32\MSVCP140.dll' 14.30 is not compatible with this PHP build linked with 14.40 in Unknown on line 0`,
			want:   true,
		},
		{
			name:   "marker present among other output",
			output: "Some preamble\n" + `PHP Warning:  'VCRUNTIME140_1.dll' 14.40 is not compatible with this PHP build linked with 14.44 in Unknown on line 0` + "\nmore output\n",
			want:   true,
		},
		{
			name:   "clean version output",
			output: "PHP 8.5.10 (cli) (built: Oct  1 2025 12:00:00) (NTS Visual C++ 2022 x64)\nCopyright (c) The PHP Group\n",
			want:   false,
		},
		{
			name:   "unrelated warning",
			output: "PHP Warning:  Unknown: Failed loading Zend extension in Unknown on line 0",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIncompatibleRuntimeWarning(tc.output); got != tc.want {
				t.Errorf("isIncompatibleRuntimeWarning(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

// TestSmokeIndicatesBrokenRuntime covers the decision Check makes from a
// smoke run's (exit code, output) pair — the actual regression this
// defect is about. vcruntime.Check previously only recognized
// STATUS_DLL_NOT_FOUND, so PHP 8.6 against an old vs17 redist (present,
// but too old — a different failure entirely) never tripped it: the
// process started, printed the "not compatible" warning, and died with a
// non-special exit code that Check silently treated as fine.
func TestSmokeIndicatesBrokenRuntime(t *testing.T) {
	incompatibleOutput := `PHP Warning:  'C:\Windows\SYSTEM32\VCRUNTIME140.dll' 14.44 is not compatible with this PHP build linked with 14.51 in Unknown on line 0`

	cases := []struct {
		name   string
		code   int
		output string
		want   bool
	}{
		{
			name:   "missing DLL entirely — the original STATUS_DLL_NOT_FOUND case",
			code:   statusDLLNotFound,
			output: "",
			want:   true,
		},
		{
			name:   "incompatible runtime: warns then fails — the PHP 8.6 field case",
			code:   255,
			output: incompatibleOutput,
			want:   true,
		},
		{
			name:   "incompatible runtime warning but the process still exits 0 — not broken, would be a false alarm",
			code:   0,
			output: incompatibleOutput,
			want:   false,
		},
		{
			name:   "ordinary non-zero exit unrelated to the runtime — not ours to diagnose",
			code:   1,
			output: "php: unrecognized option '--bogus-flag'",
			want:   false,
		},
		{
			name:   "clean success",
			code:   0,
			output: "PHP 8.5.10 (cli)\n",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := smokeIndicatesBrokenRuntime(tc.code, tc.output); got != tc.want {
				t.Errorf("smokeIndicatesBrokenRuntime(%d, %q) = %v, want %v", tc.code, tc.output, got, tc.want)
			}
		})
	}
}
