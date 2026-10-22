package agents

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func activityHookBinding() ActivityBinding {
	return ActivityBinding{OwnerID: "11111111-1111-4111-8111-111111111111", SessionID: "lara-1", RunID: "run-1", TerminalID: "term-1", Provider: AgentClaude, ProviderSessionID: "root-1"}
}

func TestActivityHookSubagentDoesNotCompleteRoot(t *testing.T) {
	for _, provider := range []AgentID{AgentClaude, AgentCodex} {
		b := activityHookBinding()
		b.Provider = provider
		events, err := NormalizeActivityHook(provider, strings.NewReader(`{"hook_event_name":"SubagentStop","session_id":"root-1","agent_id":"child-1"}`), b)
		if err != nil || len(events) != 1 || events[0].Kind != ActivitySubagentTurnStop || events[0].NodeID != "child-1" {
			t.Fatalf("%s: %+v %v", provider, events, err)
		}
		events, err = NormalizeActivityHook(provider, strings.NewReader(`{"hook_event_name":"Stop","session_id":"root-1"}`), b)
		if err != nil || len(events) != 0 {
			t.Fatalf("root stop emitted child: %+v %v", events, err)
		}
	}
}
func TestActivityHookDiscardsPrivateFields(t *testing.T) {
	raw := `{"hook_event_name":"SubagentStart","session_id":"root-1","agent_id":"child-1","agent_type":"worker\u0001","model":"gpt","tool_input":"PRIVATE_SENTINEL","tool_response":"PRIVATE_SENTINEL","messages":["PRIVATE_SENTINEL"],"transcript_path":"PRIVATE_SENTINEL","prompt":"PRIVATE_SENTINEL","receivedAt":"2000-01-01T00:00:00Z"}`
	events, err := NormalizeActivityHook(AgentClaude, strings.NewReader(raw), activityHookBinding())
	if err != nil || len(events) != 1 {
		t.Fatalf("%+v %v", events, err)
	}
	data, _ := json.Marshal(events)
	if bytes.Contains(data, []byte("PRIVATE_SENTINEL")) {
		t.Fatal("payload leaked")
	}
	if events[0].AgentType != "worker" || events[0].ReceivedAt.Year() == 2000 {
		t.Fatal(events)
	}
}
func TestActivityHookInventoryAbsentEmptyAndClipped(t *testing.T) {
	for _, suffix := range []string{"", `,"background_tasks":[]`} {
		events, err := NormalizeActivityHook(AgentClaude, strings.NewReader(`{"hook_event_name":"SubagentStop","session_id":"root-1","agent_id":"child-1"`+suffix+`}`), activityHookBinding())
		if err != nil {
			t.Fatal(err)
		}
		if suffix == "" && len(events) != 1 {
			t.Fatal(events)
		}
		if suffix != "" && (len(events) != 2 || events[1].BackgroundTasks == nil || len(*events[1].BackgroundTasks) != 0) {
			t.Fatal(events)
		}
	}
	tasks := []map[string]string{{"id": "check", "type": "checklist", "description": "no"}, {"id": "child-1", "type": "subagent", "description": "no"}}
	for i := 0; i < 160; i++ {
		tasks = append(tasks, map[string]string{"id": strings.Repeat("a", i+1), "type": "shell", "description": strings.Repeat("界", 240), "status": "running"})
	}
	raw, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "session_id": "root-1", "background_tasks": tasks})
	// Keep the fixture below the provider-input bound while exceeding envelope size.
	raw = bytes.ReplaceAll(raw, []byte(strings.Repeat("界", 240)), []byte(strings.Repeat("x", 240)))
	events, err := NormalizeActivityHook(AgentClaude, bytes.NewReader(raw), activityHookBinding())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || !events[0].Truncated {
		t.Fatal(events)
	}
	data, _ := json.Marshal(events[0])
	if len(data) > 16*1024 {
		t.Fatal(len(data))
	}
	for _, task := range *events[0].BackgroundTasks {
		if task.ProviderNodeID == "check" || task.ProviderNodeID == "child-1" {
			t.Fatal(task)
		}
	}
}
func TestActivityHookUntrustedOrUnassociatedIsUnavailable(t *testing.T) {
	if _, err := NormalizeActivityHook(AgentClaude, strings.NewReader(`{"hook_event_name":"SubagentStart","session_id":"root-1","agent_id":"c"}`), ActivityBinding{}); err == nil {
		t.Fatal("unbound hook accepted")
	}
	if _, err := NormalizeActivityHook(AgentClaude, strings.NewReader(`{}`+strings.Repeat(" ", 65536)), activityHookBinding()); err == nil {
		t.Fatal("oversized payload accepted")
	}
}
func TestActivityHookPreservesForeignHooks(t *testing.T) {
	base, err := addLifecycleSignals(Invocation{Args: []string{"--resume", "root-1"}}, AgentClaude, "lara-1", "LaraEnv.exe")
	if err != nil {
		t.Fatal(err)
	}
	got, err := addActivitySignals(base, AgentClaude, "LaraEnv.exe")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Args) != len(base.Args) || !strings.Contains(got.Args[1], "claude-stop") || !strings.Contains(got.Args[1], "SubagentStart") || !strings.Contains(got.Args[1], "--agent-activity") {
		t.Fatal(got)
	}
	codex, err := addActivitySignals(Invocation{Args: []string{"resume", "root-1"}}, AgentCodex, "LaraEnv.exe")
	if err != nil || len(codex.Args) != 2 {
		t.Fatal(codex, err)
	}
}

