package agents

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildLaunchInvocation(t *testing.T) {
	tests := []struct {
		id   AgentID
		sid  string
		args []string
	}{
		{AgentClaude, "provider-id", []string{"--session-id", "provider-id"}},
		{AgentCodex, "", nil},
		{AgentKimi, "", nil},
		{AgentOpenCode, "", []string{`C:\work\demo`}},
	}
	for _, test := range tests {
		t.Run(string(test.id), func(t *testing.T) {
			invocation, err := BuildLaunchInvocation(AgentProfile{ID: test.id, Available: true, Executable: "agent.exe"}, `C:\work\demo`, test.sid)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(invocation.Args, test.args) {
				t.Fatalf("args = %#v, want %#v", invocation.Args, test.args)
			}
		})
	}
}

func TestAdditionalDirectoryArgumentsSurviveLaunchAndResume(t *testing.T) {
	directories := []string{`C:\related projects\web & api`, `D:\shared\$(do-not-run)`}
	for _, id := range []AgentID{AgentClaude, AgentCodex, AgentKimi} {
		for _, resume := range []bool{false, true} {
			t.Run(string(id)+"/resume="+map[bool]string{false: "false", true: "true"}[resume], func(t *testing.T) {
				build := BuildLaunchInvocation
				if resume {
					build = BuildResumeInvocation
				}
				profile := AgentProfile{ID: id, Available: true, Executable: "agent.exe"}
				base, err := build(profile, `C:\primary`, "provider-id")
				if err != nil {
					t.Fatal(err)
				}
				got, err := build(profile, `C:\primary`, "provider-id", directories...)
				if err != nil {
					t.Fatal(err)
				}
				want := append(base.Args, "--add-dir", directories[0], "--add-dir", directories[1])
				if id == AgentClaude {
					want = append(base.Args, "--add-dir", directories[0], directories[1])
				}
				if !reflect.DeepEqual(got.Args, want) || got.Cwd != `C:\primary` {
					t.Fatalf("invocation = %#v, want args %#v with unchanged primary cwd", got, want)
				}
			})
		}
	}
}

func TestAdditionalDirectoriesRejectUnsupportedProvider(t *testing.T) {
	for _, build := range []func(AgentProfile, string, string, ...string) (Invocation, error){BuildLaunchInvocation, BuildResumeInvocation} {
		_, err := build(AgentProfile{ID: AgentOpenCode, Available: true, Executable: "opencode.exe"}, `C:\primary`, "provider-id", `D:\extra`)
		if err == nil || !strings.Contains(err.Error(), "does not support additional session directories") {
			t.Fatalf("got %v, want unsupported context error", err)
		}
	}
}

func TestBuildResumeInvocation(t *testing.T) {
	tests := []struct {
		id   AgentID
		args []string
	}{
		{AgentClaude, []string{"--resume", "provider-id"}},
		{AgentCodex, []string{"resume", "provider-id", "-C", `C:\work\demo`}},
		{AgentKimi, []string{"--session", "provider-id"}},
		{AgentOpenCode, []string{"--session", "provider-id", `C:\work\demo`}},
	}
	for _, test := range tests {
		t.Run(string(test.id), func(t *testing.T) {
			invocation, err := BuildResumeInvocation(AgentProfile{ID: test.id, Available: true, Executable: "agent.exe"}, `C:\work\demo`, "provider-id")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(invocation.Args, test.args) {
				t.Fatalf("args = %#v, want %#v", invocation.Args, test.args)
			}
		})
	}
}
