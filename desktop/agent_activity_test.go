package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pkgagents "lara4ever/internal/agents"
)

func activityHelperRegistry(t *testing.T) (*pkgagents.ActivityOwnerRegistry, pkgagents.ActivityBinding) {
	t.Helper()
	registry := pkgagents.NewActivityOwnerRegistry(t.TempDir(), func(pkgagents.ActivityOwnerIdentity) bool { return true })
	owner := pkgagents.ActivityOwnerDescriptor{Identity: pkgagents.ActivityOwnerIdentity{ID: uuid.NewString(), PID: os.Getpid(), CreatedAt: time.Now().UTC()}, Endpoint: "http://127.0.0.1:12345", Token: "synthetic-token"}
	if err := registry.Register(owner); err != nil {
		t.Fatal(err)
	}
	b := pkgagents.ActivityBinding{OwnerID: owner.Identity.ID, SessionID: "lara-1", RunID: uuid.NewString(), TerminalID: "term-1", Provider: pkgagents.AgentCodex, ProviderSessionID: "root-1"}
	if err := registry.SetTerminal(b.OwnerID, b.TerminalID, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.Bind(b); err != nil {
		t.Fatal(err)
	}
	return registry, b
}
func TestActivityHookStopReturnsNeutralJSON(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	var stdout bytes.Buffer
	handled, err := writeAgentActivity([]string{"LaraEnv.exe", "--agent-activity", "codex"}, strings.NewReader(`{"hook_event_name":"SubagentStop","session_id":"root-1","agent_id":"child-1","tool_response":"PRIVATE_SENTINEL"}`), &stdout, envWith(nil), registry)
	if !handled || err != nil {
		t.Fatal(handled, err)
	}
	if stdout.String() != "{}\n" {
		t.Fatal(stdout.String())
	}
	dir, _ := registry.QueueDir(b.OwnerID, "activity")
	count := 0
	pkgagents.DrainActivitySignals(dir, func(e pkgagents.ActivityEvent) {
		count++
		if e.SessionID != b.SessionID || e.RunID != b.RunID {
			t.Fatal(e)
		}
	})
	if count != 1 {
		t.Fatal(count)
	}
	sessionDir, _ := registry.QueueDir(b.OwnerID, "session")
	if got := drainHookSignals(t, sessionDir); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestWriteAgentActivityRejectsEndedAndStaleRun(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	payload := `{"hook_event_name":"SubagentStart","session_id":"root-1","agent_id":"child-1"}`
	_, err := writeAgentActivity([]string{"app", "--agent-activity", "codex"}, strings.NewReader(payload), &bytes.Buffer{}, envWith(map[string]string{"LARAENV_AGENT_OWNER": b.OwnerID, "LARAENV_TERMINAL_ID": b.TerminalID, "LARAENV_AGENT_SESSION": b.SessionID, "LARAENV_AGENT_RUN": "old-run"}), registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Release(b.SessionID, b.RunID); err != nil {
		t.Fatal(err)
	}
	_, err = writeAgentActivity([]string{"app", "--agent-activity", "codex"}, strings.NewReader(payload), &bytes.Buffer{}, envWith(nil), registry)
	if err != nil {
		t.Fatal(err)
	}
	_, err = writeAgentActivity([]string{"app", "--agent-activity", "codex"}, strings.NewReader(payload), &bytes.Buffer{}, envWith(map[string]string{"LARAENV_AGENT_OWNER": b.OwnerID, "LARAENV_AGENT_RUN": b.RunID, "LARAENV_AGENT_SESSION": b.SessionID, "LARAENV_TERMINAL_ID": b.TerminalID}), registry)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := registry.QueueDir(b.OwnerID, "activity")
	pkgagents.DrainActivitySignals(dir, func(pkgagents.ActivityEvent) { t.Fatal("stale run accepted") })
}
func TestWriteAgentActivityResolvesGrandchildCanonicalBinding(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	if err := registry.SetProviderAliases(b.SessionID, b.RunID, []string{"child-1"}); err != nil {
		t.Fatal(err)
	}
	_, err := writeAgentActivity([]string{"app", "--agent-activity", "codex"}, strings.NewReader(`{"hook_event_name":"SubagentStart","session_id":"child-1","agent_id":"grandchild-1"}`), &bytes.Buffer{}, envWith(nil), registry)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	dir, _ := registry.QueueDir(b.OwnerID, "activity")
	pkgagents.DrainActivitySignals(dir, func(e pkgagents.ActivityEvent) {
		count++
		if e.ProviderSessionID != "root-1" || e.ParentProviderID != "child-1" {
			t.Fatal(e)
		}
	})
	if count != 1 {
		t.Fatal(count)
	}
}

func TestWriteAgentEventOwnerRunFencingAndEndedLiveTerminal(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	directory := t.TempDir()
	ctx := &agentHelperContext{registry: registry, getenv: envWith(nil), stdout: &bytes.Buffer{}}
	args := []string{"app", "--agent-event", b.SessionID, "codex-notify", "--activity-run", b.RunID, "--activity-owner", b.OwnerID, `{"type":"agent-turn-complete","messages":"PRIVATE_SENTINEL"}`}
	if handled, err := writeAgentEvent(args, nil, directory, ctx); !handled || err != nil {
		t.Fatal(handled, err)
	}
	owned, _ := registry.QueueDir(b.OwnerID, "session")
	events := drainHookSignals(t, owned)
	if len(events) != 1 || events[0].RunID != b.RunID || events[0].OwnerID != b.OwnerID {
		t.Fatal(events)
	}
	oldArgs := append([]string(nil), args...)
	oldArgs[5] = "old-run"
	writeAgentEvent(oldArgs, nil, directory, ctx)
	if err := registry.Release(b.SessionID, b.RunID); err != nil {
		t.Fatal(err)
	}
	writeAgentEvent(args, nil, directory, ctx)
	if got := drainHookSignals(t, owned); len(got) != 0 {
		t.Fatal("ended or stale root accepted", got)
	}
	if got := drainHookSignals(t, directory); len(got) != 0 {
		t.Fatal("owned event fell back to legacy", got)
	}
}

func TestWriteAgentHookManagedSubagentBeforeRootGuardAndOptOut(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	directory := t.TempDir()
	out := &bytes.Buffer{}
	env := map[string]string{"LARAENV_AGENT_OWNER": b.OwnerID, "LARAENV_AGENT_RUN": b.RunID, "LARAENV_AGENT_SESSION": b.SessionID, "LARAENV_TERMINAL_ID": b.TerminalID}
	ctx := &agentHelperContext{registry: registry, getenv: envWith(env), stdout: out}
	args := []string{"app", "--agent-hook", "codex"}
	raw := `{"hook_event_name":"SubagentStop","session_id":"root-1","agent_id":"child-1"}`
	if _, err := writeAgentHook(args, strings.NewReader(raw), ctx.getenv, directory, ctx); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{}\n" {
		t.Fatal(out.String())
	}
	dir, _ := registry.QueueDir(b.OwnerID, "activity")
	count := 0
	pkgagents.DrainActivitySignals(dir, func(pkgagents.ActivityEvent) { count++ })
	if count != 1 {
		t.Fatal(count)
	}
	env["LARAENV_ACTIVITY_HOOKS"] = "0"
	writeAgentHook(args, strings.NewReader(raw), ctx.getenv, directory, ctx)
	pkgagents.DrainActivitySignals(dir, func(pkgagents.ActivityEvent) { t.Fatal("opted-out hook accepted") })
	if got := drainHookSignals(t, directory); len(got) != 0 {
		t.Fatal("subagent completed root", got)
	}
}

func TestWriteAgentActivityPreBeginKeepsManagedRunFence(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	registry.Release(b.SessionID, b.RunID)
	env := envWith(map[string]string{"LARAENV_AGENT_OWNER": b.OwnerID, "LARAENV_AGENT_RUN": "new-run", "LARAENV_AGENT_SESSION": b.SessionID, "LARAENV_TERMINAL_ID": b.TerminalID})
	if _, err := writeAgentActivity([]string{"app", "--agent-activity", "codex"}, strings.NewReader(`{"hook_event_name":"SubagentStart","session_id":"new-root","agent_id":"child-1"}`), &bytes.Buffer{}, env, registry); err != nil {
		t.Fatal(err)
	}
	dir, _ := registry.QueueDir(b.OwnerID, "activity")
	count := 0
	pkgagents.DrainActivitySignals(dir, func(e pkgagents.ActivityEvent) {
		count++
		if e.RunID != "new-run" || e.SessionID != b.SessionID {
			t.Fatal(e)
		}
	})
	if count != 1 {
		t.Fatal(count)
	}
}

func TestWriteAgentHookChildLifecycleNeverReplacesRoot(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	if err := registry.SetProviderAliases(b.SessionID, b.RunID, []string{"child-1"}); err != nil {
		t.Fatal(err)
	}
	for _, env := range []map[string]string{nil, {"LARAENV_AGENT_OWNER": b.OwnerID, "LARAENV_TERMINAL_ID": b.TerminalID}} {
		for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"} {
			ctx := &agentHelperContext{registry: registry, getenv: envWith(env), stdout: &bytes.Buffer{}}
			dir := t.TempDir()
			_, err := writeAgentHook([]string{"app", "--agent-hook", "codex"}, strings.NewReader(`{"hook_event_name":"`+event+`","session_id":"child-1","cwd":"C:/synthetic"}`), ctx.getenv, dir, ctx)
			if err != nil {
				t.Fatal(err)
			}
			owned, _ := registry.QueueDir(b.OwnerID, "session")
			if got := drainHookSignals(t, owned); len(got) != 0 {
				t.Fatal("child replaced root", got)
			}
			if got := drainHookSignals(t, dir); len(got) != 0 {
				t.Fatal("child adopted", got)
			}
		}
	}
}

func TestWriteAgentEventPreBeginPendingRequiresTerminalAndRunProof(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	if err := registry.Release(b.SessionID, b.RunID); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"LARAENV_AGENT_OWNER": b.OwnerID, "LARAENV_AGENT_RUN": "next-run", "LARAENV_TERMINAL_ID": b.TerminalID, "LARAENV_AGENT_SESSION": b.SessionID}
	ctx := &agentHelperContext{registry: registry, getenv: envWith(env), stdout: &bytes.Buffer{}}
	args := []string{"app", "--agent-event", b.SessionID, "codex-notify", "--activity-run", "next-run", "--activity-owner", b.OwnerID, `{"type":"agent-turn-complete"}`}
	directory := t.TempDir()
	if _, err := writeAgentEvent(args, nil, directory, ctx); err != nil {
		t.Fatal(err)
	}
	owned, _ := registry.QueueDir(b.OwnerID, "session")
	got := drainHookSignals(t, owned)
	if len(got) != 1 || got[0].RunID != "next-run" || got[0].ProviderSessionID != "" {
		t.Fatal(got)
	}
	env["LARAENV_AGENT_RUN"] = b.RunID
	args[5] = b.RunID
	writeAgentEvent(args, nil, directory, ctx)
	if got := drainHookSignals(t, owned); len(got) != 0 {
		t.Fatal("ended run authorized by live shell", got)
	}
}

