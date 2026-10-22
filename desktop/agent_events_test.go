package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pkgagents "lara4ever/internal/agents"
)

func TestWriteAgentEventDiscardsPrivateProviderFields(t *testing.T) {
	directory := t.TempDir()
	handled, err := writeAgentEvent(
		[]string{"LaraEnv.exe", "--agent-event", "lara-session-1", "claude-notification"},
		strings.NewReader(`{"notification_type":"permission_prompt","message":"private message","transcript_path":"private path"}`),
		directory,
	)
	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("event files = %d, err = %v", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(directory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private") {
		t.Fatalf("private provider payload leaked into signal: %s", data)
	}
	var signals []pkgagents.SessionSignal
	if err := pkgagents.DrainSessionSignals(directory, func(signal pkgagents.SessionSignal) { signals = append(signals, signal) }); err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].Kind != pkgagents.SignalNeedsInput {
		t.Fatalf("unexpected signals: %#v", signals)
	}
}

func TestWriteAgentEventMapsCodexTurnCompletion(t *testing.T) {
	directory := t.TempDir()
	handled, err := writeAgentEvent(
		[]string{"LaraEnv.exe", "--agent-event", "lara-session-1", "codex-notify", `{"type":"agent-turn-complete","last-assistant-message":"private"}`},
		nil,
		directory,
	)
	if err != nil || !handled {
		t.Fatalf("handled = %v, err = %v", handled, err)
	}
	var signals []pkgagents.SessionSignal
	if err := pkgagents.DrainSessionSignals(directory, func(signal pkgagents.SessionSignal) { signals = append(signals, signal) }); err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 || signals[0].Kind != pkgagents.SignalDone {
		t.Fatalf("unexpected signals: %#v", signals)
	}
}
