package main

import (
	"strings"
	"testing"

	pkgagents "lara4ever/internal/agents"
)

func envWith(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func drainHookSignals(t *testing.T, directory string) []pkgagents.SessionSignal {
	t.Helper()
	var signals []pkgagents.SessionSignal
	if err := pkgagents.DrainSessionSignals(directory, func(signal pkgagents.SessionSignal) { signals = append(signals, signal) }); err != nil {
		t.Fatal(err)
	}
	return signals
}

func TestAgentHookIgnoresTerminalsLaraEnvDoesNotOwn(t *testing.T) {
	directory := t.TempDir()
	handled, err := writeAgentHook(
		[]string{"LaraEnv.exe", "--agent-hook", "claude"},
		strings.NewReader(`{"hook_event_name":"SessionStart","session_id":"abc","cwd":"C:\\demo","source":"startup"}`),
		envWith(nil), directory,
	)
	if !handled || err != nil {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if got := drainHookSignals(t, directory); len(got) != 0 {
		t.Fatalf("hook wrote a signal without LARAENV_TERMINAL_ID: %#v", got)
	}
	handled, err = writeAgentHook(
		[]string{"LaraEnv.exe", "--agent-hook", "claude"},
		strings.NewReader(`{"hook_event_name":"Stop","session_id":"abc","cwd":"C:\\demo"}`),
		envWith(map[string]string{"LARAENV_TERMINAL_ID": "term-1", "LARAENV_AGENT_SESSION": "lara-1"}), directory,
	)
	if !handled || err != nil {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if got := drainHookSignals(t, directory); len(got) != 0 {
		t.Fatalf("hook duplicated a launched session's signal: %#v", got)
	}
}

func TestAgentHookMapsClaudeEvents(t *testing.T) {
	env := envWith(map[string]string{"LARAENV_TERMINAL_ID": "term-4"})
	cases := []struct {
		payload string
		kind    pkgagents.SignalKind
		source  string
	}{
		{`{"hook_event_name":"SessionStart","session_id":"s1","cwd":"C:\\demo","source":"resume","transcript_path":"private"}`, pkgagents.SignalStarted, "resume"},
		{`{"hook_event_name":"UserPromptSubmit","session_id":"s1","cwd":"C:\\demo","prompt":"private"}`, pkgagents.SignalWorking, ""},
		{`{"hook_event_name":"Stop","session_id":"s1","cwd":"C:\\demo"}`, pkgagents.SignalDone, ""},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt","session_id":"s1","cwd":"C:\\demo","message":"private"}`, pkgagents.SignalNeedsInput, ""},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt","session_id":"s1","cwd":"C:\\demo"}`, pkgagents.SignalDone, ""},
		{`{"hook_event_name":"SessionEnd","session_id":"s1","cwd":"C:\\demo","reason":"other"}`, pkgagents.SignalEnded, ""},
	}
	for _, tc := range cases {
		directory := t.TempDir()
		handled, err := writeAgentHook([]string{"LaraEnv.exe", "--agent-hook", "claude"}, strings.NewReader(tc.payload), env, directory)
		if !handled || err != nil {
			t.Fatalf("%s: handled = %v, err = %v", tc.payload, handled, err)
		}
		got := drainHookSignals(t, directory)
		if len(got) != 1 {
			t.Fatalf("%s: signals = %#v", tc.payload, got)
		}
		s := got[0]
		if s.Kind != tc.kind || s.TerminalID != "term-4" || s.Provider != pkgagents.AgentClaude || s.ProviderSessionID != "s1" || s.Cwd != `C:\demo` || s.Source != tc.source || s.SessionID != "" {
			t.Fatalf("%s: unexpected signal %#v", tc.payload, s)
		}
	}
	// Unknown events and unknown notification types are ignored, not errors.
	directory := t.TempDir()
	if handled, err := writeAgentHook([]string{"LaraEnv.exe", "--agent-hook", "claude"}, strings.NewReader(`{"hook_event_name":"PreToolUse","session_id":"s1","cwd":"C:\\demo"}`), env, directory); !handled || err != nil {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	if got := drainHookSignals(t, directory); len(got) != 0 {
		t.Fatalf("unexpected signal for ignored event: %#v", got)
	}
}

func TestAgentHookMapsCodexAndKimiEvents(t *testing.T) {
	env := envWith(map[string]string{"LARAENV_TERMINAL_ID": "term-5"})
	for _, provider := range []string{"codex", "kimi"} {
		cases := map[string]pkgagents.SignalKind{
			`{"hook_event_name":"SessionStart","session_id":"t1","cwd":"C:\\demo","source":"startup"}`: pkgagents.SignalStarted,
			`{"hook_event_name":"UserPromptSubmit","session_id":"t1","cwd":"C:\\demo"}`:                 pkgagents.SignalWorking,
			`{"hook_event_name":"Stop","session_id":"t1","cwd":"C:\\demo","last_assistant_message":"x"}`: pkgagents.SignalDone,
			`{"hook_event_name":"PermissionRequest","session_id":"t1","cwd":"C:\\demo","tool_input":{}}`: pkgagents.SignalNeedsInput,
			`{"hook_event_name":"SessionEnd","session_id":"t1","cwd":"C:\\demo"}`:                        pkgagents.SignalEnded,
		}
		for payload, kind := range cases {
			directory := t.TempDir()
			handled, err := writeAgentHook([]string{"LaraEnv.exe", "--agent-hook", provider}, strings.NewReader(payload), env, directory)
			if !handled || err != nil {
				t.Fatalf("%s %s: handled = %v, err = %v", provider, payload, handled, err)
			}
			got := drainHookSignals(t, directory)
			if len(got) != 1 || got[0].Kind != kind || string(got[0].Provider) != provider || got[0].ProviderSessionID != "t1" {
				t.Fatalf("%s %s: unexpected signals %#v", provider, payload, got)
			}
		}
	}
}

func TestAgentHookMapsOpenCodeArguments(t *testing.T) {
	env := envWith(map[string]string{"LARAENV_TERMINAL_ID": "term-6"})
	cases := map[string]pkgagents.SignalKind{
		"session.created":  pkgagents.SignalStarted,
		"message.user":     pkgagents.SignalWorking,
		"session.idle":     pkgagents.SignalDone,
		"permission.asked": pkgagents.SignalNeedsInput,
		"session.deleted":  pkgagents.SignalEnded,
	}
	for event, kind := range cases {
		directory := t.TempDir()
		handled, err := writeAgentHook([]string{"LaraEnv.exe", "--agent-hook", "opencode", event, "ses_123", `C:\demo`}, nil, env, directory)
		if !handled || err != nil {
			t.Fatalf("%s: handled = %v, err = %v", event, handled, err)
		}
		got := drainHookSignals(t, directory)
		if len(got) != 1 || got[0].Kind != kind || got[0].Provider != pkgagents.AgentOpenCode || got[0].ProviderSessionID != "ses_123" || got[0].Cwd != `C:\demo` {
			t.Fatalf("%s: unexpected signals %#v", event, got)
		}
	}
	if handled, err := writeAgentHook([]string{"LaraEnv.exe", "--agent-hook", "opencode", "session.created"}, nil, env, t.TempDir()); !handled || err == nil {
		t.Fatalf("missing OpenCode arguments should be an error: handled = %v, err = %v", handled, err)
	}
}

func TestAgentHookRejectsUnknownProviderAndNonMatchingArgs(t *testing.T) {
	if handled, _ := writeAgentHook([]string{"LaraEnv.exe", "--cron-run", "x"}, nil, envWith(nil), t.TempDir()); handled {
		t.Fatal("unrelated arguments must not be handled")
	}
	handled, err := writeAgentHook([]string{"LaraEnv.exe", "--agent-hook", "vim"}, strings.NewReader(`{}`), envWith(map[string]string{"LARAENV_TERMINAL_ID": "term-1"}), t.TempDir())
	if !handled || err == nil {
		t.Fatalf("unknown provider: handled = %v, err = %v", handled, err)
	}
}
