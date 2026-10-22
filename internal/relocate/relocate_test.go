package relocate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Every stored path under the old root follows the move, however deeply it is
// nested and whatever the field is called - the state files gain fields over
// time and a hardcoded list of names would quietly go stale.
func TestJSONFileRebasesNestedPaths(t *testing.T) {
	p := write(t, fmt.Sprintf(`{"sessions":[{"projectPath":%q,"projectKey":%q,"agentId":"claude"}]}`,
		`E:\stick\LaraEnv\www\shop`, `e:\stick\laraenv\www\shop`))

	n, err := JSONFile(p, `E:\stick\LaraEnv`, `D:\dev\LaraEnv`)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rewrote %d paths, want 2", n)
	}

	got := read(t, p)["sessions"].([]any)[0].(map[string]any)
	if got["projectPath"] != `D:\dev\LaraEnv\www\shop` {
		t.Errorf("projectPath = %v", got["projectPath"])
	}
	// projectKey is a lowercased lookup key; a mixed-case root would orphan
	// every record pointing at it.
	if got["projectKey"] != `d:\dev\laraenv\www\shop` {
		t.Errorf("projectKey = %v", got["projectKey"])
	}
	if got["agentId"] != "claude" {
		t.Errorf("unrelated value was touched: %v", got["agentId"])
	}
}

func TestJSONFileLeavesPathsOutsideTheOldRootAlone(t *testing.T) {
	p := write(t, fmt.Sprintf(`{"targetPath":%q}`, `D:\code\other`))

	n, err := JSONFile(p, `E:\stick\LaraEnv`, `D:\dev\LaraEnv`)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rewrote %d paths, want none", n)
	}
	if got := read(t, p)["targetPath"]; got != `D:\code\other` {
		t.Errorf("targetPath = %v", got)
	}
}

// A fresh install has no state files yet. That is not a failure.
func TestJSONFileIgnoresAMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.json")

	if _, err := JSONFile(missing, `E:\old`, `D:\new`); err != nil {
		t.Fatalf("missing file reported as an error: %v", err)
	}
}

// Never leave the user with a truncated state file because it could not be
// parsed - report and leave it exactly as it was.
func TestJSONFileLeavesUnparseableContentUntouched(t *testing.T) {
	p := write(t, `{not json`)

	if _, err := JSONFile(p, `E:\old`, `D:\new`); err == nil {
		t.Fatal("want an error for unparseable content")
	}
	b, _ := os.ReadFile(p)
	if string(b) != `{not json` {
		t.Fatalf("file was modified: %q", string(b))
	}
}

// Nothing to change means nothing is written.
func TestJSONFileDoesNotRewriteWhenNothingMatches(t *testing.T) {
	p := write(t, `{"a":"b"}`)
	before, _ := os.Stat(p)

	if _, err := JSONFile(p, `E:\old`, `D:\new`); err != nil {
		t.Fatal(err)
	}

	after, _ := os.Stat(p)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("file was rewritten with no changes to make")
	}
}
