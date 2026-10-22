package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookTestPaths(t *testing.T) HookPaths {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".claude", ".codex", ".kimi-code", filepath.Join(".config", "opencode", "plugins")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return HookPaths{
		ClaudeSettings: filepath.Join(root, ".claude", "settings.json"),
		CodexHooks:     filepath.Join(root, ".codex", "hooks.json"),
		KimiConfig:     filepath.Join(root, ".kimi-code", "config.toml"),
		OpenCodePlugin: filepath.Join(root, ".config", "opencode", "plugins", "laraenv.js"),
	}
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s is not valid JSON: %v\n%s", path, err, data)
	}
	return out
}

func statusFor(statuses []HookInstallStatus, id AgentID) HookInstallStatus {
	for _, status := range statuses {
		if status.Provider == id {
			return status
		}
	}
	return HookInstallStatus{}
}

const testExe = `C:\Program Files\LaraEnv\LaraEnv.exe`

func TestInstallHooksPreservesExistingClaudeAndCodexEntries(t *testing.T) {
	paths := hookTestPaths(t)
	existing := `{
  "model": "opus",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "notify-send done"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "audit.sh"}]}]
  }
}
`
	if err := os.WriteFile(paths.ClaudeSettings, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}
	statuses := InstallHooks(paths, testExe)
	for _, id := range []AgentID{AgentClaude, AgentCodex, AgentKimi, AgentOpenCode} {
		if status := statusFor(statuses, id); !status.Installed || status.Reason != "" {
			t.Fatalf("%s not installed: %#v", id, status)
		}
	}

	claude := readJSONFile(t, paths.ClaudeSettings)
	if claude["model"] != "opus" {
		t.Fatalf("unrelated key lost: %#v", claude)
	}
	hooks := claude["hooks"].(map[string]any)
	if len(hooks["Stop"].([]any)) != 2 || len(hooks["PreToolUse"].([]any)) != 1 {
		t.Fatalf("existing hooks were altered: %#v", hooks)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SubagentStart", "SubagentStop", "Notification", "SessionEnd"} {
		if !hookListContains(t, hooks[event], `"`+testExe+`" --agent-hook claude`) {
			t.Fatalf("Claude %s hook missing: %#v", event, hooks[event])
		}
	}
	notification := hooks["Notification"].([]any)[0].(map[string]any)
	if notification["matcher"] != "permission_prompt|idle_prompt" {
		t.Fatalf("Notification matcher = %#v", notification["matcher"])
	}

	codex := readJSONFile(t, paths.CodexHooks)
	codexHooks := codex["hooks"].(map[string]any)
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SubagentStart", "SubagentStop", "PermissionRequest", "SessionEnd"} {
		if !hookListContains(t, codexHooks[event], `"`+testExe+`" --agent-hook codex`) {
			t.Fatalf("Codex %s hook missing: %#v", event, codexHooks[event])
		}
	}
}

func hookListContains(t *testing.T, list any, command string) bool {
	t.Helper()
	entries, _ := list.([]any)
	for _, entry := range entries {
		group, _ := entry.(map[string]any)
		for _, hook := range group["hooks"].([]any) {
			if hook.(map[string]any)["command"] == command {
				return true
			}
		}
	}
	return false
}

