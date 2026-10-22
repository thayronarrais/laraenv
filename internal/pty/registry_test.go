package pty

import (
	"strings"
	"testing"
)

func TestApplyEnvOverridesReplacesAndRemovesCaseInsensitively(t *testing.T) {
	env := []string{"Path=C:\\tools", "TERM=dumb", "NO_COLOR=1", "KEEP=yes"}
	got := applyEnvOverrides(env, map[string]string{
		"TERM":         "xterm-256color",
		"COLORTERM":    "truecolor",
		"TERM_PROGRAM": "LaraEnv",
		"NO_COLOR":     "",
	})
	joined := strings.Join(got, "\n")
	for _, expected := range []string{"TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=LaraEnv", "KEEP=yes"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("environment missing %q: %#v", expected, got)
		}
	}
	if strings.Contains(strings.ToUpper(joined), "NO_COLOR=") || strings.Contains(joined, "TERM=dumb") {
		t.Fatalf("terminal color opt-out leaked into command: %#v", got)
	}
}

func TestSessionEnvCarriesTerminalIDAndOverrides(t *testing.T) {
	got := sessionEnv("term-7", nil, map[string]string{"TERM": "xterm-256color"})
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, TerminalIDEnv+"=term-7") {
		t.Fatalf("environment missing terminal id: %#v", got)
	}
	if !strings.Contains(joined, "TERM=xterm-256color") {
		t.Fatalf("overrides were not applied: %#v", got)
	}
	// Overrides must not be able to spoof the terminal id.
	got = sessionEnv("term-8", nil, map[string]string{TerminalIDEnv: "spoofed"})
	if strings.Contains(strings.Join(got, "\n"), "spoofed") {
		t.Fatalf("override replaced the terminal id: %#v", got)
	}
}

func TestRegistryOwnerEnvironment(t *testing.T) {
	r := NewRegistry()
	r.SetOwnerID("owner-local")
	got := r.sessionEnvironment("term-9", nil, map[string]string{
		"laraenv_agent_owner": "spoof-owner", "laraenv_terminal_id": "spoof-terminal",
		"LARAENV_AGENT_RUN": "run-1", "TERM": "xterm-256color",
	})
	joined := strings.Join(got, "\n")
	for _, expected := range []string{"LARAENV_AGENT_OWNER=owner-local", "LARAENV_TERMINAL_ID=term-9", "LARAENV_AGENT_RUN=run-1", "TERM=xterm-256color"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %s in %v", expected, got)
		}
	}
	if strings.Contains(joined, "spoof-") {
		t.Fatal("identity override accepted", got)
	}
	r.SetOwnerID("")
	got = r.sessionEnvironment("term-10", nil, map[string]string{"LARAENV_AGENT_OWNER": "spoof-owner"})
	if strings.Contains(strings.Join(got, "\n"), "LARAENV_AGENT_OWNER=") {
		t.Fatal("unconfigured owner inherited/spoofed", got)
	}
}

func TestRegistryActivityTerminalObserver(t *testing.T) {
	r := NewRegistry()
	var events []string
	r.SetActivityTerminalObserver(func(id string, live bool) {
		_, exists := r.Get(id)
		if exists != live {
			t.Error("observer invoked before registry mutation")
		}
		if live {
			events = append(events, id+":live")
		} else {
			events = append(events, id+":closed")
		}
	})
	one := r.Register(&fakePlainSession{fakeWaitSession: *newFakeWaitSession()})
	two := r.Register(&fakePlainSession{fakeWaitSession: *newFakeWaitSession()})
	if err := r.Close(one); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(one); err != nil {
		t.Fatal(err)
	}
	r.CloseAll()
	r.CloseAll()
	if strings.Join(events, ",") != one+":live,"+two+":live,"+one+":closed,"+two+":closed" {
		t.Fatal(events)
	}
}