func TestWriteAgentHookExplicitChildWithRootSessionCannotMutateRoot(t *testing.T) {
	registry, b := activityHelperRegistry(t)
	if err := registry.Release(b.SessionID, b.RunID); err != nil {
		t.Fatal(err)
	}
	b.Provider = pkgagents.AgentClaude
	if err := registry.Bind(b); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetProviderAliases(b.SessionID, b.RunID, []string{"child-1"}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"Notification", "SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"} {
		for _, owned := range []bool{true, false} {
			t.Run(event+map[bool]string{true: "/owned", false: "/legacy"}[owned], func(t *testing.T) {
				dir := t.TempDir()
				env := envWith(map[string]string{"LARAENV_TERMINAL_ID": b.TerminalID, "LARAENV_AGENT_OWNER": b.OwnerID})
				out := &bytes.Buffer{}
				var contexts []*agentHelperContext
				if owned {
					contexts = []*agentHelperContext{{registry: registry, getenv: env, stdout: out}}
				}
				payload := `{"hook_event_name":"` + event + `","session_id":"root-1","agent_id":"child-1","notification_type":"permission_prompt","cwd":"C:/synthetic"}`
				if _, err := writeAgentHook([]string{"app", "--agent-hook", "claude"}, strings.NewReader(payload), env, dir, contexts...); err != nil {
					t.Fatal(err)
				}
				queue, _ := registry.QueueDir(b.OwnerID, "session")
				if got := drainHookSignals(t, queue); len(got) != 0 {
					t.Fatal("explicit child changed owned root", got)
				}
				if got := drainHookSignals(t, dir); len(got) != 0 {
					t.Fatal("explicit child adopted or changed legacy root", got)
				}
			})
		}
	}
	ctx := &agentHelperContext{registry: registry, getenv: envWith(nil), stdout: &bytes.Buffer{}}
	if _, err := writeAgentHook([]string{"app", "--agent-hook", "claude"}, strings.NewReader(`{"hook_event_name":"Notification","session_id":"root-1","agent_type":"named-root","notification_type":"permission_prompt"}`), ctx.getenv, t.TempDir(), ctx); err != nil {
		t.Fatal(err)
	}
	queue, _ := registry.QueueDir(b.OwnerID, "session")
	if got := drainHookSignals(t, queue); len(got) != 1 || got[0].Kind != pkgagents.SignalNeedsInput {
		t.Fatal("agent_type alone suppressed root", got)
	}
}

func TestWriteAgentEventExplicitChildCannotMutateRoot(t *testing.T) {
	for _, source := range []string{"claude-notification", "claude-stop"} {
		t.Run(source, func(t *testing.T) {
			registry, b := activityHelperRegistry(t)
			dir := t.TempDir()
			ctx := &agentHelperContext{registry: registry, getenv: envWith(nil), stdout: &bytes.Buffer{}}
			args := []string{"app", "--agent-event", b.SessionID, source, "--activity-run", b.RunID, "--activity-owner", b.OwnerID}
			if _, err := writeAgentEvent(args, strings.NewReader(`{"agent_id":"child-1","session_id":"root-1","notification_type":"permission_prompt"}`), dir, ctx); err != nil {
				t.Fatal(err)
			}
			queue, _ := registry.QueueDir(b.OwnerID, "session")
			if got := drainHookSignals(t, queue); len(got) != 0 {
				t.Fatal("transient child hook changed root", got)
			}
		})
	}
}
