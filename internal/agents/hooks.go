package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
)

// Hook installation for adopted sessions. Every entry LaraEnv writes is
// recognisable (hookMarker in the command, a delimited TOML block, a managed
// plugin file) so uninstall removes exactly what install added.

const (
	hookMarker      = "--agent-hook"
	kimiBlockBegin  = "# laraenv:begin"
	kimiBlockEnd    = "# laraenv:end"
	openCodeManaged = "// laraenv:managed"
)

type HookPaths struct {
	ClaudeSettings string
	CodexHooks     string
	KimiConfig     string
	OpenCodePlugin string
}

type HookInstallStatus struct {
	Provider  AgentID `json:"provider"`
	Label     string  `json:"label"`
	Installed bool    `json:"installed"`
	Path      string  `json:"path"`
	Reason    string  `json:"reason,omitempty"`
}

func DefaultHookPaths(getenv func(string) string, home string) HookPaths {
	dir := func(key, fallback string) string {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
		return fallback
	}
	xdg := dir("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	return HookPaths{
		ClaudeSettings: filepath.Join(dir("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "settings.json"),
		CodexHooks:     filepath.Join(dir("CODEX_HOME", filepath.Join(home, ".codex")), "hooks.json"),
		KimiConfig:     filepath.Join(dir("KIMI_CODE_HOME", filepath.Join(home, ".kimi-code")), "config.toml"),
		OpenCodePlugin: filepath.Join(xdg, "opencode", "plugins", "laraenv.js"),
	}
}

type hookProvider struct {
	id   AgentID
	path func(HookPaths) string
	// configRoot maps the hook file path to the directory whose existence
	// gates installation: for most providers that's the file's own parent,
	// but OpenCode's hook file lives in a "plugins" subfolder most users
	// never created, so its config root is the opencode folder one level up
	// (install may then create "plugins" itself).
	configRoot func(path string) string
	read       func(path string) (bool, error)     // installed?
	write      func(path, executable string) error // install (idempotent)
	clear      func(path string) error             // uninstall
}

func defaultConfigRoot(path string) string { return filepath.Dir(path) }

var jsonHookEvents = map[AgentID][]struct{ event, matcher string }{
	AgentClaude: {{"SessionStart", ""}, {"UserPromptSubmit", ""}, {"Stop", ""}, {"SubagentStart", ""}, {"SubagentStop", ""}, {"Notification", "permission_prompt|idle_prompt"}, {"SessionEnd", ""}},
	AgentCodex:  {{"SessionStart", ""}, {"UserPromptSubmit", ""}, {"Stop", ""}, {"SubagentStart", ""}, {"SubagentStop", ""}, {"PermissionRequest", ""}, {"SessionEnd", ""}},
}

var kimiHookEvents = []string{"SessionStart", "UserPromptSubmit", "Stop", "PermissionRequest", "SessionEnd"}

func hookProviders() []hookProvider {
	return []hookProvider{
		{id: AgentClaude, path: func(p HookPaths) string { return p.ClaudeSettings }, configRoot: defaultConfigRoot,
			read: jsonHooksInstalled, write: func(path, exe string) error { return installJSONHooks(path, exe, AgentClaude) }, clear: uninstallJSONHooks},
		{id: AgentCodex, path: func(p HookPaths) string { return p.CodexHooks }, configRoot: defaultConfigRoot,
			read: jsonHooksInstalled, write: func(path, exe string) error { return installJSONHooks(path, exe, AgentCodex) }, clear: uninstallJSONHooks},
		{id: AgentKimi, path: func(p HookPaths) string { return p.KimiConfig }, configRoot: defaultConfigRoot,
			read: kimiHooksInstalled, write: installKimiHooks, clear: uninstallKimiHooks},
		{id: AgentOpenCode, path: func(p HookPaths) string { return p.OpenCodePlugin },
			// p.OpenCodePlugin is "<opencode>/plugins/laraenv.js"; the config
			// root is "<opencode>", the parent of "plugins".
			configRoot: func(path string) string { return filepath.Dir(filepath.Dir(path)) },
			read:       openCodePluginInstalled, write: installOpenCodePlugin, clear: uninstallOpenCodePlugin},
	}
}

func hookCommand(executable string, provider AgentID) string {
	return `"` + executable + `" ` + hookMarker + " " + string(provider)
}

func validateHookExecutable(executable string) error {
	executable = strings.TrimSpace(executable)
	if executable == "" || strings.ContainsAny(executable, "\"'\r\n") {
		return i18n.Errorf("errors.agents.executablePathUnsafeForHooks", nil)
	}
	return nil
}

func InstallHooks(paths HookPaths, executable string) []HookInstallStatus {
	exeErr := validateHookExecutable(executable)
	return eachHookProvider(paths, func(provider hookProvider, path string) (bool, error) {
		if exeErr != nil {
			return false, exeErr
		}
		if err := provider.write(path, strings.TrimSpace(executable)); err != nil {
			return false, err
		}
		return provider.read(path)
	})
}

func UninstallHooks(paths HookPaths) []HookInstallStatus {
	return eachHookProvider(paths, func(provider hookProvider, path string) (bool, error) {
		if err := provider.clear(path); err != nil {
			return false, err
		}
		return provider.read(path)
	})
}

func HookStatus(paths HookPaths) []HookInstallStatus {
	return eachHookProvider(paths, func(provider hookProvider, path string) (bool, error) { return provider.read(path) })
}

func eachHookProvider(paths HookPaths, action func(hookProvider, string) (bool, error)) []HookInstallStatus {
	var statuses []HookInstallStatus
	for _, provider := range hookProviders() {
		path := provider.path(paths)
		status := HookInstallStatus{Provider: provider.id, Label: providerLabel(provider.id), Path: path}
		if info, err := os.Stat(provider.configRoot(path)); err != nil || !info.IsDir() {
			status.Reason = i18n.T("backend.agents.hookConfigFolderNotFound", map[string]any{"label": status.Label})
			statuses = append(statuses, status)
			continue
		}
		installed, err := action(provider, path)
		status.Installed = installed
		if err != nil {
			status.Reason = err.Error()
		}
		statuses = append(statuses, status)
	}
	return statuses
}

// ---- Claude / Codex: {"hooks": {"Event": [{"matcher"?, "hooks": [{"type":"command","command"}]}]}}

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, i18n.Wrapf(err, "errors.agents.notJSONObject", map[string]any{"file": filepath.Base(path)})
	}
	if object == nil {
		object = map[string]any{}
	}
	return object, nil
}

