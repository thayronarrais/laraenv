package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverClaudeSessionIDsUsesTranscriptNamesWithoutReadingContent(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	cwd := `C:\projects\Demo App`
	projectDir := filepath.Join(configDir, "projects", strings.ToLower(claudeProjectDirectory(cwd)))
	if err := os.MkdirAll(filepath.Join(projectDir, "subagents"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"session-one.jsonl": "this content must not need to be valid JSON",
		"session-two.JSONL": "private transcript content",
		"notes.txt":         "not a session",
	} {
		if err := os.WriteFile(filepath.Join(projectDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	ids, err := discoverClaudeSessionIDs(cwd)
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, ids, "session-one", "session-two")
}

func TestParseCodexSessionIDsReadsOnlyIDs(t *testing.T) {
	ids, err := parseCodexSessionIDs([]byte(`{
		"data": [
			{"id":"thread-old","title":"private title","preview":"private prompt"},
			{"id":"thread-new","title":"another private title"},
			{"id":""}
		],
		"nextCursor": null
	}`))
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, ids, "thread-old", "thread-new")
	if _, leaked := ids["private title"]; leaked {
		t.Fatal("provider title leaked into the session ID set")
	}
}

func TestParseOpenCodeSessionIDsFiltersByDirectory(t *testing.T) {
	ids, err := parseOpenCodeSessionIDs([]byte(`[
		{"id":"same-project","directory":"C:\\projects\\demo","title":"private title"},
		{"id":"case-insensitive","directory":"c:\\PROJECTS\\DEMO"},
		{"id":"other-project","directory":"C:\\projects\\other"},
		{"id":"","directory":"C:\\projects\\demo"}
	]`), `C:\projects\demo`)
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, ids, "same-project", "case-insensitive")
}

func TestParseKimiSessionIDsSupportsDocumentedMetadataNames(t *testing.T) {
	index := strings.Join([]string{
		`{"sessionId":"camel","sessionDir":"ignored","workDir":"C:\\projects\\demo","title":"private title"}`,
		`{"session_id":"snake","session_dir":"ignored","work_dir":"c:\\PROJECTS\\DEMO"}`,
		`{"sessionId":"other","workDir":"C:\\projects\\other"}`,
		`not valid json`,
	}, "\n")
	ids, err := parseKimiSessionIDs(strings.NewReader(index), `C:\projects\demo`)
	if err != nil {
		t.Fatal(err)
	}
	assertSessionIDs(t, ids, "camel", "snake")
}

func TestParseProviderSessionMetadataRejectsInvalidContainers(t *testing.T) {
	if _, err := parseCodexSessionIDs([]byte(`[]`)); err == nil {
		t.Fatal("Codex array response should be rejected")
	}
	if _, err := parseOpenCodeSessionIDs([]byte(`{}`), `C:\projects\demo`); err == nil {
		t.Fatal("OpenCode object response should be rejected")
	}
}

func assertSessionIDs(t *testing.T, got map[string]struct{}, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("session IDs = %#v, want %v", got, want)
	}
	for _, id := range want {
		if _, ok := got[id]; !ok {
			t.Fatalf("session ID %q is missing from %#v", id, got)
		}
	}
}
