package vhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// baseHttpdConf approximates the httpd.conf shipped in the Apache archive
// before we've ever touched it: modules present but some commented out, no
// ServerName, and — this is the bug — no IncludeOptional for our per-project
// vhosts.
const baseHttpdConf = `ServerRoot "C:/laraenv/apache"
Listen 80
#LoadModule proxy_module modules/mod_proxy.so
#LoadModule proxy_fcgi_module modules/mod_proxy_fcgi.so
#LoadModule proxy_http_module modules/mod_proxy_http.so
#LoadModule proxy_wstunnel_module modules/mod_proxy_wstunnel.so
LoadModule rewrite_module modules/mod_rewrite.so
#LoadModule ssl_module modules/mod_ssl.so
#LoadModule socache_shmcb_module modules/mod_socache_shmcb.so
DocumentRoot "C:/laraenv/apache/htdocs"
`

// TestEnsureApacheSitesIncludedPatchesPristineHttpdConf reproduces the
// Apache side of the same bug as nginx: an httpd.conf fresh out of the
// archive has no IncludeOptional for conf/sites/*.conf, so a project vhost
// written there is never loaded until this patch runs.
func TestEnsureApacheSitesIncludedPatchesPristineHttpdConf(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "httpd.conf")
	if err := os.WriteFile(conf, []byte(baseHttpdConf), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureApacheSitesIncluded(conf); err != nil {
		t.Fatalf("EnsureApacheSitesIncluded: %v", err)
	}
	got, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"IncludeOptional conf/sites/*.conf",
		"LoadModule proxy_module modules/mod_proxy.so",
		"LoadModule proxy_fcgi_module modules/mod_proxy_fcgi.so",
		"LoadModule ssl_module modules/mod_ssl.so",
		"ServerName localhost",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q after patching a pristine httpd.conf:\n%s", want, s)
		}
	}
}

// TestEnsureApacheSitesIncludedIsIdempotent guards the startup/post-install
// call sites, which both run this on every launch/(re)install.
func TestEnsureApacheSitesIncludedIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "httpd.conf")
	if err := os.WriteFile(conf, []byte(baseHttpdConf), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureApacheSitesIncluded(conf); err != nil {
		t.Fatalf("EnsureApacheSitesIncluded (first): %v", err)
	}
	before, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}

	if err := EnsureApacheSitesIncluded(conf); err != nil {
		t.Fatalf("EnsureApacheSitesIncluded (second): %v", err)
	}
	after, err := os.ReadFile(conf)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Errorf("second call changed content (not idempotent):\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if n := strings.Count(string(after), "IncludeOptional conf/sites/*.conf"); n != 1 {
		t.Errorf("include line duplicated: %d occurrences", n)
	}
}
