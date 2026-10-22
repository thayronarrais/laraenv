package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/i18n"
)

const claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
const claudeUsagePollInterval = 5 * time.Minute

// The Claude Code OAuth usage endpoint is also used by Orca. It is not a public
// stable API. Keep this reader read-only: Claude Code owns login/token rotation.
type claudeUsageReader struct {
	mu          sync.Mutex
	client      *http.Client
	endpoint    string
	credentials func() ([]byte, error)
	last        UsageSnapshot
	nextAttempt time.Time
	clock       func() time.Time
}

func newClaudeUsageReader() *claudeUsageReader {
	return &claudeUsageReader{
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse // Never send the account credential to another endpoint.
		}},
		endpoint: claudeUsageURL, credentials: readClaudeUsageCredentials, clock: time.Now,
	}
}

func readClaudeUsageCredentials() ([]byte, error) {
	configDir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, i18n.Errorf("errors.agents.claudeConfigDirUnavailable", nil)
		}
		configDir = filepath.Join(home, ".claude")
	}
	// Read only the provider's own credential file; never copy it into app data,
	// session metadata, frontend responses, error details or logs.
	return os.ReadFile(filepath.Join(configDir, ".credentials.json"))
}

func (r *claudeUsageReader) Read(ctx context.Context) UsageSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock().UTC()
	if now.Before(r.nextAttempt) {
		return r.last
	}
	r.nextAttempt = now.Add(claudeUsagePollInterval)
	snapshot, retryAt := r.fetch(ctx, now)
	if retryAt.After(r.nextAttempt) {
		r.nextAttempt = retryAt
	}
	// Retain provider-reported values during transient failures, with an explicit
	// stale marker. Auth/unavailable results clear old account data.
	if snapshot.Status == "error" && r.last.Status == "available" {
		previous := r.last
		previous.Stale = true
		previous.Message = snapshot.Message
		snapshot = previous
	}
	r.last = snapshot
	return snapshot
}

func (r *claudeUsageReader) fetch(ctx context.Context, now time.Time) (UsageSnapshot, time.Time) {
	snapshot := UsageSnapshot{Provider: AgentClaude, Status: "unavailable", ReadAt: now, Windows: []UsageWindow{}}
	data, err := r.credentials()
	if err != nil {
		snapshot.Message = i18n.T("backend.agents.claudeSignInRequired", nil)
		return snapshot, time.Time{}
	}
	var credentials struct {
		OAuth struct {
			AccessToken      string `json:"accessToken"`
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &credentials) != nil || strings.TrimSpace(credentials.OAuth.AccessToken) == "" {
		snapshot.Message = i18n.T("backend.agents.claudeCredentialsUnavailable", nil)
		return snapshot, time.Time{}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.endpoint, nil)
	if err != nil {
		snapshot.Status, snapshot.Message = "error", i18n.T("backend.agents.claudeCannotPrepareRequest", nil)
		return snapshot, time.Time{}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.OAuth.AccessToken)
	request.Header.Set("anthropic-beta", "oauth-2025-04-20")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "LaraEnv")
	response, err := r.client.Do(request)
	if err != nil {
		snapshot.Status, snapshot.Message = "error", i18n.T("backend.agents.claudeCouldNotReachLimits", nil)
		return snapshot, time.Time{}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			snapshot.Message = i18n.T("backend.agents.claudeMustRenewLogin", nil)
		case http.StatusTooManyRequests:
			snapshot.Status, snapshot.Message = "error", i18n.T("backend.agents.claudeRateLimited", nil)
		default:
			snapshot.Status, snapshot.Message = "error", i18n.T("backend.agents.claudeTemporarilyUnavailable", map[string]any{"status": response.StatusCode})
		}
		// Never surface the response body: provider errors can contain auth details.
		return snapshot, claudeUsageRetryAt(response.Header.Get("Retry-After"), now)
	}
	const maxUsageBytes = 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxUsageBytes+1))
	if err != nil || len(body) > maxUsageBytes {
		snapshot.Status, snapshot.Message = "error", i18n.T("backend.agents.claudeCouldNotReadLimits", nil)
		return snapshot, time.Time{}
	}
	parsed, err := parseClaudeUsage(body, now)
	if err != nil {
		snapshot.Status, snapshot.Message = "error", i18n.T("backend.agents.claudeUnreadableResponse", nil)
		return snapshot, time.Time{}
	}
	parsed.Plan = credentials.OAuth.SubscriptionType
	return parsed, time.Time{}
}

