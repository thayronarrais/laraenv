package agents

import (
	"strings"
	"testing"
)

func TestLifecycleSignalsAreTransientProviderArguments(t *testing.T) {
	base := Invocation{Executable: `C:\tools\claude.exe`, Cwd: `C:\projects\demo`, Args: []string{"--resume", "provider-id"}}
	claude, err := addLifecycleSignals(base, AgentClaude, "lara-session-1", `C:\Program Files\LaraEnv\LaraEnv.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if len(claude.Args) != 4 || claude.Args[0] != "--settings" || claude.Args[2] != "--resume" {
		t.Fatalf("unexpected Claude args: %#v", claude.Args)
	}
	if !strings.Contains(claude.Args[1], `"Stop"`) || !strings.Contains(claude.Args[1], "claude-notification") {
		t.Fatalf("Claude lifecycle settings missing: %s", claude.Args[1])
	}

	codex, err := addLifecycleSignals(Invocation{Args: []string{"resume", "provider-id"}}, AgentCodex, "lara-session-1", `C:\LaraEnv.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if len(codex.Args) != 4 || codex.Args[0] != "-c" || !strings.HasPrefix(codex.Args[1], "notify=") || codex.Args[2] != "resume" {
		t.Fatalf("unexpected Codex args: %#v", codex.Args)
	}
}

func TestLifecycleSignalsLeaveUnsupportedProvidersUntouched(t *testing.T) {
	base := Invocation{Args: []string{"--session", "provider-id"}}
	got, err := addLifecycleSignals(base, AgentKimi, "lara-session-1", `C:\LaraEnv.exe`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Args) != 2 || got.Args[0] != "--session" {
		t.Fatalf("unsupported provider args changed: %#v", got.Args)
	}
}
