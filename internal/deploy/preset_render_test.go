package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/i18n"
)

// TestMain loads the real catalogues before running this package's tests, so
// i18n.T renders the actual English prose (with vars interpolated) instead
// of degrading to a bare key path — TestRenderDeployScriptCustomVarsAndShadowing
// below asserts on that English text ("branch"). See
// internal/agents/manager_test.go and internal/services/manager_test.go for
// the same pattern.
func TestMain(m *testing.M) {
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load langs:", err)
		os.Exit(1)
	}
	i18n.SetGlobal(set)
	os.Exit(m.Run())
}

func testCfg() ApplyConfig {
	return ApplyConfig{
		ProjectKind: "laravel",
		Name:        "my-app",
		Domain:      "example.com",
		Environment: "production",
		GitURL:      "https://github.com/acme/my-app.git",
		GitBranch:   "main",
		WebServer:   "nginx",
		PHPVersion:  "8.4",
		Database:    "mysql",
	}
}

func TestRenderDeployScriptSubstitutesBuiltins(t *testing.T) {
	out, unresolved, warnings := RenderDeployScript(
		"git reset --hard origin/{branch} # {name} on {domain} root={deploy_root} php={php_version}",
		testCfg(),
	)
	want := "git reset --hard origin/main # my-app on example.com root=/var/www/my-app php=8.4"
	if out != want {
		t.Errorf("rendered = %q, want %q", out, want)
	}
	if len(unresolved) != 0 || len(warnings) != 0 {
		t.Errorf("unresolved=%v warnings=%v, want none", unresolved, warnings)
	}
}

func TestRenderDeployScriptEffectiveDomain(t *testing.T) {
	cfg := testCfg()
	cfg.Environment = "development"
	out, _, _ := RenderDeployScript("{domain} {raw_domain}", cfg)
	if out != "dev.example.com example.com" {
		t.Errorf("rendered = %q", out)
	}
}

func TestRenderDeployScriptLeavesBashExpansionAlone(t *testing.T) {
	cfg := testCfg()
	cfg.PresetVars = map[string]string{"force": "1"}
	script := `if [ "${old_sha}" = "x" ]; then echo ${force:-0}; fi # {force}`
	out, unresolved, _ := RenderDeployScript(script, cfg)
	want := `if [ "${old_sha}" = "x" ]; then echo ${force:-0}; fi # 1`
	if out != want {
		t.Errorf("rendered = %q, want %q", out, want)
	}
	if len(unresolved) != 0 {
		t.Errorf("unresolved = %v, want none ($-prefixed tokens aren't ours)", unresolved)
	}
}

func TestRenderDeployScriptReportsUnresolved(t *testing.T) {
	out, unresolved, _ := RenderDeployScript("echo {mystery} {mystery} {another}", testCfg())
	if out != "echo {mystery} {mystery} {another}" {
		t.Errorf("rendered = %q (unknown tokens must pass through)", out)
	}
	if len(unresolved) != 2 || unresolved[0] != "another" || unresolved[1] != "mystery" {
		t.Errorf("unresolved = %v, want [another mystery]", unresolved)
	}
}

func TestRenderDeployScriptCustomVarsAndShadowing(t *testing.T) {
	cfg := testCfg()
	cfg.PresetVars = map[string]string{
		"api_key": "secret123",
		"branch":  "hacked", // built-in — must be ignored + warned
	}
	out, _, warnings := RenderDeployScript("deploy {branch} key={api_key}", cfg)
	if out != "deploy main key=secret123" {
		t.Errorf("rendered = %q", out)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "branch") {
		t.Errorf("warnings = %v, want shadow warning for branch", warnings)
	}
}

func TestResolveDeployScriptPriority(t *testing.T) {
	lookup := func(id string) (string, bool) {
		if id == "p1" {
			return "preset body", true
		}
		return "", false
	}

	cfg := testCfg()
	cfg.ScriptOverride = "override body"
	cfg.PresetID = "p1"
	cfg.BuildScript = "legacy body"
	s, legacy, err := ResolveDeployScript(cfg, lookup)
	if err != nil || s != "override body" || legacy {
		t.Errorf("override: s=%q legacy=%v err=%v", s, legacy, err)
	}

	cfg.ScriptOverride = ""
	s, legacy, err = ResolveDeployScript(cfg, lookup)
	if err != nil || s != "preset body" || legacy {
		t.Errorf("preset: s=%q legacy=%v err=%v", s, legacy, err)
	}

	cfg.PresetID = ""
	s, legacy, err = ResolveDeployScript(cfg, lookup)
	if err != nil || s != "legacy body" || !legacy {
		t.Errorf("legacy: s=%q legacy=%v err=%v", s, legacy, err)
	}

	cfg.BuildScript = ""
	s, legacy, err = ResolveDeployScript(cfg, lookup)
	if err != nil || s != "" || !legacy {
		t.Errorf("empty: s=%q legacy=%v err=%v (empty config must stay on the legacy path)", s, legacy, err)
	}

	cfg.PresetID = "missing"
	if _, _, err = ResolveDeployScript(cfg, lookup); err == nil {
		t.Error("missing preset: want error")
	}
}

func TestBuildDeployScriptWrapper(t *testing.T) {
	cfg := testCfg()
	out := BuildDeployScript(cfg, "echo hello")
	for _, want := range []string{
		"set -e",
		"cd '/var/www/my-app'",
		"ln -sf /usr/bin/php8.4 .laraenv-bin/php",
		`export PATH="$(pwd)/.laraenv-bin:$PATH"`,
		"echo hello",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("wrapper missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderDeployPipelineLegacyVerbatim(t *testing.T) {
	cfg := testCfg()
	cfg.BuildScript = "composer install # do not touch {branch}"
	out, err := RenderDeployPipeline(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Legacy {
		t.Error("want legacy=true")
	}
	if out.Pipeline != cfg.BuildScript {
		t.Errorf("legacy pipeline must be verbatim, got %q", out.Pipeline)
	}
	if !strings.Contains(out.Script, "{branch}") {
		t.Error("legacy script must keep {tokens} untouched")
	}
}

func TestBuiltinPresetsRenderClean(t *testing.T) {
	cfg := testCfg()
	for _, p := range BuiltinPresets() {
		rendered, unresolved, warnings := RenderDeployScript(p.Script, cfg)
		if len(unresolved) != 0 {
			t.Errorf("%s: unresolved tokens %v", p.ID, unresolved)
		}
		if len(warnings) != 0 {
			t.Errorf("%s: warnings %v", p.ID, warnings)
		}
		if strings.Contains(rendered, "{branch}") {
			t.Errorf("%s: {branch} not substituted", p.ID)
		}
		if !strings.Contains(rendered, ".laraenv-deployed-sha") {
			t.Errorf("%s: missing SHA marker logic", p.ID)
		}
	}
}
