package agents

import (
	"strings"

	"lara4ever/internal/i18n"
)

type Invocation struct {
	Executable string
	Args       []string
	Cwd        string
}

func BuildLaunchInvocation(profile AgentProfile, projectPath, providerSessionID string, additionalDirectories ...string) (Invocation, error) {
	if err := validateInvocationInput(profile, projectPath); err != nil {
		return Invocation{}, err
	}
	invocation := Invocation{Executable: profile.Executable, Cwd: projectPath}
	switch profile.ID {
	case AgentClaude:
		if providerSessionID != "" {
			invocation.Args = []string{"--session-id", providerSessionID}
		}
	case AgentCodex, AgentKimi:
		// Both inherit the process cwd. Avoid duplicating the path in provider
		// flags so ConPTY remains the single source of workspace selection.
	case AgentOpenCode:
		invocation.Args = []string{projectPath}
	default:
		return Invocation{}, i18n.Errorf("errors.agents.unsupportedAgent", map[string]any{"agent": profile.ID})
	}
	return addDirectoryArguments(invocation, profile, additionalDirectories)
}

func BuildResumeInvocation(profile AgentProfile, projectPath, providerSessionID string, additionalDirectories ...string) (Invocation, error) {
	if err := validateInvocationInput(profile, projectPath); err != nil {
		return Invocation{}, err
	}
	providerSessionID = strings.TrimSpace(providerSessionID)
	if providerSessionID == "" {
		return Invocation{}, i18n.Errorf("errors.agents.resumeSessionIdRequired", nil)
	}
	invocation := Invocation{Executable: profile.Executable, Cwd: projectPath}
	switch profile.ID {
	case AgentClaude:
		invocation.Args = []string{"--resume", providerSessionID}
	case AgentCodex:
		invocation.Args = []string{"resume", providerSessionID, "-C", projectPath}
	case AgentKimi:
		invocation.Args = []string{"--session", providerSessionID}
	case AgentOpenCode:
		invocation.Args = []string{"--session", providerSessionID, projectPath}
	default:
		return Invocation{}, i18n.Errorf("errors.agents.unsupportedAgent", map[string]any{"agent": profile.ID})
	}
	return addDirectoryArguments(invocation, profile, additionalDirectories)
}

func addDirectoryArguments(invocation Invocation, profile AgentProfile, directories []string) (Invocation, error) {
	if err := validateAdditionalDirectorySupport(profile, directories); err != nil {
		return Invocation{}, err
	}
	if len(directories) > 0 && profile.ID == AgentClaude {
		// Claude documents one variadic --add-dir option.
		invocation.Args = append(invocation.Args, "--add-dir")
		invocation.Args = append(invocation.Args, directories...)
		return invocation, nil
	}
	for _, directory := range directories {
		// Keep each path as one argument; the terminal registry handles Windows
		// quoting without involving a shell. Codex and Kimi accept repeats.
		invocation.Args = append(invocation.Args, "--add-dir", directory)
	}
	return invocation, nil
}

func validateInvocationInput(profile AgentProfile, projectPath string) error {
	if !profile.Available || strings.TrimSpace(profile.Executable) == "" {
		return i18n.Errorf("errors.agents.agentUnavailable", map[string]any{"agent": profile.ID})
	}
	if strings.TrimSpace(projectPath) == "" {
		return i18n.Errorf("errors.agents.projectPathRequired", nil)
	}
	return nil
}
