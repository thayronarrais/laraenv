package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"lara4ever/internal/i18n"
)

// This file is the single source of truth for turning a deploy preset
// (bash with {token} placeholders) into the exact script that executes on
// the remote. The preview RPC, the Apply build step, the "Deploy now"
// runner, and the autopull cron file all go through these functions, so
// what the user previews is byte-identical to what runs.

// presetTokenRe matches {token} placeholders: lowercase identifier inside
// single braces. Bash's ${VAR} expansion is protected by the $-guard in
// RenderDeployScript, not by the regex itself.
var presetTokenRe = regexp.MustCompile(`\{([a-z_][a-z0-9_]*)\}`)

// builtinVars derives the always-available substitution variables from the
// deployment's apply config. Keys here are reserved — preset custom vars
// can't shadow them.
func builtinVars(cfg ApplyConfig) map[string]string {
	branch := cfg.GitBranch
	if branch == "" {
		branch = "main"
	}
	phpVer := cfg.PHPVersion
	if phpVer == "" {
		phpVer = "8.3"
	}
	name := safeName(cfg.Name)
	return map[string]string{
		"name":        name,
		"domain":      cfg.EffectiveDomain(),
		"raw_domain":  cfg.Domain,
		"branch":      branch,
		"php_version": phpVer,
		"deploy_root": "/var/www/" + name,
		"environment": cfg.Environment,
		"git_url":     cfg.GitURL,
		"web_server":  cfg.WebServer,
		"database":    cfg.Database,
	}
}

// RenderDeployScript substitutes {token} placeholders in a preset script.
// Resolution order: built-in vars from cfg, then cfg.PresetVars — but a
// custom var can never shadow a built-in key (attempts are reported in
// warnings and ignored). Tokens preceded by '$' are left alone so bash's
// ${VAR} expansion survives. Unknown tokens stay as-is and are returned in
// unresolved so the UI can flag them before anything runs.
func RenderDeployScript(script string, cfg ApplyConfig) (rendered string, unresolved []string, warnings []string) {
	vars := builtinVars(cfg)
	for k, v := range cfg.PresetVars {
		if _, reserved := vars[k]; reserved {
			warnings = append(warnings, i18n.T("backend.cloud.presetVarBuiltinIgnored", map[string]any{"name": k}))
			continue
		}
		vars[k] = v
	}

	seenUnresolved := map[string]bool{}
	var b strings.Builder
	last := 0
	for _, m := range presetTokenRe.FindAllStringSubmatchIndex(script, -1) {
		start, end := m[0], m[1]
		key := script[m[2]:m[3]]
		b.WriteString(script[last:start])
		// {token} right after '$' is bash parameter expansion — pass through.
		if start > 0 && script[start-1] == '$' {
			b.WriteString(script[start:end])
			last = end
			continue
		}
		if val, ok := vars[key]; ok {
			b.WriteString(val)
		} else {
			b.WriteString(script[start:end])
			if !seenUnresolved[key] {
				seenUnresolved[key] = true
				unresolved = append(unresolved, key)
			}
		}
		last = end
	}
	b.WriteString(script[last:])
	sort.Strings(unresolved)
	sort.Strings(warnings)
	return b.String(), unresolved, warnings
}

