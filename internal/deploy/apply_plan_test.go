package deploy

import (
	"encoding/base64"
	"strings"
	"testing"
)

func planStep(t *testing.T, steps []Step, title string) Step {
	t.Helper()
	for _, s := range steps {
		if strings.Contains(s.Title, title) {
			return s
		}
	}
	t.Fatalf("no step titled %q in plan: %v", title, stepTitles(steps))
	return Step{}
}

func stepTitles(steps []Step) []string {
	out := make([]string, len(steps))
	for i, s := range steps {
		out[i] = s.Title
	}
	return out
}

func TestBuildPlanClonePreservesRuntimeData(t *testing.T) {
	cfg := testCfg()
	steps := BuildPlan(cfg, nil, "", true)
	clone := planStep(t, steps, "Clone repo")
	keep := "'/var/www/.laraenv-keep-my-app'"
	for _, want := range []string{
		// .env and storage/ moved aside before the wipe...
		"sudo cp -a '/var/www/my-app'/.env " + keep + "/.env",
		"sudo mv '/var/www/my-app'/storage " + keep + "/storage",
		"sudo rm -rf '/var/www/my-app'",
		// ...and restored over the fresh clone, keep dir cleaned LAST so a
		// failed clone leaves the data for the next attempt.
		"sudo mv " + keep + "/.env '/var/www/my-app'/.env",
		"sudo mv " + keep + "/storage '/var/www/my-app'/storage",
		"sudo rm -rf " + keep + " &&",
	} {
		if !strings.Contains(clone.Cmd, want) {
			t.Errorf("clone step missing %q in:\n%s", want, clone.Cmd)
		}
	}
	// "sudo rm -rf <keep> &&" (no /storage suffix) is the final cleanup —
	// it must come after git clone so a failed clone keeps the data.
	if cleanIdx, cloneIdx := strings.Index(clone.Cmd, "sudo rm -rf "+keep+" &&"), strings.Index(clone.Cmd, "git clone"); cleanIdx < cloneIdx {
		t.Error("keep dir must only be cleaned after the clone+restore succeed")
	}
}

func TestBuildDestroyPlanRemovesKeepDir(t *testing.T) {
	cfg := testCfg()
	steps := BuildDestroyPlan(cfg)
	wipe := planStep(t, steps, "Wipe deploy dir")
	if !strings.Contains(wipe.Cmd, "'/var/www/.laraenv-keep-my-app'") {
		t.Errorf("destroy must remove the keep dir too:\n%s", wipe.Cmd)
	}
}

func TestBuildPlanLegacyBuildStep(t *testing.T) {
	cfg := testCfg()
	cfg.BuildScript = "composer install --no-dev"
	steps := BuildPlan(cfg, nil, cfg.BuildScript, true)
	build := planStep(t, steps, "Run build script")
	if !strings.Contains(build.Cmd, "composer install --no-dev") {
		t.Errorf("legacy build step lost the script:\n%s", build.Cmd)
	}
	if strings.Contains(build.Cmd, "LARAENV_FORCE") {
		t.Error("legacy build step must not set LARAENV_FORCE")
	}
}

func TestBuildPlanPresetBuildStep(t *testing.T) {
	cfg := testCfg()
	pipeline, _, _ := RenderDeployScript(BuiltinPresets()[0].Script, cfg)
	steps := BuildPlan(cfg, nil, pipeline, false)
	build := planStep(t, steps, "Run deploy pipeline")
	if !strings.HasPrefix(build.Cmd, "export LARAENV_FORCE=1\n") {
		t.Error("preset build step must force the pipeline on the fresh clone")
	}
	if !strings.Contains(build.Cmd, ".laraenv-deployed-sha") {
		t.Error("preset build step missing the rendered pipeline body")
	}
}

func TestBuildPlanAutopullLegacyTemplate(t *testing.T) {
	cfg := testCfg()
	cfg.AutoDeploy = true
	cfg.BuildScript = "composer install"
	steps := BuildPlan(cfg, nil, cfg.BuildScript, true)
	install := planStep(t, steps, "Install auto-pull script")

	script := decodeAutopull(t, install.Cmd)
	for _, want := range []string{
		"git fetch --all --prune",
		"git reset --hard origin/main",
		`[laraenv-autopull] $(date -u +%FT%TZ) $BEFORE -> $AFTER`,
		"composer install",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("legacy autopull missing %q in:\n%s", want, script)
		}
	}
}

func TestBuildPlanAutopullPresetPipeline(t *testing.T) {
	cfg := testCfg()
	cfg.AutoDeploy = true
	pipeline, _, _ := RenderDeployScript(BuiltinPresets()[0].Script, cfg)
	steps := BuildPlan(cfg, nil, pipeline, false)
	install := planStep(t, steps, "Install auto-pull script")

	script := decodeAutopull(t, install.Cmd)
	for _, want := range []string{
		"BEFORE=$(git rev-parse HEAD)",
		".laraenv-deployed-sha",
		`[laraenv-autopull] $(date -u +%FT%TZ) $BEFORE -> $AFTER`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("preset autopull missing %q in:\n%s", want, script)
		}
	}
	// A pipeline containing the old heredoc delimiter must survive intact —
	// that's the whole point of the base64 write.
	cfgEvil := testCfg()
	cfgEvil.AutoDeploy = true
	evil := "echo L4E_PULL_EOF\necho done"
	stepsEvil := BuildPlan(cfgEvil, nil, evil, false)
	scriptEvil := decodeAutopull(t, planStep(t, stepsEvil, "Install auto-pull script").Cmd)
	if !strings.Contains(scriptEvil, "echo L4E_PULL_EOF") {
		t.Error("pipeline containing the heredoc delimiter was mangled")
	}
}

// decodeAutopull extracts and decodes the base64 payload from the
// `printf '%s' '<b64>' | base64 -d > path` install command.
func decodeAutopull(t *testing.T, cmd string) string {
	t.Helper()
	start := strings.Index(cmd, "printf '%s' '")
	if start < 0 {
		t.Fatalf("autopull step is not a base64 write:\n%s", cmd)
	}
	rest := cmd[start+len("printf '%s' '"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		t.Fatalf("unterminated base64 payload:\n%s", cmd)
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:end])
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	return string(raw)
}
