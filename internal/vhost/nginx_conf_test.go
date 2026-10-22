package vhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/i18n"
)

// TestMain loads the real catalogues before running this package's tests, so
// an *i18n.Error's message renders the actual English prose (with vars
// interpolated) instead of degrading to the bare key path a nil Global
// would produce — the same fallback a package hits before LoadCatalogues
// runs in production, but not what a test asserting on error text wants.
func TestMain(m *testing.M) {
	set, err := i18n.Load(os.DirFS(filepath.Join("..", "..", "langs")), ".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "load langs:", err)
		os.Exit(1)
	}
	i18n.SetGlobal(set)
	os.Exit(m.Run())
}

// TestEnsureSitesIncludedPatchesPristineNginxConf reproduces the bug: a
// nginx.conf as it ships in the archive (baseNginxConf, from
// upstream_test.go — same "http { ... server { listen 80 } }" shape as the
// real bundled default) has no `include sites/*.conf` at all. Installing
// nginx mid-session left this file untouched until EnsureSitesIncluded ran
// from startup on the *next* launch — this test locks in that a single call
// patches it immediately.
func TestEnsureSitesIncludedPatchesPristineNginxConf(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte(baseNginxConf), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSitesIncluded(conf); err != nil {
		t.Fatalf("EnsureSitesIncluded: %v", err)
	}
	got, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	if !strings.Contains(s, "include sites/*.conf;") {
		t.Errorf("include line missing after patching a pristine config:\n%s", s)
	}

	// The include must land inside the http {} block, not appended after it —
	// nginx ignores an `include` sitting outside http {} for our purposes,
	// and it's exactly this kind of "technically present but not loaded"
	// mistake that produced the original bug.
	httpIdx := strings.Index(s, "http {")
	incIdx := strings.Index(s, "include sites/*.conf;")
	closeIdx := strings.LastIndex(s, "}")
	if httpIdx < 0 || incIdx < httpIdx || incIdx > closeIdx {
		t.Errorf("include line landed outside the http {} block:\n%s", s)
	}
}

// TestEnsureSitesIncludedIsIdempotent guards the startup/post-install call
// sites, which both run this on every launch/(re)install: a second call
// against an already-patched file must not duplicate the include line or
// otherwise change the file.
func TestEnsureSitesIncludedIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "nginx.conf")
	if err := os.WriteFile(conf, []byte(baseNginxConf), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSitesIncluded(conf); err != nil {
		t.Fatalf("EnsureSitesIncluded (first): %v", err)
	}
	before, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}

	if err := EnsureSitesIncluded(conf); err != nil {
		t.Fatalf("EnsureSitesIncluded (second): %v", err)
	}
	after, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Errorf("second call changed content (not idempotent):\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if n := strings.Count(string(after), "include sites/*.conf;"); n != 1 {
		t.Errorf("include line duplicated: %d occurrences", n)
	}
}