// ResolveDeployScript picks the deploy pipeline source for a config:
//
//	ScriptOverride (per-deployment edited copy, still has {tokens})
//	→ preset.Script looked up via PresetID
//	→ legacy BuildScript, returned VERBATIM with legacy=true — old free-text
//	  scripts are never token-rendered, keeping pre-preset deployments
//	  byte-identical to what they ran before this feature existed.
//
// Returns script=="" when nothing is configured (deploy pipeline disabled).
func ResolveDeployScript(cfg ApplyConfig, lookup func(id string) (string, bool)) (script string, legacy bool, err error) {
	if s := strings.TrimSpace(cfg.ScriptOverride); s != "" {
		return cfg.ScriptOverride, false, nil
	}
	if cfg.PresetID != "" {
		if lookup == nil {
			return "", false, i18n.Errorf("errors.cloud.presetLookupUnavailable", map[string]any{"preset": cfg.PresetID})
		}
		s, ok := lookup(cfg.PresetID)
		if !ok {
			return "", false, i18n.Errorf("errors.cloud.deployPresetNotFound", map[string]any{"preset": cfg.PresetID})
		}
		return s, false, nil
	}
	// Legacy fallthrough — including the nothing-configured case, so
	// pre-preset deployments (with or without a build script) keep the old
	// plan/autopull templates byte-identical.
	return cfg.BuildScript, true, nil
}

// BuildDeployScript wraps a rendered pipeline with the shared execution
// preamble: strict mode, cd into the deploy root, and the per-deployment
// .laraenv-bin shim that pins `php` and `composer` to the configured PHP
// version without touching the host's global symlinks.
func BuildDeployScript(cfg ApplyConfig, pipeline string) string {
	vars := builtinVars(cfg)
	return fmt.Sprintf(`set -e
export COMPOSER_ALLOW_SUPERUSER=1
cd %[1]s
mkdir -p .laraenv-bin
ln -sf /usr/bin/php%[2]s .laraenv-bin/php
printf '#!/usr/bin/env bash\nexec /usr/bin/php%[2]s /usr/bin/composer "$@"\n' > .laraenv-bin/composer
chmod +x .laraenv-bin/composer
export PATH="$(pwd)/.laraenv-bin:$PATH"
echo "Using $(php --version | head -n1)"
%[3]s`, shQuote(vars["deploy_root"]), vars["php_version"], pipeline)
}

// RenderedScript is the preview payload returned to the UI. Script is the
// exact text that will execute (wrapper included); Pipeline is the rendered
// preset body alone, for the editor-style view.
type RenderedScript struct {
	Script     string   `json:"script"`
	Pipeline   string   `json:"pipeline"`
	Legacy     bool     `json:"legacy"`
	Unresolved []string `json:"unresolved"`
	Warnings   []string `json:"warnings"`
}

// DeployNowScript returns the exact script a manual "Deploy now" run
// executes. Preset pipelines already fetch/reset themselves; legacy build
// scripts don't, so those get a pull prepended. LARAENV_FORCE=1 makes an
// already-up-to-date repo still re-run the pipeline (the user explicitly
// asked for a deploy — migrate/optimize should run even with no new commits).
func DeployNowScript(cfg ApplyConfig, lookup func(id string) (string, bool)) (RenderedScript, error) {
	r, err := RenderDeployPipeline(cfg, lookup)
	if err != nil || r.Script == "" {
		return r, err
	}
	if r.Legacy {
		r.Pipeline = "git fetch --all --prune\ngit reset --hard \"origin/" + branchOrMain(cfg) + "\"\n" + r.Pipeline
		r.Script = BuildDeployScript(cfg, r.Pipeline)
	}
	r.Script = "export LARAENV_FORCE=1\n" + r.Script
	return r, nil
}

// RenderDeployPipeline runs the full resolve → render → wrap chain and
// returns the preview payload. Legacy scripts skip rendering (verbatim).
// An empty Script means no deploy pipeline is configured.
func RenderDeployPipeline(cfg ApplyConfig, lookup func(id string) (string, bool)) (RenderedScript, error) {
	src, legacy, err := ResolveDeployScript(cfg, lookup)
	if err != nil {
		return RenderedScript{}, err
	}
	if src == "" {
		return RenderedScript{Legacy: legacy}, nil
	}
	out := RenderedScript{Legacy: legacy, Pipeline: src}
	if !legacy {
		out.Pipeline, out.Unresolved, out.Warnings = RenderDeployScript(src, cfg)
	}
	out.Script = BuildDeployScript(cfg, out.Pipeline)
	return out, nil
}
