package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseClaudeUsage(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	snapshot, err := parseClaudeUsage([]byte(`{"five_hour":{"utilization":0,"resets_at":"2026-09-07T17:00:00Z"},"seven_day":{"utilization":12.3,"resets_at":1788883200},"seven_day_opus":null,"limits":[{"kind":"weekly_scoped","percent":8,"resets_at":"1788883200000","scope":{"model":{"display_name":"Fable"}}}]}`), now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Provider != AgentClaude || snapshot.Status != "available" || len(snapshot.Windows) != 3 {
		t.Fatalf("unexpected Claude windows: %#v", snapshot)
	}
	if *snapshot.Windows[0].UsedPct != 0 || *snapshot.Windows[1].UsedPct != 12.3 || snapshot.Windows[2].Label != "Fable weekly" || *snapshot.Windows[2].UsedPct != 8 {
		t.Fatalf("incorrect provider percentages: %#v", snapshot.Windows)
	}
	if snapshot.Windows[0].ResetsAt == nil || snapshot.Windows[0].ResetsAt.Format(time.RFC3339) != "2026-09-07T17:00:00Z" || snapshot.Windows[1].ResetsAt.Unix() != snapshot.Windows[2].ResetsAt.Unix() {
		t.Fatalf("incorrect reset times: %#v", snapshot.Windows)
	}
}

func TestParseClaudeUsageMissingOrMalformedWindows(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"five_hour":null}`, `{"five_hour":{"utilization":null}}`, `{"five_hour":{"utilization":"unknown"}}`} {
		snapshot, err := parseClaudeUsage([]byte(data), time.Now())
		if err != nil || snapshot.Status != "unavailable" || len(snapshot.Windows) != 0 {
			t.Fatalf("missing values must not become a zero quota: input=%s snapshot=%#v err=%v", data, snapshot, err)
		}
	}
	if _, err := parseClaudeUsage([]byte(`{"invalid":`), time.Now()); err == nil {
		t.Fatal("expected invalid JSON to fail")
	}
	snapshot, err := parseClaudeUsage([]byte(`{"five_hour":{"used_percentage":104,"resets_at":"unknown"},"fable_weekly":{"utilization":7},"limits":[{"kind":"weekly_scoped","percent":7,"scope":{"model":{"display_name":"Fable"}}}]}`), time.Now())
	if err != nil || len(snapshot.Windows) != 2 || *snapshot.Windows[0].UsedPct != 100 || snapshot.Windows[0].ResetsAt != nil {
		t.Fatalf("unexpected optional window handling: %#v, %v", snapshot, err)
	}
}

func TestClaudeUsageReaderCachesAndHonorsRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requests++
		if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer test-token" || req.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			t.Error("incorrect read-only OAuth request")
		}
		if requests == 1 {
			fmt.Fprint(w, `{"five_hour":{"utilization":23}}`)
			return
		}
		w.Header().Set("Retry-After", "900")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "secret-provider-error-test-token")
	}))
	defer server.Close()
	reader := newClaudeUsageReader()
	reader.endpoint, reader.client = server.URL, server.Client()
	reader.clock = func() time.Time { return now }
	reader.credentials = func() ([]byte, error) {
		return []byte(`{"claudeAiOauth":{"accessToken":"test-token","subscriptionType":"max"}}`), nil
	}
	first := reader.Read(context.Background())
	if first.Plan != "max" || first.Status != "available" || first.Stale {
		t.Fatalf("unexpected first snapshot: %#v", first)
	}
	now = now.Add(time.Minute)
	reader.Read(context.Background())
	if requests != 1 {
		t.Fatal("background/manual refresh bypassed the cache")
	}
	now = now.Add(4 * time.Minute)
	stale := reader.Read(context.Background())
	if !stale.Stale || stale.ReadAt != first.ReadAt || *stale.Windows[0].UsedPct != 23 {
		t.Fatalf("last known usage was not preserved: %#v", stale)
	}
	encoded, _ := json.Marshal(stale)
	if strings.Contains(string(encoded), "test-token") || strings.Contains(string(encoded), "secret-provider") {
		t.Fatal("provider error or credential leaked into the snapshot")
	}
	now = now.Add(10 * time.Minute)
	reader.Read(context.Background())
	if requests != 2 {
		t.Fatal("Retry-After was ignored")
	}
	now = now.Add(5 * time.Minute)
	reader.Read(context.Background())
	if requests != 3 {
		t.Fatal("reader did not retry after the cooldown")
	}
}

func TestClaudeUsageReaderClearsOldQuotaOnAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "secret")
	}))
	defer server.Close()
	reader := newClaudeUsageReader()
	reader.endpoint, reader.client = server.URL, server.Client()
	reader.credentials = func() ([]byte, error) { return []byte(`{"claudeAiOauth":{"accessToken":"test-token"}}`), nil }
	percent := 99.0
	reader.last = UsageSnapshot{Status: "available", Windows: []UsageWindow{{UsedPct: &percent}}}
	snapshot := reader.Read(context.Background())
	if snapshot.Status != "unavailable" || len(snapshot.Windows) != 0 || snapshot.Stale {
		t.Fatalf("expired credentials retained old account quota: %#v", snapshot)
	}
}

func TestClaudeUsageReaderDoesNotFollowRedirect(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetRequests++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	reader := newClaudeUsageReader()
	reader.endpoint = server.URL
	reader.credentials = func() ([]byte, error) { return []byte(`{"claudeAiOauth":{"accessToken":"test-token"}}`), nil }
	snapshot := reader.Read(context.Background())
	if targetRequests != 0 || snapshot.Status != "error" {
		t.Fatal("usage credential request followed a redirect")
	}
}

func TestClaudeUsageCredentialsRespectConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	data := []byte(`{"claudeAiOauth":{"accessToken":"test-token"}}`)
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	read, err := readClaudeUsageCredentials()
	if err != nil || string(read) != string(data) {
		t.Fatal("did not read credentials from CLAUDE_CONFIG_DIR")
	}
}

func TestReadClaudeUsageLive(t *testing.T) {
	if os.Getenv("LARAENV_LIVE_CLAUDE_USAGE_TEST") != "1" {
		t.Skip("set LARAENV_LIVE_CLAUDE_USAGE_TEST=1 to read the installed Claude Code subscription quota")
	}
	snapshot := newClaudeUsageReader().Read(context.Background())
	if snapshot.Status != "available" || len(snapshot.Windows) == 0 {
		t.Fatalf("Claude usage status=%s, message=%s", snapshot.Status, snapshot.Message)
	}
	for _, window := range snapshot.Windows {
		t.Logf("%s: %.1f%% used", window.Label, *window.UsedPct)
	}
}
