package agents

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"
)

const ActivityHookInputLimit = 64 * 1024
const ActivitySignalLimit = 16 * 1024

// ActivityHookMetadata deliberately omits prompts, transcripts, tool contents,
// commands, paths, and provider-supplied ownership or timestamps.
type ActivityHookMetadata struct {
	Event           string          `json:"hook_event_name"`
	SessionID       string          `json:"session_id"`
	AgentID         string          `json:"agent_id"`
	AgentType       string          `json:"agent_type"`
	Model           string          `json:"model"`
	TurnID          string          `json:"turn_id"`
	BackgroundTasks json.RawMessage `json:"background_tasks"`
}

func ReadActivityHook(reader io.Reader) ([]byte, ActivityHookMetadata, error) {
	var payload ActivityHookMetadata
	if reader == nil {
		return nil, payload, errors.New("missing activity hook payload")
	}
	data, err := io.ReadAll(io.LimitReader(reader, ActivityHookInputLimit+1))
	if err != nil {
		return nil, payload, err
	}
	if len(data) > ActivityHookInputLimit {
		return nil, payload, errors.New("activity hook payload too large")
	}
	if err = json.Unmarshal(data, &payload); err != nil {
		return nil, payload, errors.New("invalid activity hook payload")
	}
	return data, payload, nil
}

func activityHookText(value string) string {
	out := make([]rune, 0, 240)
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		out = append(out, r)
		if len(out) == 240 {
			break
		}
	}
	return strings.TrimSpace(string(out))
}

func NormalizeActivityHook(provider AgentID, reader io.Reader, binding ActivityBinding) ([]ActivityEvent, error) {
	_, p, err := ReadActivityHook(reader)
	if err != nil {
		return nil, err
	}
	if (provider != AgentClaude && provider != AgentCodex) || binding.Provider != provider || !validActivityOwnerID(binding.OwnerID) || !safeSignalTerminalID.MatchString(binding.TerminalID) || !safeProviderSession.MatchString(binding.ProviderSessionID) || !safeProviderSession.MatchString(p.SessionID) {
		return nil, errors.New("unassociated activity hook")
	}
	if (binding.SessionID == "") != (binding.RunID == "") || (binding.SessionID != "" && (!safeSignalSessionID.MatchString(binding.SessionID) || !safeSignalSessionID.MatchString(binding.RunID))) {
		return nil, errors.New("invalid activity run")
	}
	base := ActivityEvent{OwnerID: binding.OwnerID, SessionID: binding.SessionID, RunID: binding.RunID, TerminalID: binding.TerminalID, Provider: provider, ProviderSessionID: binding.ProviderSessionID, ReceivedAt: time.Now().UTC()}
	events := []ActivityEvent{}
	switch p.Event {
	case "SubagentStart", "SubagentStop":
		if !safeProviderSession.MatchString(p.AgentID) || p.AgentID == binding.ProviderSessionID || p.AgentID == p.SessionID {
			return nil, errors.New("invalid subagent identity")
		}
		e := base
		e.NodeID = p.AgentID
		e.ParentProviderID = p.SessionID
		e.AgentType = activityHookText(p.AgentType)
		if p.TurnID != "" && safeProviderSession.MatchString(p.TurnID) {
			e.TurnID = p.TurnID
		}
		if name := activityHookText(p.Model); name != "" {
			e.Model = &ActivityModel{Name: name, Kind: ActivityModelObserved}
		}
		e.Kind = ActivitySubagentStart
		if p.Event == "SubagentStop" {
			e.Kind = ActivitySubagentTurnStop
		}
		events = append(events, e)
	}
	if (p.Event == "Stop" || p.Event == "SubagentStop") && len(p.BackgroundTasks) > 0 && string(p.BackgroundTasks) != "null" {
		var tasks []struct {
			ID          string `json:"id"`
			TaskID      string `json:"task_id"`
			Type        string `json:"type"`
			Description string `json:"description"`
			Status      string `json:"status"`
		}
		if err := json.Unmarshal(p.BackgroundTasks, &tasks); err != nil {
			return nil, errors.New("invalid background inventory")
		}
		out := []ActivityBackgroundTask{}
		e := base
		e.Kind = ActivityBackgroundInventory
		e.BackgroundTasks = &out
		seen := map[string]bool{}
		for _, task := range tasks {
			id := task.ID
			if id == "" {
				id = task.TaskID
			}
			// Subagent identities are supplied by dedicated events; checklists and
			// generic tool invocations are not background processes.
			switch task.Type {
			case "shell", "local_bash", "bash", "background_task", "remote", "monitor", "workflow", "teammate", "cloud session", "MCP task":
			case "subagent", "checklist", "todo":
				continue
			default:
				// Unknown task discriminants must not turn a partial inventory
				// into a claim that no background work exists.
				e.Truncated = true
				continue
			}
			if !safeProviderSession.MatchString(id) {
				e.Truncated = true
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			state := ActivityUnknown
			switch task.Status {
			case "running", "working":
				state = ActivityWorking
			case "completed", "done":
				state = ActivityStopped
			case "failed", "error":
				state = ActivityError
			case "killed", "cancelled":
				state = ActivityInterrupted
			}
			item := ActivityBackgroundTask{ProviderNodeID: id, Label: activityHookText(task.Description), AgentType: activityHookText(task.Type), State: state}
			out = append(out, item)
			encoded, _ := json.Marshal(e)
			if len(out) > 500 || len(encoded) > ActivitySignalLimit-1 {
				out = out[:len(out)-1]
				e.Truncated = true
				break
			}
		}
		events = append(events, e)
	}
	return events, nil
}
