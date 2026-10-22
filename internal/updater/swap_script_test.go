package updater

import (
	"strings"
	"testing"
)

const (
	target = `C:\portatil\LaraEnv.exe`
	fresh  = `C:\portatil\cache\LaraEnv.exe`
	logf   = `C:\portatil\cache\update.log`
)

func TestPortableSwapScriptBacksUpBeforeOverwriting(t *testing.T) {
	s := portableSwapScript(target, fresh, logf)

	if !strings.Contains(s, target+".old") {
		t.Fatalf("the running executable must be moved aside before it is replaced:\n%s", s)
	}
}

// If the copy fails — a read-only USB stick, antivirus holding the file — the
// user must not be left with no executable at all.
func TestPortableSwapScriptRestoresTheBackupWhenTheCopyFails(t *testing.T) {
	s := portableSwapScript(target, fresh, logf)

	restore := strings.Index(s, `move /y "`+target+`.old" "`+target+`"`)
	if restore < 0 {
		t.Fatalf("no restore path found:\n%s", s)
	}
}

func TestPortableSwapScriptRelaunchesTheApp(t *testing.T) {
	s := portableSwapScript(target, fresh, logf)

	if !strings.Contains(s, `start "" "`+target+`"`) {
		t.Fatalf("the app must come back up after the swap:\n%s", s)
	}
}

// Everything the script does goes to the log next to the download, so a
// failure can be inspected after the fact — the window is hidden.
func TestPortableSwapScriptLogsEveryStep(t *testing.T) {
	s := portableSwapScript(target, fresh, logf)

	if got := strings.Count(s, logf); got < 4 {
		t.Fatalf("only %d log redirections, want the whole flow traced:\n%s", got, s)
	}
}
