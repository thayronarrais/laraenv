package agents

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestParseCodexUsagePrefersNamedCodexBucket(t *testing.T) {
	used := 31.0
	reset := int64(1784246400)
	data := []byte(`{"rateLimits":{"primary":{"usedPercent":99,"windowDurationMins":15}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":31,"windowDurationMins":10080,"resetsAt":1784246400},"secondary":null,"planType":"plus"}}}`)
	snapshot, err := parseCodexUsage(data, time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "available" || snapshot.Plan != "plus" || len(snapshot.Windows) != 1 || snapshot.Windows[0].Label != "Weekly" || snapshot.Windows[0].UsedPct == nil || *snapshot.Windows[0].UsedPct != used || snapshot.Windows[0].ResetsAt == nil || snapshot.Windows[0].ResetsAt.Unix() != reset {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestReadCodexUsageLive(t *testing.T) {
	if os.Getenv("LARAENV_LIVE_USAGE_TEST") != "1" {
		t.Skip("set LARAENV_LIVE_USAGE_TEST=1 to exercise the installed Codex app-server")
	}
	executable, err := exec.LookPath("codex")
	if err != nil {
		t.Skip("Codex is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := ReadCodexUsage(ctx, executable)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "available" || len(snapshot.Windows) == 0 {
		t.Fatalf("Codex returned no usable rate-limit windows (status=%s)", snapshot.Status)
	}
}

func TestParseCodexUsagePreservesMissingWindows(t *testing.T) {
	snapshot, err := parseCodexUsage([]byte(`{"rateLimits":null}`), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "unavailable" || len(snapshot.Windows) != 0 {
		t.Fatalf("missing limits were presented as known: %#v", snapshot)
	}
}

func TestUsageManagerSerializesUnsupportedWindowsAsArray(t *testing.T) {
	catalog := newCatalog(
		func(string) (string, error) { return "", exec.ErrNotFound },
		func(context.Context, string) (string, error) { return "", nil },
		catalogDirs{},
	)

	snapshots := NewUsageManager(catalog).Refresh(context.Background())
	if len(snapshots) == 0 {
		t.Fatal("expected built-in provider snapshots")
	}
	for _, snapshot := range snapshots {
		if snapshot.Windows == nil {
			t.Fatalf("usage windows must be a non-nil empty slice: %#v", snapshots)
		}
	}
	data, err := json.Marshal(snapshots)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data) == "null" {
		t.Fatalf("unexpected usage JSON: %s", data)
	}
	var wire []struct {
		Windows json.RawMessage `json:"windows"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range wire {
		if string(snapshot.Windows) != "[]" {
			t.Fatalf("usage windows serialized as %s, want []", snapshot.Windows)
		}
	}
}
