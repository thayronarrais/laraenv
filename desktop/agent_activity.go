package main

import (
	"bytes"
	"io"
	"strings"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/pty"
)

type agentHelperContext struct {
	registry *pkgagents.ActivityOwnerRegistry
	getenv   func(string) string
	stdout   io.Writer
}

// resolveHookActivityBinding uses only active provider claims. Environment
// fallback proves a live local terminal; its unresolved envelope must await
// the matching BeginRun in the bounded adoption queue, never enter the store.
func resolveHookActivityBinding(provider pkgagents.AgentID, providerID string, getenv func(string) string, registry *pkgagents.ActivityOwnerRegistry) (pkgagents.ActivityBinding, bool, error) {
	if registry == nil {
		return pkgagents.ActivityBinding{}, false, nil
	}
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	owner, terminal, session, run := strings.TrimSpace(getenv("LARAENV_AGENT_OWNER")), strings.TrimSpace(getenv(pty.TerminalIDEnv)), strings.TrimSpace(getenv(pkgagents.AgentSessionEnv)), strings.TrimSpace(getenv("LARAENV_AGENT_RUN"))
	binding, found, err := registry.ResolveProvider(provider, providerID)
	if err != nil {
		return binding, false, err
	}
	if found {
		if (owner != "" && owner != binding.OwnerID) || (terminal != "" && terminal != binding.TerminalID) || (session != "" && session != binding.SessionID) || (run != "" && run != binding.RunID) {
			return binding, false, nil
		}
		return binding, true, nil
	}
	if owner == "" || terminal == "" {
		return binding, false, nil
	}
	live, err := registry.ResolveTerminal(owner, terminal)
	if err != nil || !live {
		return binding, false, err
	}
	// Even managed launches may emit before BeginRun. Preserve their run fence
	// but do not treat the inherited values as active: the consumer resolves
	// this pending envelope against BeginRun/provider identities within five seconds.
	if (session == "") != (run == "") {
		return binding, false, nil
	}
	if session != "" {
		active, found, err := registry.ResolveActiveBinding(session)
		if err != nil {
			return binding, false, err
		}
		if found {
			if active.OwnerID != owner || active.TerminalID != terminal || active.RunID != run || active.Provider != provider || active.ProviderSessionID != "" {
				return binding, false, nil
			}
			// BeginRun may precede provider discovery. This still needs the
			// consumer's provider-ID correlation before the store accepts it.
			active.ProviderSessionID = providerID
			return active, true, nil
		}
		// Retained bindings are denial evidence only. Neither an ended run
		// nor a known run whose provider identity conflicts may be re-adopted
		// through the environment-backed pending path.
		previous, known, err := registry.ResolveBinding(session)
		if err != nil {
			return binding, false, err
		}
		if known && previous.RunID == run {
			return binding, false, nil
		}
	}
	return pkgagents.ActivityBinding{OwnerID: owner, TerminalID: terminal, SessionID: session, RunID: run, Provider: provider, ProviderSessionID: providerID}, true, nil
}

func writeAgentActivity(args []string, stdin io.Reader, stdout io.Writer, getenv func(string) string, registry *pkgagents.ActivityOwnerRegistry) (bool, error) {
	if len(args) < 3 || args[1] != "--agent-activity" {
		return false, nil
	}
	provider := pkgagents.AgentID(strings.ToLower(strings.TrimSpace(args[2])))
	data, metadata, err := pkgagents.ReadActivityHook(stdin)
	if err != nil {
		return true, err
	}
	if metadata.Event == "Stop" || metadata.Event == "SubagentStop" {
		if stdout != nil {
			defer io.WriteString(stdout, "{}\n")
		}
	}
	if getenv != nil && (getenv("LARAENV_ACTIVITY_HOOKS") == "0" || getenv("LARAENV_ACTIVITY_HOOKS") == "false") {
		return true, nil
	}
	binding, ok, err := resolveHookActivityBinding(provider, metadata.SessionID, getenv, registry)
	if err != nil || !ok {
		return true, err
	}
	events, err := pkgagents.NormalizeActivityHook(provider, bytes.NewReader(data), binding)
	if err != nil {
		return true, err
	}
	directory, err := registry.QueueDir(binding.OwnerID, "activity")
	if err != nil {
		return true, err
	}
	for _, event := range events {
		if err := pkgagents.WriteActivitySignal(directory, event); err != nil {
			return true, err
		}
	}
	return true, nil
}

func writeOwnedSessionSignal(directory string, signal pkgagents.SessionSignal, ctx *agentHelperContext) (bool, error) {
	if ctx == nil || ctx.registry == nil {
		return false, nil
	}
	binding, ok, err := resolveHookActivityBinding(signal.Provider, signal.ProviderSessionID, ctx.getenv, ctx.registry)
	if err != nil {
		return true, err
	}
	if !ok {
		// An explicit owner opts into isolated queues; do not spill into legacy.
		if ctx.getenv("LARAENV_AGENT_OWNER") != "" {
			return true, nil
		}
		return false, nil
	}
	signal.OwnerID = binding.OwnerID
	signal.RunID = binding.RunID
	signal.SessionID = binding.SessionID
	signal.TerminalID = binding.TerminalID
	directory, err = ctx.registry.QueueDir(binding.OwnerID, "session")
	if err != nil {
		return true, err
	}
	return true, pkgagents.WriteSignal(directory, signal)
}
