package vhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCaddyfileCreatesOneThatImportsTheSites(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "Caddyfile")

	if err := EnsureCaddyfile(conf, filepath.Join(dir, "sites")); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "import sites/*.caddy") {
		t.Fatalf("sites are not imported:\n%s", b)
	}
}

// The user may have written global options into it. Adding the import must
// not discard them.
func TestEnsureCaddyfileKeepsExistingContent(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(conf, []byte("{\n\tadmin off\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureCaddyfile(conf, filepath.Join(dir, "sites")); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(conf)
	if !strings.Contains(string(b), "admin off") {
		t.Errorf("hand-written options were lost:\n%s", b)
	}
	if !strings.Contains(string(b), "import sites/*.caddy") {
		t.Errorf("import was not added:\n%s", b)
	}
}

func TestEnsureCaddyfileIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "Caddyfile")

	for i := 0; i < 3; i++ {
		if err := EnsureCaddyfile(conf, filepath.Join(dir, "sites")); err != nil {
			t.Fatal(err)
		}
	}

	b, _ := os.ReadFile(conf)
	if got := strings.Count(string(b), "import sites/*.caddy"); got != 1 {
		t.Fatalf("import appears %d times:\n%s", got, b)
	}
}

// Caddy refuses to start when it cannot open its log file, which is exactly
// how nginx failed in the portable build. The directories have to exist.
func TestEnsureCaddyfileCreatesTheSitesDirectory(t *testing.T) {
	dir := t.TempDir()
	sites := filepath.Join(dir, "sites")

	if err := EnsureCaddyfile(filepath.Join(dir, "Caddyfile"), sites); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(sites); err != nil {
		t.Fatalf("sites dir not created: %v", err)
	}
}