func TestActivityHookPreservesMixedForeignHookGroup(t *testing.T) {
	object := map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": "foreign-hook"}, map[string]any{"type": "command", "command": "app --agent-hook claude"}}}}}}
	stripLaraEnvJSONHooks(object)
	data, _ := json.Marshal(object)
	if !strings.Contains(string(data), "foreign-hook") || strings.Contains(string(data), "--agent-hook") {
		t.Fatal(string(data))
	}
}

func TestActivityHookSyntheticFixtures(t *testing.T) {
	for _, provider := range []AgentID{AgentClaude, AgentCodex} {
		for _, kind := range []string{"start", "stop"} {
			raw, err := os.ReadFile(filepath.Join("testdata", string(provider), "activity-"+kind+".json"))
			if err != nil {
				t.Fatal(err)
			}
			b := activityHookBinding()
			b.Provider = provider
			events, err := NormalizeActivityHook(provider, bytes.NewReader(raw), b)
			if err != nil || len(events) == 0 {
				t.Fatal(events, err)
			}
			data, _ := json.Marshal(events)
			if bytes.Contains(data, []byte("PRIVATE_SENTINEL")) {
				t.Fatal("fixture leaked")
			}
		}
	}
}

func TestLifecycleActivityIdentityIsTransientAndLiteral(t *testing.T) {
	identity := ActivityLifecycleIdentity{RunID: "run-1", OwnerID: activityHookBinding().OwnerID}
	for _, provider := range []AgentID{AgentClaude, AgentCodex} {
		invocation, err := addLifecycleSignals(Invocation{}, provider, "lara-1", "app.exe", identity)
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(invocation.Args, " ")
		if !strings.Contains(joined, "--activity-run") || !strings.Contains(joined, "run-1") || !strings.Contains(joined, identity.OwnerID) {
			t.Fatal(joined)
		}
	}
}

func TestActivityHookOfficialBackgroundTaskTypes(t *testing.T) {
	raw := `{"hook_event_name":"Stop","session_id":"root-1","background_tasks":[{"id":"m","type":"monitor","description":"Monitor","status":"running"},{"id":"w","type":"workflow","description":"Workflow","status":"running"},{"id":"t","type":"teammate","description":"Teammate","status":"running"},{"id":"c","type":"cloud session","description":"Cloud","status":"running"},{"id":"p","type":"MCP task","description":"MCP","status":"running"}]}`
	events, err := NormalizeActivityHook(AgentClaude, strings.NewReader(raw), activityHookBinding())
	if err != nil || len(events) != 1 || len(*events[0].BackgroundTasks) != 5 {
		t.Fatalf("%+v %v", events, err)
	}
}

func TestActivityHookInvalidInventoryIdentityPreservesPartialCoverage(t *testing.T) {
	for _, idField := range []string{`"id":"invalid/id",`, `"id":"",`, ""} {
		t.Run(idField, func(t *testing.T) {
			payload := `{"hook_event_name":"Stop","session_id":"root-1","background_tasks":[{` + idField + `"type":"shell","status":"running"}]}`
			events, err := NormalizeActivityHook(AgentClaude, strings.NewReader(payload), activityHookBinding())
			if err != nil || len(events) != 1 || events[0].BackgroundTasks == nil || len(*events[0].BackgroundTasks) != 0 || !events[0].Truncated {
				t.Fatalf("invalid identity became complete empty inventory: %+v %v", events, err)
			}
		})
	}
	payload := `{"hook_event_name":"Stop","session_id":"root-1","background_tasks":[{"id":"task-1","type":"shell","status":"running"},{"id":"task-1","type":"shell","status":"running"}]}`
	events, err := NormalizeActivityHook(AgentClaude, strings.NewReader(payload), activityHookBinding())
	if err != nil || len(events) != 1 || len(*events[0].BackgroundTasks) != 1 || events[0].Truncated {
		t.Fatal("duplicate should remain complete", events, err)
	}
}
