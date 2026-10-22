package main

import (
	"errors"
	"os"
	"path/filepath"

	pkgagents "lara4ever/internal/agents"
	"lara4ever/internal/i18n"
	"lara4ever/internal/terminal"
	"lara4ever/internal/workspacefiles"
)

// WorkspacePtyResult identifies a shell whose authority is a live agent
// session, without making the shell itself an agent session.
type WorkspacePtyResult struct {
	TermID  string `json:"termId"`
	RootKey string `json:"rootKey"`
	Label   string `json:"label"`
}

func startProjectPty(name, kind, projectRoot string, cols, rows int,
	spawn func(string, string, string, int, int) (string, error),
	savePreference func(string, string) error,
) (string, error) {
	if name == "" {
		return "", i18n.Errorf("errors.app.nameRequired", nil)
	}
	projectPath := filepath.Join(projectRoot, name)
	if _, err := os.Stat(projectPath); err != nil {
		return "", i18n.Wrapf(err, "errors.app.projectFolderNotFound", nil)
	}
	id, err := spawn(name, kind, projectPath, cols, rows)
	if err != nil {
		return "", err
	}
	if savePreference != nil && kind != "" {
		_ = savePreference(name, kind)
	}
	return id, nil
}

func startProjectShell(name, kind, cwd string, cols, rows int,
	resolve func(string, string) (terminal.Profile, error),
	phpDir, nodeDir func(string) string,
	spawn func(string, string, []string, int, int) (string, error),
) (string, error) {
	picked, err := resolve(kind, name)
	if err != nil {
		return "", err
	}
	command := ptyCommandLine(picked)
	if command == "" {
		return "", i18n.Errorf("errors.app.terminalKindNotEmbeddable", map[string]any{"kind": picked.Kind})
	}
	var prefix []string
	if dir := phpDir(name); dir != "" {
		prefix = append(prefix, dir)
	}
	if dir := nodeDir(name); dir != "" {
		prefix = append(prefix, dir)
	}
	return spawn(command, cwd, prefix, cols, rows)
}

func startAgentWorkspacePty(sessionID, kind string, cols, rows int, expectedRootKey string,
	context func(workspacefiles.ContextRef) workspacefiles.Result,
	get func(string) (pkgagents.AgentSession, bool),
	spawn func(string, string, string, int, int) (string, error),
) (WorkspacePtyResult, error) {
	if sessionID == "" {
		return WorkspacePtyResult{}, errors.New("agent session required")
	}
	ref := workspacefiles.ContextRef{Kind: "agent", SessionID: sessionID}
	resolved := context(ref)
	if resolved.Code != "ok" || resolved.Context == nil {
		return WorkspacePtyResult{}, errors.New("agent workspace unavailable")
	}
	info := resolved.Context
	if expectedRootKey != "" && expectedRootKey != info.RootKey {
		return WorkspacePtyResult{}, errors.New("agent workspace root changed")
	}
	session, ok := get(sessionID)
	if !ok || session.ID != sessionID || session.ProjectName == "" || session.ProjectPath == "" ||
		filepath.Clean(session.ProjectPath) != filepath.Clean(info.Path) {
		return WorkspacePtyResult{}, errors.New("agent session unavailable")
	}
	id, err := spawn(session.ProjectName, kind, info.Path, cols, rows)
	if err != nil {
		return WorkspacePtyResult{}, err
	}
	return WorkspacePtyResult{TermID: id, RootKey: info.RootKey, Label: info.Path}, nil
}

func (a *App) startProjectShell(name, kind, cwd string, cols, rows int) (string, error) {
	phpRuntime, nodeRuntime := a.projectRuntimeSelection(name, cwd)
	if _, err := runtimeSelectedPaths(name, phpRuntime, nodeRuntime, a.php, a.nodejs); err != nil {
		return "", err
	}
	return startProjectShell(name, kind, cwd, cols, rows, a.resolveTerminal,
		func(name string) string {
			if v := phpRuntime.Version; v != "" {
				return a.php.VersionDir(v)
			}
			return ""
		},
		func(name string) string {
			if nodeRuntime.Version != "" {
				return a.nodejs.VersionDir(nodeRuntime.Version)
			}
			return ""
		},
		a.startPtyCmd)
}

// StartPtyForAgentSession opens a shell in the session's effective checkout.
// expectedRootKey pins a restored shell to the physical root it previously used.
func (a *App) StartPtyForAgentSession(sessionID, kind string, cols, rows int, expectedRootKey string) (WorkspacePtyResult, error) {
	if a.agents == nil {
		return WorkspacePtyResult{}, errors.New("agent sessions unavailable")
	}
	return startAgentWorkspacePty(sessionID, kind, cols, rows, expectedRootKey,
		a.fileService().Context, a.agents.GetSession, a.startProjectShell)
}
