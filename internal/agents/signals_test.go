package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionSignalRoundTripContainsOnlyEnvelope(t *testing.T) {
	directory := t.TempDir()
	if err := WriteSessionSignal(directory, "lara-session-1", SignalNeedsInput); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("signal files = %d, err = %v", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 512 {
		t.Fatalf("signal envelope unexpectedly large: %d bytes", len(data))
	}
	var got []SessionSignal
	if err := DrainSessionSignals(directory, func(signal SessionSignal) { got = append(got, signal) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionID != "lara-session-1" || got[0].Kind != SignalNeedsInput {
		t.Fatalf("unexpected signals: %#v", got)
	}
	entries, err = os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("signal was not consumed: %v, err = %v", entries, err)
	}
}

func TestSessionSignalRejectsUnsafeInputAndConsumesMalformedFiles(t *testing.T) {
	directory := t.TempDir()
	if err := WriteSessionSignal(directory, `..\outside`, SignalDone); err == nil {
		t.Fatal("unsafe session ID should be rejected")
	}
	bad := filepath.Join(directory, "malformed.json")
	if err := os.WriteFile(bad, []byte(`{"sessionId":"x","kind":"unknown"}`), 0644); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := DrainSessionSignals(directory, func(SessionSignal) { called = true }); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("malformed signal reached the manager")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal("malformed signal was not consumed")
	}
}

func TestAdoptedSignalRoundTripKeepsTerminalAndProviderFields(t *testing.T) {
	directory := t.TempDir()
	err := WriteSignal(directory, SessionSignal{
		Kind: SignalStarted, TerminalID: "term-3", Provider: AgentClaude,
		ProviderSessionID: "0f1e2d3c-1111-2222-3333-444455556666", Cwd: `C:\laraenv\www\demo`, Source: "startup",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []SessionSignal
	if err := DrainSessionSignals(directory, func(signal SessionSignal) { got = append(got, signal) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("signals = %#v", got)
	}
	s := got[0]
	if s.SessionID != "" || s.Kind != SignalStarted || s.TerminalID != "term-3" || s.Provider != AgentClaude ||
		s.ProviderSessionID != "0f1e2d3c-1111-2222-3333-444455556666" || s.Cwd != `C:\laraenv\www\demo` || s.Source != "startup" || s.CreatedAt.IsZero() {
		t.Fatalf("unexpected adopted signal: %#v", s)
	}
}

func TestSignalValidationRejectsUnsafeAdoptedFields(t *testing.T) {
	base := SessionSignal{Kind: SignalStarted, TerminalID: "term-3", Provider: AgentCodex, ProviderSessionID: "abc", Cwd: `C:\x`}
	cases := map[string]SessionSignal{
		"no ids":           {Kind: SignalDone},
		"bad terminal":     func() SessionSignal { s := base; s.TerminalID = `..\x`; return s }(),
		"unknown provider": func() SessionSignal { s := base; s.Provider = "vim"; return s }(),
		"bad provider id":  func() SessionSignal { s := base; s.ProviderSessionID = "a b"; return s }(),
		"control in cwd":   func() SessionSignal { s := base; s.Cwd = "C:\\x\n"; return s }(),
		"escape in cwd":    func() SessionSignal { s := base; s.Cwd = "C:\\x\x1b[31m"; return s }(),
		"tab in cwd":       func() SessionSignal { s := base; s.Cwd = "C:\\x\ty"; return s }(),
		"unknown kind":     func() SessionSignal { s := base; s.Kind = "nope"; return s }(),
	}
	for name, signal := range cases {
		if validSignal(signal) {
			t.Fatalf("%s: signal should be rejected: %#v", name, signal)
		}
	}
	if !validSignal(base) || !validSignal(SessionSignal{SessionID: "lara-1", Kind: SignalDone}) {
		t.Fatal("valid signals were rejected")
	}
	if err := WriteSignal(t.TempDir(), cases["bad terminal"]); err == nil {
		t.Fatal("WriteSignal must reject invalid envelopes")
	}
}
