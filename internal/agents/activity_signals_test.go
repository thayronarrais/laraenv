package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActivitySignalOwnerRunAndSizeValidation(t *testing.T) {
	b := activityHookBinding()
	event := ActivityEvent{OwnerID: b.OwnerID, SessionID: b.SessionID, RunID: b.RunID, TerminalID: b.TerminalID, Provider: b.Provider, ProviderSessionID: b.ProviderSessionID, NodeID: "child-1", Kind: ActivitySubagentStart, ReceivedAt: time.Now().UTC()}
	dir := t.TempDir()
	if err := WriteActivitySignal(dir, event); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := DrainActivitySignals(dir, func(e ActivityEvent) {
		count++
		if e.RunID != b.RunID || e.OwnerID != b.OwnerID {
			t.Fatal(e)
		}
	}); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, change := range []func(*ActivityEvent){func(e *ActivityEvent) { e.OwnerID = "" }, func(e *ActivityEvent) { e.RunID = "" }, func(e *ActivityEvent) { e.AgentType = strings.Repeat("x", 17000) }, func(e *ActivityEvent) { e.ReceivedAt = time.Now().Add(-time.Minute) }} {
		bad := event
		change(&bad)
		if err := WriteActivitySignal(dir, bad); err == nil {
			t.Fatal("invalid envelope accepted", bad.Kind)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.json"), []byte(strings.Repeat("x", 17000)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := DrainActivitySignals(dir, func(ActivityEvent) { t.Fatal("invalid persisted envelope accepted") }); err != nil {
		t.Fatal(err)
	}
}
