package deploy_test

import (
	"strings"
	"testing"

	"lara4ever/internal/deploy"
)

// Losing preset rendering or the rendered build step during extraction must
// fail at the public, account-independent package boundary.
func TestNeutralDomainRendersPresetIntoApplyPlan(t *testing.T) {
	cfg := deploy.ApplyConfig{
		Name: "storefront", ProjectKind: "static", Domain: "example.test",
		Environment: "staging", GitURL: "https://example.test/repo.git",
		WebServer: "nginx", PresetID: "fixture-preset",
		PresetVars: map[string]string{"greeting": "hello"},
	}
	rendered, err := deploy.RenderDeployPipeline(cfg, func(id string) (string, bool) {
		if id != "fixture-preset" {
			return "", false
		}
		return "printf '%s\\n' '{domain}|{deploy_root}|{branch}|{greeting}'", true
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "printf '%s\\n' 'staging.example.test|/var/www/storefront|main|hello'"
	if rendered.Pipeline != want || rendered.Legacy || len(rendered.Unresolved) != 0 {
		t.Fatalf("unexpected rendered preset: %+v", rendered)
	}
	for _, step := range deploy.BuildPlan(cfg, nil, rendered.Pipeline, rendered.Legacy) {
		if step.Title == "Run deploy pipeline" {
			if !strings.Contains(step.Cmd, want) || !strings.Contains(step.Cmd, "cd '/var/www/storefront'") {
				t.Fatalf("apply step lost the rendered pipeline or deploy directory:\n%s", step.Cmd)
			}
			return
		}
	}
	t.Fatal("apply plan omitted the rendered deploy pipeline")
}