func TestInstallHooksWritesKimiBlockAndOpenCodePlugin(t *testing.T) {
	paths := hookTestPaths(t)
	if err := os.WriteFile(paths.KimiConfig, []byte("model = \"kimi-k2\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	InstallHooks(paths, testExe)

	kimi, _ := os.ReadFile(paths.KimiConfig)
	text := string(kimi)
	if !strings.HasPrefix(text, "model = \"kimi-k2\"\n") || strings.Count(text, "# laraenv:begin") != 1 || strings.Count(text, "# laraenv:end") != 1 {
		t.Fatalf("Kimi config block malformed:\n%s", text)
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "PermissionRequest", "SessionEnd"} {
		if !strings.Contains(text, "event = \""+event+"\"") {
			t.Fatalf("Kimi %s hook missing:\n%s", event, text)
		}
	}
	if !strings.Contains(text, `command = '"`+testExe+`" --agent-hook kimi'`) {
		t.Fatalf("Kimi command missing or wrongly quoted:\n%s", text)
	}

	plugin, err := os.ReadFile(paths.OpenCodePlugin)
	if err != nil {
		t.Fatal(err)
	}
	js := string(plugin)
	if !strings.HasPrefix(js, "// laraenv:managed") || !strings.Contains(js, "LARAENV_TERMINAL_ID") || !strings.Contains(js, "LARAENV_AGENT_SESSION") {
		t.Fatalf("OpenCode plugin missing guard or marker:\n%s", js)
	}
	for _, event := range []string{"session.created", "session.idle", "permission.asked", "session.deleted", "message.updated"} {
		if !strings.Contains(js, `"`+event+`"`) {
			t.Fatalf("OpenCode plugin does not handle %s:\n%s", event, js)
		}
	}
	if !strings.Contains(js, strings.ReplaceAll(testExe, `\`, `\\`)) {
		t.Fatalf("OpenCode plugin does not reference the executable:\n%s", js)
	}
}

func TestInstallHooksIsIdempotentAndUninstallRestoresFiles(t *testing.T) {
	paths := hookTestPaths(t)
	claudeBefore := "{\n  \"model\": \"opus\"\n}\n"
	kimiBefore := "model = \"kimi-k2\"\n"
	if err := os.WriteFile(paths.ClaudeSettings, []byte(claudeBefore), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.KimiConfig, []byte(kimiBefore), 0644); err != nil {
		t.Fatal(err)
	}
	InstallHooks(paths, testExe)
	first, _ := os.ReadFile(paths.ClaudeSettings)
	InstallHooks(paths, testExe)
	second, _ := os.ReadFile(paths.ClaudeSettings)
	if string(first) != string(second) {
		t.Fatalf("second install changed the file:\n%s\n---\n%s", first, second)
	}
	kimiTwice, _ := os.ReadFile(paths.KimiConfig)
	if strings.Count(string(kimiTwice), "# laraenv:begin") != 1 {
		t.Fatalf("Kimi block duplicated:\n%s", kimiTwice)
	}

	statuses := UninstallHooks(paths)
	for _, status := range statuses {
		if status.Installed {
			t.Fatalf("still installed after uninstall: %#v", status)
		}
	}
	claudeAfter := readJSONFile(t, paths.ClaudeSettings)
	if claudeAfter["model"] != "opus" {
		t.Fatalf("unrelated key lost on uninstall: %#v", claudeAfter)
	}
	if _, hasHooks := claudeAfter["hooks"]; hasHooks {
		t.Fatalf("empty hooks map left behind: %#v", claudeAfter)
	}
	kimiAfter, _ := os.ReadFile(paths.KimiConfig)
	if string(kimiAfter) != kimiBefore {
		t.Fatalf("Kimi config not restored:\n%q", kimiAfter)
	}
	if _, err := os.Stat(paths.OpenCodePlugin); !os.IsNotExist(err) {
		t.Fatal("OpenCode plugin was not removed")
	}
	codexAfter := readJSONFile(t, paths.CodexHooks)
	if hooks, ok := codexAfter["hooks"].(map[string]any); ok && len(hooks) != 0 {
		t.Fatalf("Codex hooks left behind: %#v", hooks)
	}
	// Executable moved (after an update): re-install replaces the command.
	InstallHooks(paths, testExe)
	InstallHooks(paths, `D:\LaraEnv\LaraEnv.exe`)
	claudeMoved := readJSONFile(t, paths.ClaudeSettings)
	stop := claudeMoved["hooks"].(map[string]any)["Stop"]
	if hookListContains(t, stop, `"`+testExe+`" --agent-hook claude`) || !hookListContains(t, stop, `"D:\LaraEnv\LaraEnv.exe" --agent-hook claude`) {
		t.Fatalf("stale executable path kept: %#v", stop)
	}
}

func TestHookStatusSkipsMissingProviderFolders(t *testing.T) {
	paths := hookTestPaths(t)
	if err := os.RemoveAll(filepath.Dir(paths.KimiConfig)); err != nil {
		t.Fatal(err)
	}
	statuses := InstallHooks(paths, testExe)
	kimi := statusFor(statuses, AgentKimi)
	if kimi.Installed || !strings.Contains(kimi.Reason, "config folder not found") {
		t.Fatalf("missing Kimi folder must be skipped: %#v", kimi)
	}
	if _, err := os.Stat(paths.KimiConfig); !os.IsNotExist(err) {
		t.Fatal("installer created a provider folder")
	}
	status := HookStatus(paths)
	if !statusFor(status, AgentClaude).Installed || statusFor(status, AgentKimi).Installed {
		t.Fatalf("unexpected status: %#v", status)
	}
	// Unmanaged OpenCode plugin file is never overwritten or deleted.
	if err := os.WriteFile(paths.OpenCodePlugin, []byte("export const Mine = async () => ({});\n"), 0644); err != nil {
		t.Fatal(err)
	}
	oc := statusFor(InstallHooks(paths, testExe), AgentOpenCode)
	if oc.Installed || !strings.Contains(oc.Reason, "not managed by LaraEnv") {
		t.Fatalf("foreign plugin file must not be replaced: %#v", oc)
	}
	UninstallHooks(paths)
	if data, _ := os.ReadFile(paths.OpenCodePlugin); !strings.Contains(string(data), "Mine") {
		t.Fatal("foreign plugin file was deleted")
	}
}

// TestOpenCodeConfigRootIsTheOpenCodeFolderNotPlugins covers the common case
// where a user has never run OpenCode's plugin system: ~/.config/opencode
// exists but its "plugins" subfolder does not. Gating on filepath.Dir(path)
// (the plugins folder itself) would skip installation for nearly everyone;
// the config root must be the opencode folder one level up, and install must
// then create "plugins" itself.
func TestOpenCodeConfigRootIsTheOpenCodeFolderNotPlugins(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".claude", ".codex", ".kimi-code"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// .config/opencode exists, but its plugins/ subfolder does not.
	if err := os.MkdirAll(filepath.Join(root, ".config", "opencode"), 0755); err != nil {
		t.Fatal(err)
	}
	paths := HookPaths{
		ClaudeSettings: filepath.Join(root, ".claude", "settings.json"),
		CodexHooks:     filepath.Join(root, ".codex", "hooks.json"),
		KimiConfig:     filepath.Join(root, ".kimi-code", "config.toml"),
		OpenCodePlugin: filepath.Join(root, ".config", "opencode", "plugins", "laraenv.js"),
	}

	oc := statusFor(InstallHooks(paths, testExe), AgentOpenCode)
	if !oc.Installed || oc.Reason != "" {
		t.Fatalf("OpenCode install must succeed when only the opencode folder exists: %#v", oc)
	}
	if data, err := os.ReadFile(paths.OpenCodePlugin); err != nil || !strings.HasPrefix(string(data), openCodeManaged) {
		t.Fatalf("install did not create the plugins folder and file: err=%v data=%q", err, data)
	}

	// Without even the opencode folder, OpenCode must still be skipped.
	root2 := t.TempDir()
	for _, dir := range []string{".claude", ".codex", ".kimi-code"} {
		if err := os.MkdirAll(filepath.Join(root2, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	paths2 := HookPaths{
		ClaudeSettings: filepath.Join(root2, ".claude", "settings.json"),
		CodexHooks:     filepath.Join(root2, ".codex", "hooks.json"),
		KimiConfig:     filepath.Join(root2, ".kimi-code", "config.toml"),
		OpenCodePlugin: filepath.Join(root2, ".config", "opencode", "plugins", "laraenv.js"),
	}
	oc2 := statusFor(InstallHooks(paths2, testExe), AgentOpenCode)
	if oc2.Installed || !strings.Contains(oc2.Reason, "config folder not found") {
		t.Fatalf("OpenCode must be skipped when the opencode folder itself is missing: %#v", oc2)
	}
	if _, err := os.Stat(paths2.OpenCodePlugin); !os.IsNotExist(err) {
		t.Fatal("installer created files for a provider whose config folder is missing")
	}
}

func TestInstallHooksRejectsUnsafeExecutable(t *testing.T) {
	paths := hookTestPaths(t)
	for _, status := range InstallHooks(paths, "C:\\bad\"quote\\LaraEnv.exe") {
		if status.Installed || status.Reason == "" {
			t.Fatalf("unsafe executable accepted: %#v", status)
		}
	}
}

func TestDefaultHookPathsHonourProviderHomeVariables(t *testing.T) {
	env := map[string]string{
		"CLAUDE_CONFIG_DIR": `D:\cfg\claude`, "CODEX_HOME": `D:\cfg\codex`, "KIMI_CODE_HOME": `D:\cfg\kimi`, "XDG_CONFIG_HOME": `D:\cfg\xdg`,
	}
	got := DefaultHookPaths(func(key string) string { return env[key] }, `C:\Users\me`)
	want := HookPaths{
		ClaudeSettings: filepath.Join(`D:\cfg\claude`, "settings.json"),
		CodexHooks:     filepath.Join(`D:\cfg\codex`, "hooks.json"),
		KimiConfig:     filepath.Join(`D:\cfg\kimi`, "config.toml"),
		OpenCodePlugin: filepath.Join(`D:\cfg\xdg`, "opencode", "plugins", "laraenv.js"),
	}
	if got != want {
		t.Fatalf("got %#v want %#v", got, want)
	}
	got = DefaultHookPaths(func(string) string { return "" }, `C:\Users\me`)
	want = HookPaths{
		ClaudeSettings: filepath.Join(`C:\Users\me`, ".claude", "settings.json"),
		CodexHooks:     filepath.Join(`C:\Users\me`, ".codex", "hooks.json"),
		KimiConfig:     filepath.Join(`C:\Users\me`, ".kimi-code", "config.toml"),
		OpenCodePlugin: filepath.Join(`C:\Users\me`, ".config", "opencode", "plugins", "laraenv.js"),
	}
	if got != want {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestInstallHooksRefusesMalformedHooksSections(t *testing.T) {
	cases := map[string]string{
		"hooks not an object":            "{\n  \"hooks\": \"oops\"\n}\n",
		"event not an array":             "{\n  \"hooks\": {\n    \"Stop\": \"weird\"\n  }\n}\n",
		"inner hooks element not object": "{\n  \"hooks\": {\n    \"Stop\": [{\"hooks\": [\"echo hi\"]}]\n  }\n}\n",
		"inner hooks element null":       "{\n  \"hooks\": {\n    \"Stop\": [{\"hooks\": [null]}]\n  }\n}\n",
		"inner hooks value not an array": "{\n  \"hooks\": {\n    \"Stop\": [{\"hooks\": \"nope\"}]\n  }\n}\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			paths := hookTestPaths(t)
			if err := os.WriteFile(paths.ClaudeSettings, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			claude := statusFor(InstallHooks(paths, testExe), AgentClaude)
			if claude.Installed || !strings.Contains(claude.Reason, "unexpected shape") {
				t.Fatalf("malformed file was not refused: %#v", claude)
			}
			after, _ := os.ReadFile(paths.ClaudeSettings)
			if string(after) != content {
				t.Fatalf("malformed file was modified:\n%s", after)
			}
			claude = statusFor(UninstallHooks(paths), AgentClaude)
			if claude.Installed || !strings.Contains(claude.Reason, "unexpected shape") {
				t.Fatalf("uninstall did not refuse the malformed file: %#v", claude)
			}
			after, _ = os.ReadFile(paths.ClaudeSettings)
			if string(after) != content {
				t.Fatalf("uninstall modified the malformed file:\n%s", after)
			}
			// Codex uses the same JSON path.
			if err := os.WriteFile(paths.CodexHooks, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
			if codex := statusFor(InstallHooks(paths, testExe), AgentCodex); codex.Installed || !strings.Contains(codex.Reason, "unexpected shape") {
				t.Fatalf("Codex malformed file was not refused: %#v", codex)
			}
		})
	}
}