func writeJSONObject(path string, object map[string]any) error {
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(data, '\n'), 0644)
}

func hookGroupIsLaraEnv(group any) bool {
	entry, _ := group.(map[string]any)
	hooks, _ := entry["hooks"].([]any)
	for _, hook := range hooks {
		inner, _ := hook.(map[string]any)
		if command, _ := inner["command"].(string); strings.Contains(command, hookMarker) {
			return true
		}
	}
	return false
}

// validateJSONHooksShape rejects a "hooks" section whose shape LaraEnv does
// not understand, so install/uninstall/status never rewrite (or silently
// misreport) a file some other tool wrote in a shape we can't safely parse.
// A non-map entry inside a valid event array is still tolerated: it isn't a
// LaraEnv group and hookGroupIsLaraEnv already skips it. An entry's own inner
// "hooks" array, however, is rejected when malformed (not an array, or
// containing a non-object element) because hookGroupIsLaraEnv must type-assert
// into each of those elements to look for LaraEnv's marker command.
func validateJSONHooksShape(object map[string]any, path string) error {
	raw, present := object["hooks"]
	if !present {
		return nil
	}
	hooks, ok := raw.(map[string]any)
	if !ok {
		return i18n.Errorf("errors.agents.hooksShapeUnexpected", map[string]any{"file": filepath.Base(path)})
	}
	shapeErr := i18n.Errorf("errors.agents.hooksShapeUnexpected", map[string]any{"file": filepath.Base(path)})
	for _, list := range hooks {
		entries, ok := list.([]any)
		if !ok {
			return shapeErr
		}
		for _, entry := range entries {
			group, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			innerRaw, present := group["hooks"]
			if !present {
				continue
			}
			inner, ok := innerRaw.([]any)
			if !ok {
				return shapeErr
			}
			for _, hook := range inner {
				if _, ok := hook.(map[string]any); !ok {
					return shapeErr
				}
			}
		}
	}
	return nil
}

