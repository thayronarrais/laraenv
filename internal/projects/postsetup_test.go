package projects

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"lara4ever/internal/i18n"
)

func TestPostSetupSkipFrontendPreservesDatabaseEnvAndPHPSteps(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "my-app")
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".env.example"), []byte("DB_CONNECTION=sqlite\nDB_DATABASE=old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	nodeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeDir, "npm.cmd"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	cfg := PostSetupConfig{ProjectsRoot: root, Database: "mysql", MySQLBin: t.TempDir(), NodeDir: nodeDir, PHPExe: "not-a-php-exe", SkipFrontend: true}
	// A canceled context also prevents process execution if the frontend gate regresses.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var lines []string
	PostSetupLaravel(ctx, cfg, "my-app", func(line string) { lines = append(lines, line) })
	conn, name := dbInfoFromEnv(projectDir)
	if conn != "mysql" || name != "my_app" {
		t.Fatalf("patched DB settings = (%q, %q), want (mysql, my_app)", conn, name)
	}
	for _, want := range []string{
		i18n.T("backend.projects.creatingMysqlDb", map[string]any{"name": "my_app"}),
		i18n.T("backend.projects.runningMigrate", nil),
		i18n.T("backend.projects.skippingFrontendBuildNoNode", nil),
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("post-setup did not perform expected step %q: %v", want, lines)
		}
	}
	for _, unwanted := range []string{
		i18n.T("backend.projects.runningNpmInstall", map[string]any{"npm": filepath.Join(nodeDir, "npm.cmd")}),
		i18n.T("backend.projects.runningNpmBuild", nil),
	} {
		if slices.Contains(lines, unwanted) {
			t.Errorf("frontend was attempted while explicitly skipped: %q", unwanted)
		}
	}
}
