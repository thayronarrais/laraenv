package agents

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"lara4ever/internal/i18n"
)

type codexRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type codexRateWindow struct {
	UsedPercent       *float64 `json:"usedPercent"`
	WindowDurationMin int      `json:"windowDurationMins"`
	ResetsAt          *int64   `json:"resetsAt"`
}

type codexRateLimits struct {
	Primary   *codexRateWindow `json:"primary"`
	Secondary *codexRateWindow `json:"secondary"`
	PlanType  string           `json:"planType"`
}

type codexRateResult struct {
	RateLimits          *codexRateLimits           `json:"rateLimits"`
	RateLimitsByLimitID map[string]codexRateLimits `json:"rateLimitsByLimitId"`
}

func ReadCodexUsage(ctx context.Context, executable string) (UsageSnapshot, error) {
	if executable == "" {
		return UsageSnapshot{}, i18n.Errorf("errors.agents.codexExecutableUnavailable", nil)
	}
	command := exec.CommandContext(ctx, executable, "app-server", "--stdio")
	hideUsageWindow(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return UsageSnapshot{}, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return UsageSnapshot{}, err
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return UsageSnapshot{}, err
	}
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()

	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	if err := encoder.Encode(map[string]any{
		"method": "initialize", "id": 1,
		"params": map[string]any{"clientInfo": map[string]string{"name": "laraenv", "title": "LaraEnv", "version": "1"}, "capabilities": map[string]any{}},
	}); err != nil {
		return UsageSnapshot{}, err
	}
	if _, err := scanCodexResponse(scanner, 1); err != nil {
		return UsageSnapshot{}, i18n.Wrapf(err, "errors.agents.initializeCodexUsageFailed", nil)
	}
	if err := encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return UsageSnapshot{}, err
	}
	if err := encoder.Encode(map[string]any{"method": "account/rateLimits/read", "id": 2}); err != nil {
		return UsageSnapshot{}, err
	}
	response, err := scanCodexResponse(scanner, 2)
	if err != nil {
		if stderr.Len() > 0 {
			return UsageSnapshot{}, i18n.Wrapf(err, "errors.agents.codexRpcStderr", map[string]any{"stderr": string(bytes.TrimSpace(stderr.Bytes()))})
		}
		return UsageSnapshot{}, err
	}
	return parseCodexUsage(response.Result, time.Now().UTC())
}

func scanCodexResponse(scanner *bufio.Scanner, id int) (codexRPCResponse, error) {
	for scanner.Scan() {
		var response codexRPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil || response.ID != id {
			continue
		}
		if response.Error != nil {
			return codexRPCResponse{}, i18n.Errorf("errors.agents.codexRpcError", map[string]any{"code": response.Error.Code, "message": response.Error.Message})
		}
		return response, nil
	}
	if err := scanner.Err(); err != nil {
		return codexRPCResponse{}, err
	}
	return codexRPCResponse{}, i18n.Errorf("errors.agents.codexAppServerClosed", nil)
}

func parseCodexUsage(data []byte, readAt time.Time) (UsageSnapshot, error) {
	var result codexRateResult
	if err := json.Unmarshal(data, &result); err != nil {
		return UsageSnapshot{}, i18n.Wrapf(err, "errors.agents.decodeCodexRateLimitsFailed", nil)
	}
	limits := result.RateLimits
	if byID, ok := result.RateLimitsByLimitID["codex"]; ok {
		limits = &byID
	}
	if limits == nil {
		return UsageSnapshot{Provider: AgentCodex, Status: "unavailable", Message: i18n.T("backend.agents.codexNoChatGptRateLimitWindows", nil), ReadAt: readAt}, nil
	}
	snapshot := UsageSnapshot{Provider: AgentCodex, Plan: limits.PlanType, Status: "available", ReadAt: readAt}
	for index, window := range []*codexRateWindow{limits.Primary, limits.Secondary} {
		if window == nil {
			continue
		}
		usageWindow := UsageWindow{ID: []string{"primary", "secondary"}[index], Label: codexWindowLabel(window.WindowDurationMin), UsedPct: window.UsedPercent}
		if window.ResetsAt != nil && *window.ResetsAt > 0 {
			reset := time.Unix(*window.ResetsAt, 0).UTC()
			usageWindow.ResetsAt = &reset
		}
		snapshot.Windows = append(snapshot.Windows, usageWindow)
	}
	if len(snapshot.Windows) == 0 {
		snapshot.Status = "unavailable"
		snapshot.Message = i18n.T("backend.agents.codexNoRateLimitWindows", nil)
	}
	return snapshot, nil
}

func codexWindowLabel(minutes int) string {
	switch minutes {
	case 300:
		return i18n.T("backend.agents.windowLabelFiveHour", nil)
	case 10080:
		return i18n.T("backend.agents.windowLabelWeekly", nil)
	case 43200, 43800, 44640:
		return i18n.T("backend.agents.windowLabelMonthly", nil)
	case 0:
		return i18n.T("backend.agents.windowLabelLimit", nil)
	default:
		return i18n.T("backend.agents.windowLabelMinutes", map[string]any{"minutes": minutes})
	}
}
