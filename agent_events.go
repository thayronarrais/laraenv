package main

import (
	"encoding/json"
	"io"
	"strings"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/i18n"
)

// writeAgentEvent converts provider hook payloads into a minimal LaraEnv-owned
// envelope. Message text, prompts, transcript paths and tool data are ignored.
func writeAgentEvent(args []string, stdin io.Reader, directory string, contexts ...*agentHelperContext) (bool, error) {
	if len(args) < 4 || args[1] != "--agent-event" {
		return false, nil
	}
	sessionID, source := args[2], args[3]
	var ctx *agentHelperContext
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	if source == "claude-stop" && ctx != nil && ctx.stdout != nil {
		defer io.WriteString(ctx.stdout, "{}\n")
	}
	runID, ownerID := "", ""
	if len(args) >= 8 && args[4] == "--activity-run" && args[6] == "--activity-owner" {
		runID, ownerID = args[5], args[7]
		args = append(append([]string(nil), args[:4]...), args[8:]...)
	}
	var binding pkgagents.ActivityBinding
	if ctx != nil && ctx.registry != nil {
		if runID == "" {
			runID = ctx.getenv("LARAENV_AGENT_RUN")
		}
		if ownerID == "" {
			ownerID = ctx.getenv("LARAENV_AGENT_OWNER")
		}
		var found bool
		var err error
		binding, found, err = ctx.registry.ResolveActiveBinding(sessionID)
		if err != nil {
			return true, err
		}
		if found {
			if runID == "" || ownerID == "" || runID != binding.RunID || ownerID != binding.OwnerID {
				return true, nil
			}
			if terminal := ctx.getenv("LARAENV_TERMINAL_ID"); terminal != "" && terminal != binding.TerminalID {
				return true, nil
			}
			if envRun := ctx.getenv("LARAENV_AGENT_RUN"); envRun != "" && envRun != runID {
				return true, nil
			}
			if envOwner := ctx.getenv("LARAENV_AGENT_OWNER"); envOwner != "" && envOwner != ownerID {
				return true, nil
			}
		} else if ownerID != "" || runID != "" {
			// An inherited terminal can emit before BeginRun. Literal run/owner
			// arguments and the matching live terminal prove routing, not an
			// active run; Task6's bounded queue must validate BeginRun first.
			terminalID := ctx.getenv("LARAENV_TERMINAL_ID")
			if runID == "" || ownerID == "" || ctx.getenv("LARAENV_AGENT_RUN") != runID || ctx.getenv("LARAENV_AGENT_OWNER") != ownerID || ctx.getenv(pkgagents.AgentSessionEnv) != sessionID || terminalID == "" {
				return true, nil
			}
			live, err := ctx.registry.ResolveTerminal(ownerID, terminalID)
			if err != nil || !live {
				return true, err
			}
			// Snapshot lookup is used only as a tombstone rejection. An ended
			// binding can never supply authorization or routing identities.
			previous, known, err := ctx.registry.ResolveBinding(sessionID)
			if err != nil {
				return true, err
			}
			if known && previous.RunID == runID {
				return true, nil
			}
			provider := pkgagents.AgentID("")
			switch source {
			case "claude-stop", "claude-notification":
				provider = pkgagents.AgentClaude
			case "codex-notify":
				provider = pkgagents.AgentCodex
			default:
				return true, nil
			}
			binding = pkgagents.ActivityBinding{OwnerID: ownerID, RunID: runID, SessionID: sessionID, TerminalID: terminalID, Provider: provider}
		}
	}
	var kind pkgagents.SignalKind
	switch source {
	case "claude-stop":
		if stdin != nil {
			var payload struct {
				AgentID string `json:"agent_id"`
			}
			if err := decodeBoundedJSON(stdin, &payload); err != nil {
				return true, err
			}
			if payload.AgentID != "" {
				return true, nil
			}
		}
		kind = pkgagents.SignalDone
	case "claude-notification":
		var payload struct {
			NotificationType string `json:"notification_type"`
			AgentID          string `json:"agent_id"`
		}
		if err := decodeBoundedJSON(stdin, &payload); err != nil {
			return true, err
		}
		if payload.AgentID != "" {
			return true, nil
		}
		switch payload.NotificationType {
		case "permission_prompt":
			kind = pkgagents.SignalNeedsInput
		case "idle_prompt":
			kind = pkgagents.SignalDone
		default:
			return true, nil
		}
	case "codex-notify":
		if len(args) < 5 {
			return true, i18n.Errorf("errors.app.codexPayloadMissing", nil)
		}
		var payload struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread-id"`
		}
		if len(args[4]) > pkgagents.ActivityHookInputLimit {
			return true, i18n.Errorf("errors.app.decodeCodexNotificationType", nil)
		}
		if err := json.Unmarshal([]byte(args[4]), &payload); err != nil {
			return true, i18n.Errorf("errors.app.decodeCodexNotificationType", nil)
		}
		if binding.ProviderSessionID != "" && payload.ThreadID != "" && payload.ThreadID != binding.ProviderSessionID {
			return true, nil
		}
		switch strings.ToLower(payload.Type) {
		case "agent-turn-complete":
			kind = pkgagents.SignalDone
		case "approval-requested", "approval-request":
			kind = pkgagents.SignalNeedsInput
		default:
			return true, nil
		}
	default:
		return true, i18n.Errorf("errors.app.unknownAgentEventSource", nil)
	}
	if binding.OwnerID != "" {
		directory, err := ctx.registry.QueueDir(binding.OwnerID, "session")
		if err != nil {
			return true, err
		}
		return true, pkgagents.WriteSignal(directory, pkgagents.SessionSignal{OwnerID: binding.OwnerID, RunID: binding.RunID, SessionID: sessionID, TerminalID: binding.TerminalID, Provider: binding.Provider, ProviderSessionID: binding.ProviderSessionID, Kind: kind})
	}
	return true, pkgagents.WriteSessionSignal(directory, sessionID, kind)
}

func decodeBoundedJSON(reader io.Reader, target any) error {
	if reader == nil {
		return i18n.Errorf("errors.app.decodeBoundedPayloadMissing", nil)
	}
	data, err := io.ReadAll(io.LimitReader(reader, pkgagents.ActivityHookInputLimit+1))
	if err != nil || len(data) > pkgagents.ActivityHookInputLimit {
		return i18n.Errorf("errors.app.decodeBoundedTypeFailed", nil)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return i18n.Errorf("errors.app.decodeBoundedTypeFailed", nil)
	}
	return nil
}