type claudeRateWindow struct {
	Utilization    *float64        `json:"utilization"`
	UsedPercentage *float64        `json:"used_percentage"`
	ResetsAt       json.RawMessage `json:"resets_at"`
}

func parseClaudeUsage(data []byte, readAt time.Time) (UsageSnapshot, error) {
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return UsageSnapshot{}, i18n.Errorf("errors.agents.decodeClaudeUsageFailed", nil)
	}
	snapshot := UsageSnapshot{Provider: AgentClaude, Status: "unavailable", ReadAt: readAt, Windows: []UsageWindow{}}
	// Additional subscription buckets are optional and vary by plan/model.
	// labelKey names a catalogue key under backend.agents.* rather than the
	// literal display text, so the window label the UI shows is translated.
	for _, entry := range []struct{ id, labelKey string }{
		{"five_hour", "backend.agents.windowLabelFiveHour"}, {"seven_day", "backend.agents.windowLabelWeekly"},
		{"seven_day_sonnet", "backend.agents.windowLabelSonnetWeekly"}, {"seven_day_opus", "backend.agents.windowLabelOpusWeekly"},
		{"seven_day_fable", "backend.agents.windowLabelFableWeekly"}, {"fable_seven_day", "backend.agents.windowLabelFableWeekly"}, {"fable_weekly", "backend.agents.windowLabelFableWeekly"},
	} {
		var raw claudeRateWindow
		if len(result[entry.id]) == 0 || json.Unmarshal(result[entry.id], &raw) != nil {
			continue
		}
		label := i18n.T(entry.labelKey, nil)
		window := claudeWindow(entry.id, label, raw)
		if window != nil && !hasClaudeWindowLabel(snapshot.Windows, label) {
			snapshot.Windows = append(snapshot.Windows, *window)
		}
	}
	var scoped []struct {
		Kind     string          `json:"kind"`
		Percent  *float64        `json:"percent"`
		ResetsAt json.RawMessage `json:"resets_at"`
		Scope    struct {
			Model struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	}
	if len(result["limits"]) > 0 && json.Unmarshal(result["limits"], &scoped) == nil {
		for index, limit := range scoped {
			name := strings.TrimSpace(limit.Scope.Model.DisplayName)
			if limit.Kind != "weekly_scoped" || name == "" {
				continue
			}
			label := i18n.T("backend.agents.windowLabelModelWeekly", map[string]any{"model": name})
			window := claudeWindow(fmt.Sprintf("weekly_scoped_%d", index), label, claudeRateWindow{UsedPercentage: limit.Percent, ResetsAt: limit.ResetsAt})
			if window != nil && !hasClaudeWindowLabel(snapshot.Windows, label) {
				snapshot.Windows = append(snapshot.Windows, *window)
			}
		}
	}
	if len(snapshot.Windows) > 0 {
		snapshot.Status = "available"
	} else {
		snapshot.Message = i18n.T("backend.agents.claudeNoLimitWindows", nil)
	}
	return snapshot, nil
}

func hasClaudeWindowLabel(windows []UsageWindow, label string) bool {
	for _, window := range windows {
		if strings.EqualFold(window.Label, label) {
			return true
		}
	}
	return false
}

func claudeWindow(id, label string, raw claudeRateWindow) *UsageWindow {
	used := raw.Utilization
	if used == nil {
		used = raw.UsedPercentage
	}
	if used == nil || math.IsNaN(*used) || math.IsInf(*used, 0) {
		return nil
	}
	percent := math.Max(0, math.Min(100, *used))
	return &UsageWindow{ID: id, Label: label, UsedPct: &percent, ResetsAt: parseClaudeReset(raw.ResetsAt)}
}

func parseClaudeReset(raw json.RawMessage) *time.Time {
	value := strings.Trim(string(raw), "\"")
	if value == "" || value == "null" {
		return nil
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) && number > 0 && number < 1e15 {
		if number > 1e10 {
			number /= 1000
		}
		reset := time.Unix(int64(number), 0).UTC()
		return &reset
	}
	if reset, err := time.Parse(time.RFC3339Nano, value); err == nil {
		reset = reset.UTC()
		return &reset
	}
	return nil
}

func claudeUsageRetryAt(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds > 0 && seconds <= 86400 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if retry, err := http.ParseTime(value); err == nil && retry.After(now) {
		return retry
	}
	return now.Add(claudeUsagePollInterval)
}
