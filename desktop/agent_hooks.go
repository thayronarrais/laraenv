package main

import (
	"bytes"
	"io"
	"strings"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/i18n"
	"lara4ever/internal/pty"
)

// writeAgentHook is the command behind the global provider hooks LaraEnv
// installs for adopted sessions. It only acts inside terminals LaraEnv spawned
// (LARAENV_TERMINAL_ID) that are not already managed launches
// (LARAENV_AGENT_SESSION). Payload fields other than the event name, session
// id, cwd, source and notification type are discarded.
func writeAgentHook(args []string, stdin io.Reader, getenv func(string) string, directory string, contexts ...*agentHelperContext) (bool, error) {
	if len(args) < 3 || args[1] != "--agent-hook" {
		return false, nil
	}
	provider := pkgagents.AgentID(strings.ToLower(strings.TrimSpace(args[2])))
	var ctx *agentHelperContext
	if len(contexts) > 0 {
		ctx = contexts[0]
	}
	var registered pkgagents.ActivityBinding
	if ctx != nil && (provider == pkgagents.AgentClaude || provider == pkgagents.AgentCodex) {
		data, metadata, err := pkgagents.ReadActivityHook(stdin)
		if err != nil {
			return true, err
		}
		stdin = bytes.NewReader(data)
		// Managed Claude already has transient activity hooks; the global
		// handler must not produce the same event a second time.
		if provider == pkgagents.AgentClaude && getenv(pkgagents.AgentSessionEnv) != "" {
			if ctx.stdout != nil && (metadata.Event == "Stop" || metadata.Event == "SubagentStop") {
				_, _ = io.WriteString(ctx.stdout, "{}\n")
			}
			return true, nil
		}
		// Common hook metadata can identify a child while session_id still
		// names its parent. Only dedicated subagent events have an activity
		// mapping; never discard this child attribution into a root signal.
		if metadata.AgentID != "" && metadata.Event != "SubagentStart" && metadata.Event != "SubagentStop" {
			if ctx.stdout != nil && metadata.Event == "Stop" {
				_, _ = io.WriteString(ctx.stdout, "{}\n")
			}
			return true, nil
		}
		if metadata.Event == "SubagentStart" || metadata.Event == "SubagentStop" || metadata.Event == "Stop" {
			_, err := writeAgentActivity([]string{args[0], "--agent-activity", string(provider)}, bytes.NewReader(data), ctx.stdout, getenv, ctx.registry)
			if err != nil {
				return true, err
			}
			if metadata.Event != "Stop" {
				return true, nil
			}
		}
		var ok bool
		registered, ok, err = resolveHookActivityBinding(provider, metadata.SessionID, getenv, ctx.registry)
		if err != nil {
			return true, err
		}
		if !ok && (getenv("LARAENV_AGENT_OWNER") != "" || registered.OwnerID != "") {
			return true, nil
		}
		// A proven descendant may emit its own ordinary lifecycle hooks.
		// Those are never lifecycle transitions of the canonical principal.
		if ok && registered.ProviderSessionID != metadata.SessionID {
			return true, nil
		}
	}
	terminalID := strings.TrimSpace(getenv(pty.TerminalIDEnv))
	if terminalID == "" {
		terminalID = registered.TerminalID
	}
	if terminalID == "" || strings.TrimSpace(getenv(pkgagents.AgentSessionEnv)) != "" {
		// Not a LaraEnv terminal, or a launched session that already has hooks.
		if stdin != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(stdin, 1024*1024))
		}
		return true, nil
	}
	signal := pkgagents.SessionSignal{TerminalID: terminalID, Provider: provider}
	var kind pkgagents.SignalKind
	var ok bool
	switch provider {
	case pkgagents.AgentClaude, pkgagents.AgentCodex, pkgagents.AgentKimi:
		var payload struct {
			Event            string `json:"hook_event_name"`
			SessionID        string `json:"session_id"`
			AgentID          string `json:"agent_id"`
			Cwd              string `json:"cwd"`
			Source           string `json:"source"`
			NotificationType string `json:"notification_type"`
		}
		if err := decodeBoundedJSON(stdin, &payload); err != nil {
			return true, err
		}
		if (provider == pkgagents.AgentClaude || provider == pkgagents.AgentCodex) && payload.AgentID != "" {
			return true, nil
		}
		signal.ProviderSessionID = strings.TrimSpace(payload.SessionID)
		signal.Cwd = strings.TrimSpace(payload.Cwd)
		kind, ok = hookEventKind(payload.Event, payload.NotificationType)
		if payload.Event == "SessionStart" {
			signal.Source = strings.TrimSpace(payload.Source)
		}
	case pkgagents.AgentOpenCode:
		if len(args) < 6 {
			return true, i18n.Errorf("errors.app.openCodeArgsMissing", nil)
		}
		signal.ProviderSessionID = strings.TrimSpace(args[4])
		signal.Cwd = strings.TrimSpace(args[5])
		kind, ok = openCodeEventKind(args[3])
	default:
		return true, i18n.Errorf("errors.app.unknownAgentHookProvider", nil)
	}
	if !ok {
		return true, nil
	}
	signal.Kind = kind
	if handled, err := writeOwnedSessionSignal(directory, signal, ctx); handled {
		return true, err
	}
	return true, pkgagents.WriteSignal(directory, signal)
}

// hookEventKind maps the shared Claude/Codex/Kimi hook vocabulary.
func hookEventKind(event, notificationType string) (pkgagents.SignalKind, bool) {
	switch event {
	case "SessionStart":
		return pkgagents.SignalStarted, true
	case "UserPromptSubmit":
		return pkgagents.SignalWorking, true
	case "Stop":
		return pkgagents.SignalDone, true
	case "PermissionRequest":
		return pkgagents.SignalNeedsInput, true
	case "SessionEnd":
		return pkgagents.SignalEnded, true
	case "Notification":
		switch notificationType {
		case "permission_prompt":
			return pkgagents.SignalNeedsInput, true
		case "idle_prompt":
			return pkgagents.SignalDone, true
		}
	}
	return "", false
}

func openCodeEventKind(event string) (pkgagents.SignalKind, bool) {
	switch strings.TrimSpace(event) {
	case "session.created":
		return pkgagents.SignalStarted, true
	case "message.user":
		return pkgagents.SignalWorking, true
	case "session.idle":
		return pkgagents.SignalDone, true
	case "permission.asked":
		return pkgagents.SignalNeedsInput, true
	case "session.deleted":
		return pkgagents.SignalEnded, true
	}
	return "", false
}
