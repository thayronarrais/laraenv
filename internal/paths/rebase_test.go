package paths

import "testing"

func TestSameRootIgnoresCaseAndTrailingSeparator(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		// Windows does not distinguish case, and treating these as different
		// roots would relocate an environment onto itself.
		{`C:\laraenv`, `C:\LaraEnv`, true},
		{`C:\laraenv`, `C:\laraenv\`, true},
		{`C:\laraenv`, `C:/laraenv`, true},
		{`C:\laraenv`, `D:\laraenv`, false},
		{`C:\laraenv`, `C:\laraenv-portable`, false},
	} {
		if got := SameRoot(tc.a, tc.b); got != tc.want {
			t.Errorf("SameRoot(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestRebaseMovesPathsUnderTheOldRoot(t *testing.T) {
	got, ok := Rebase(`C:\portatil`, `D:\dev\laraenv`, `C:\portatil\www\shop`)

	if !ok || got != `D:\dev\laraenv\www\shop` {
		t.Fatalf("Rebase = %q, %v", got, ok)
	}
}

// A project the user keeps outside the environment is not part of it. Moving
// the portable folder must not rewrite where their unrelated code lives.
func TestRebaseLeavesPathsOutsideTheOldRootAlone(t *testing.T) {
	got, ok := Rebase(`C:\portatil`, `D:\dev\laraenv`, `E:\code\other-project`)

	if ok {
		t.Fatalf("rewrote an unrelated path to %q", got)
	}
	if got != `E:\code\other-project` {
		t.Fatalf("returned %q, want the input unchanged", got)
	}
}

func TestRebaseMatchesTheOldRootRegardlessOfCase(t *testing.T) {
	got, ok := Rebase(`C:\Portatil`, `D:\new`, `c:\portatil\www\shop`)

	if !ok || got != `d:\new\www\shop` {
		t.Fatalf("Rebase = %q, %v — a lowercased key must stay lowercased", got, ok)
	}
}

// agent-sessions.json stores projectKey lowercased and looks sessions up by
// it. Rebasing that field to a mixed-case root would orphan every session.
func TestRebaseKeepsTheCaseStyleOfWhatItReplaced(t *testing.T) {
	lower, _ := Rebase(`C:\portatil`, `D:\New\Root`, `c:\portatil\www\shop`)
	if lower != `d:\new\root\www\shop` {
		t.Errorf("lowercase input produced %q", lower)
	}

	mixed, _ := Rebase(`C:\portatil`, `D:\New\Root`, `C:\portatil\www\shop`)
	if mixed != `D:\New\Root\www\shop` {
		t.Errorf("mixed-case input produced %q", mixed)
	}
}

// A path that only shares a prefix is a different directory.
func TestRebaseDoesNotMatchAPartialSegment(t *testing.T) {
	if got, ok := Rebase(`C:\lara`, `D:\new`, `C:\laraenv\www`); ok {
		t.Fatalf("rewrote a sibling directory to %q", got)
	}
}

// The root itself is under itself.
func TestRebaseMovesTheRootItself(t *testing.T) {
	got, ok := Rebase(`C:\portatil`, `D:\new`, `C:\portatil`)

	if !ok || got != `D:\new` {
		t.Fatalf("Rebase = %q, %v", got, ok)
	}
}
