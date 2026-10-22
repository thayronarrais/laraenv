package agents

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"lara4ever/internal/pty"
)

type runtimeAgentRegistry struct {
	*fakeAgentRegistry
	dirs []string
}

func (r *runtimeAgentRegistry) NewCommand(executable string, args []string, cols, rows int, dirs []string, cwd string, env map[string]string) (string, pty.Session, error) {
	r.dirs = append([]string(nil), dirs...)
	return r.fakeAgentRegistry.NewCommand(executable, args, cols, rows, dirs, cwd, env)
}

func TestManagerRuntimeResolverUsesWorktreeForLaunchAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "launch", true: "resume"}[resume], func(t *testing.T) {
			manager, store, terminalSession := newDiscoveryTestManager(t)
			manager.discover = nil
			registry := &runtimeAgentRegistry{fakeAgentRegistry: manager.terminals.(*fakeAgentRegistry)}
			manager.terminals = registry
			defer terminalSession.Close()
			const checkout = `C:\worktrees\demo\feature`
			manager.workspace = func(AgentLaunchOptions) (string, string, string, error) { return checkout, "feature", checkout, nil }
			manager.pathPrefixes = func(string) []string { t.Error("legacy project path resolver used"); return nil }
			var got []string
			manager.SetRuntimePathResolver(func(project, cwd string) ([]string, error) {
				got = []string{project, cwd}
				return []string{`C:\worktree-php`, `C:\worktree-node`}, nil
			})
			var session AgentSession
			var err error
			if resume {
				initial := AgentSession{ID: "resume", ProjectName: "demo", ProjectPath: `C:\projects\demo`, AgentID: AgentCodex, LaunchMode: LaunchWorktree, State: SessionStopped, ProviderSessionID: "provider", WorktreePath: checkout}
				if err := store.Upsert(initial); err != nil {
					t.Fatal(err)
				}
				session, err = manager.Resume(context.Background(), initial.ID)
			} else {
				session, err = manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex, Mode: LaunchWorktree})
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, []string{"demo", checkout}) || registry.cwd != checkout || !reflect.DeepEqual(registry.dirs, []string{`C:\worktree-php`, `C:\worktree-node`}) {
				t.Fatalf("resolver=%v cwd=%q dirs=%v", got, registry.cwd, registry.dirs)
			}
			if err := manager.Stop(session.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManagerRuntimeRejectionDoesNotLaunchOrAlterStoppedSession(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(map[bool]string{false: "launch", true: "resume"}[resume], func(t *testing.T) {
			manager, store, terminalSession := newDiscoveryTestManager(t)
			defer terminalSession.Close()
			manager.discover = nil
			manager.pathPrefixes = func(string) []string { t.Error("legacy fallback used"); return nil }
			failure := errors.New("Node 24 required by worktree")
			manager.SetRuntimePathResolver(func(string, string) ([]string, error) { return nil, failure })
			var err error
			if resume {
				initial := AgentSession{ID: "resume", ProjectName: "demo", AgentID: AgentCodex, LaunchMode: LaunchCurrent, State: SessionStopped, ProviderSessionID: "provider"}
				if err := store.Upsert(initial); err != nil {
					t.Fatal(err)
				}
				_, err = manager.Resume(context.Background(), initial.ID)
				got, _ := store.Get(initial.ID)
				if got.State != SessionStopped || got.TerminalID != "" {
					t.Fatalf("rejection altered session: %+v", got)
				}
			} else {
				_, err = manager.Launch(context.Background(), AgentLaunchOptions{ProjectName: "demo", AgentID: AgentCodex})
				if len(store.List("", true)) != 0 {
					t.Fatal("rejected launch persisted a session")
				}
			}
			if !errors.Is(err, failure) {
				t.Fatalf("error=%v", err)
			}
			if manager.terminals.(*fakeAgentRegistry).executable != "" {
				t.Fatal("terminal spawned despite runtime rejection")
			}
		})
	}
}
