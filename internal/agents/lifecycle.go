package agents

import (
	"encoding/json"
	"strings"

	"lara4ever/internal/i18n"
)

type ActivityLifecycleIdentity struct{ RunID, OwnerID string }

func addLifecycleSignals(invocation Invocation, provider AgentID, laraSessionID, eventExecutable string, identity ...ActivityLifecycleIdentity) (Invocation, error) {
	if strings.TrimSpace(eventExecutable) == "" || !safeSignalSessionID.MatchString(laraSessionID) {
		return invocation, nil
	}
	if strings.ContainsAny(eventExecutable, "\"\r\n") {
		return Invocation{}, i18n.Errorf("errors.agents.executablePathUnsafeForLifecycleHooks", nil)
	}

	var fence []string
	if len(identity) > 0 && identity[0].RunID != "" && identity[0].OwnerID != "" {
		if !safeSignalSessionID.MatchString(identity[0].RunID) || !validActivityOwnerID(identity[0].OwnerID) {
			return Invocation{}, i18n.Errorf("errors.agents.invalidSignalEnvelope", nil)
		}
		fence = []string{"--activity-run", identity[0].RunID, "--activity-owner", identity[0].OwnerID}
	}
	switch provider {
	case AgentClaude:
		stopCommand := lifecycleCommand(eventExecutable, laraSessionID, "claude-stop")
		notificationCommand := lifecycleCommand(eventExecutable, laraSessionID, "claude-notification")
		if len(fence) > 0 {
			stopCommand += " " + strings.Join(fence, " ")
			notificationCommand += " " + strings.Join(fence, " ")
		}
		settings := map[string]any{
			"hooks": map[string]any{
				"Stop": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": stopCommand}}}},
				"Notification": []any{map[string]any{
					"matcher": "permission_prompt|idle_prompt",
					"hooks":   []any{map[string]any{"type": "command", "command": notificationCommand}},
				}},
			},
		}
		encoded, err := json.Marshal(settings)
		if err != nil {
			return Invocation{}, err
		}
		invocation.Args = append([]string{"--settings", string(encoded)}, invocation.Args...)
	case AgentCodex:
		command := []string{eventExecutable, "--agent-event", laraSessionID, "codex-notify"}
		command = append(command, fence...)
		encoded, err := json.Marshal(command)
		if err != nil {
			return Invocation{}, err
		}
		invocation.Args = append([]string{"-c", "notify=" + string(encoded)}, invocation.Args...)
	}
	return invocation, nil
}

// Claude accepts transient settings. Codex uses the stable installed handler;
// no trust flags, daemon settings, or per-run command hashes are changed here.
func addActivitySignals(invocation Invocation, provider AgentID, eventExecutable string) (Invocation, error) {
	if provider != AgentClaude || strings.TrimSpace(eventExecutable) == "" {
		return invocation, nil
	}
	if err := validateHookExecutable(eventExecutable); err != nil {
		return Invocation{}, err
	}
	settings := map[string]any{}
	index := -1
	for i := 0; i+1 < len(invocation.Args); i++ {
		if invocation.Args[i] == "--settings" && strings.HasPrefix(strings.TrimSpace(invocation.Args[i+1]), "{") {
			index = i + 1
			if err := json.Unmarshal([]byte(invocation.Args[index]), &settings); err != nil {
				return Invocation{}, err
			}
			break
		}
	}
	if err := validateJSONHooksShape(settings, "transient settings"); err != nil {
		return Invocation{}, err
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	command := `"` + eventExecutable + `" --agent-activity claude`
	for _, event := range []string{"SubagentStart", "SubagentStop", "Stop"} {
		entries, _ := hooks[event].([]any)
		found := false
		for _, entry := range entries {
			group, _ := entry.(map[string]any)
			inner, _ := group["hooks"].([]any)
			for _, item := range inner {
				h, _ := item.(map[string]any)
				if h["command"] == command {
					found = true
				}
			}
		}
		if !found {
			hooks[event] = append(entries, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}})
		}
	}
	settings["hooks"] = hooks
	encoded, err := json.Marshal(settings)
	if err != nil {
		return Invocation{}, err
	}
	invocation.Args = append([]string(nil), invocation.Args...)
	if index >= 0 {
		invocation.Args[index] = string(encoded)
	} else {
		invocation.Args = append([]string{"--settings", string(encoded)}, invocation.Args...)
	}
	return invocation, nil
}

func lifecycleCommand(executable, sessionID, event string) string {
	return `"` + executable + `" --agent-event ` + sessionID + ` ` + event
}