func stripLaraEnvJSONHooks(object map[string]any) {
	hooks, _ := object["hooks"].(map[string]any)
	for event, list := range hooks {
		entries, _ := list.([]any)
		kept := entries[:0:0]
		for _, entry := range entries {
			if !hookGroupIsLaraEnv(entry) {
				kept = append(kept, entry)
				continue
			}
			// A provider/user may merge commands into one matcher group. Remove
			// only our marked commands, preserving every foreign sibling.
			group, _ := entry.(map[string]any)
			inner, _ := group["hooks"].([]any)
			remaining := make([]any, 0, len(inner))
			for _, hook := range inner {
				item, _ := hook.(map[string]any)
				command, _ := item["command"].(string)
				if !strings.Contains(command, hookMarker) {
					remaining = append(remaining, hook)
				}
			}
			if len(remaining) > 0 {
				group["hooks"] = remaining
				kept = append(kept, group)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if len(hooks) == 0 {
		delete(object, "hooks")
	}
}

func installJSONHooks(path, executable string, provider AgentID) error {
	object, err := readJSONObject(path)
	if err != nil {
		return err
	}
	if err := validateJSONHooksShape(object, path); err != nil {
		return err
	}
	stripLaraEnvJSONHooks(object) // drop stale entries (old executable path) first
	hooks, _ := object["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	command := hookCommand(executable, provider)
	for _, definition := range jsonHookEvents[provider] {
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}
		if definition.matcher != "" {
			group["matcher"] = definition.matcher
		}
		existing, _ := hooks[definition.event].([]any)
		hooks[definition.event] = append(existing, group)
	}
	object["hooks"] = hooks
	return writeJSONObject(path, object)
}

func uninstallJSONHooks(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	object, err := readJSONObject(path)
	if err != nil {
		return err
	}
	if err := validateJSONHooksShape(object, path); err != nil {
		return err
	}
	stripLaraEnvJSONHooks(object)
	return writeJSONObject(path, object)
}

func jsonHooksInstalled(path string) (bool, error) {
	object, err := readJSONObject(path)
	if err != nil {
		return false, err
	}
	if err := validateJSONHooksShape(object, path); err != nil {
		return false, err
	}
	hooks, _ := object["hooks"].(map[string]any)
	for _, list := range hooks {
		entries, _ := list.([]any)
		for _, entry := range entries {
			if hookGroupIsLaraEnv(entry) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ---- Kimi: TOML [[hooks]] entries inside a delimited block.

func kimiBlock(executable string) string {
	var b strings.Builder
	b.WriteString(kimiBlockBegin + "\n")
	for _, event := range kimiHookEvents {
		fmt.Fprintf(&b, "[[hooks]]\nevent = %q\ncommand = '%s'\ntimeout = 10\n\n", event, hookCommand(executable, AgentKimi))
	}
	b.WriteString(kimiBlockEnd + "\n")
	return b.String()
}

// stripKimiBlock removes a previously-installed LaraEnv block (including the
// blank separator line install adds before it) from text, returning it
// unchanged if no block is present. install+uninstall round-trips exactly
// when the file ended with a single trailing newline before install.
func stripKimiBlock(text string) string {
	start := strings.Index(text, kimiBlockBegin)
	if start < 0 {
		return text
	}
	endMarker := strings.Index(text[start:], kimiBlockEnd)
	if endMarker < 0 {
		return text
	}
	end := start + endMarker + len(kimiBlockEnd)
	if end < len(text) && text[end] == '\n' {
		end++
	}
	before, after := text[:start], text[end:]
	// install prepends "\n"+block to non-empty content; remove that
	// separator newline along with the block so uninstall is exact.
	before = strings.TrimSuffix(before, "\n")
	return before + after
}

func installKimiHooks(path, executable string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := stripKimiBlock(string(data))
	if text != "" {
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n"
	}
	text += kimiBlock(executable)
	return atomicfile.Write(path, []byte(text), 0644)
}

func uninstallKimiHooks(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stripped := stripKimiBlock(string(data))
	if stripped == string(data) {
		return nil
	}
	return atomicfile.Write(path, []byte(stripped), 0644)
}

func kimiHooksInstalled(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(data), kimiBlockBegin) && strings.Contains(string(data), hookMarker), nil
}

// ---- OpenCode: a managed plugin file.

func openCodePlugin(executable string) string {
	quoted, _ := json.Marshal(executable) // JS string literal with escaped backslashes
	template := `// laraenv:managed
// Written by LaraEnv. Reports agent lifecycle for terminals LaraEnv owns.
// Only session ids and directories are forwarded; never message content.
const exe = %s;
const terminalId = process.env.LARAENV_TERMINAL_ID;
const managedLaunch = process.env.LARAENV_AGENT_SESSION;

export const LaraEnvPlugin = async ({ $ }) => {
  if (!terminalId || managedLaunch) return {};
  const report = async (event, sessionId, directory) => {
    if (!sessionId) return;
    try { await $` + "`${exe} --agent-hook opencode ${event} ${sessionId} ${directory || \"\"}`" + `.quiet(); } catch {}
  };
  return {
    event: async ({ event }) => {
      const p = event.properties || {};
      const info = p.info || {};
      switch (event.type) {
        case "session.created": return report("session.created", info.id, info.directory);
        case "session.deleted": return report("session.deleted", info.id, info.directory);
        case "session.idle": return report("session.idle", p.sessionID || info.id, info.directory);
        case "permission.asked": return report("permission.asked", p.sessionID || info.sessionID, info.directory);
        case "message.updated": if (info.role === "user") return report("message.user", info.sessionID, info.directory); return;
        default: return;
      }
    },
  };
};
`
	return fmt.Sprintf(template, string(quoted))
}

func openCodePluginInstalled(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(string(data), openCodeManaged), nil
}

func installOpenCodePlugin(path, executable string) error {
	if data, err := os.ReadFile(path); err == nil && !strings.HasPrefix(string(data), openCodeManaged) {
		return i18n.Errorf("errors.agents.pluginFileNotManaged", nil)
	}
	return atomicfile.Write(path, []byte(openCodePlugin(executable)), 0644)
}

func uninstallOpenCodePlugin(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(data), openCodeManaged) {
		return nil
	}
	return os.Remove(path)
}
