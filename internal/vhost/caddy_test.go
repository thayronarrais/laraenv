package vhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lara4ever/internal/paths"
)

func TestCaddyfileServesPHPThroughTheVersionsFastCGIPort(t *testing.T) {
	conf := string(RenderCaddyfile("shop", VhostSpec{
		ServerName:  "shop.test",
		DocRoot:     `C:\laraenv\www\shop\public`,
		Kind:        "laravel",
		FastCGIPort: 9084,
	}))

	for _, want := range []string{
		"shop.test {",
		`root * "C:/laraenv/www/shop/public"`,
		"php_fastcgi 127.0.0.1:9084",
		"file_server",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
}

// A plain project with no index.php is served as files only: routing every
// request through php_fastcgi would break a static site.
func TestCaddyfileServesAStaticProjectWithoutPHP(t *testing.T) {
	conf := string(RenderCaddyfile("docs", VhostSpec{
		ServerName: "docs.test",
		DocRoot:    t.TempDir(),
		Kind:       "plain",
	}))

	if strings.Contains(conf, "php_fastcgi") {
		t.Errorf("static project got a PHP handler:\n%s", conf)
	}
	if !strings.Contains(conf, "file_server") {
		t.Errorf("static project has no file server:\n%s", conf)
	}
}

// Caddy's reverse_proxy upgrades WebSocket connections on its own — the
// manual Upgrade/Connection headers the nginx block spells out are not
// needed, and HMR must work without them.
func TestCaddyfileProxiesADevServer(t *testing.T) {
	conf := string(RenderCaddyfile("app", VhostSpec{
		ServerName: "app.test",
		Kind:       "plain",
		ProxyPort:  5173,
	}))

	if !strings.Contains(conf, "reverse_proxy 127.0.0.1:5173") {
		t.Errorf("no reverse proxy in:\n%s", conf)
	}
	// Same reason the nginx renderer declares a dual-stack upstream: Node 17+
	// resolves localhost to ::1 first, so a Vite server often binds only the
	// IPv6 loopback. Without the second upstream it 502s under Caddy while
	// working under nginx.
	if !strings.Contains(conf, "reverse_proxy 127.0.0.1:5173 [::1]:5173") {
		t.Errorf("no IPv6 loopback fallback in:\n%s", conf)
	}
	if strings.Contains(conf, "root *") {
		t.Errorf("a proxied project must not also serve files:\n%s", conf)
	}
}

// The certificates are the ones nginx and Apache already render. tls internal
// would have Caddy run a second local CA and install a second root into the
// Windows store, so switching servers would change the chain the browser sees.
func TestCaddyfileReusesTheExistingCertificates(t *testing.T) {
	conf := string(RenderCaddyfile("shop", VhostSpec{
		ServerName:  "shop.test",
		DocRoot:     `C:\laraenv\www\shop\public`,
		Kind:        "laravel",
		FastCGIPort: 9084,
		SSL:         true,
		CertFile:    `C:\laraenv\nginx\certs\shop.crt`,
		KeyFile:     `C:\laraenv\nginx\certs\shop.key`,
	}))

	if !strings.Contains(conf, `tls "C:/laraenv/nginx/certs/shop.crt" "C:/laraenv/nginx/certs/shop.key"`) {
		t.Errorf("certificates not wired:\n%s", conf)
	}
	if strings.Contains(conf, "tls internal") {
		t.Errorf("tls internal would install a second root CA:\n%s", conf)
	}
}

func TestCaddyfileLogsUnderTheResolvedRoot(t *testing.T) {
	conf := string(RenderCaddyfile("shop", VhostSpec{
		ServerName: "shop.test", DocRoot: "X", Kind: "laravel", FastCGIPort: 9084,
	}))

	want := filepath.ToSlash(paths.CaddyLogs()) + "/shop.access.log"
	if !strings.Contains(conf, want) {
		t.Fatalf("no log at %q in:\n%s", want, conf)
	}
}

// The site address decides whether Caddy turns Automatic HTTPS on. A bare
// hostname that is not localhost/*.localhost/*.local — and .test is not
// exempt — gets an HTTP→HTTPS redirect on :80 plus an ACME order that can
// never succeed for a local TLD, so a non-SSL project (the default) would
// never load. The scheme must be explicit; with a certificate it must not be,
// or the site would never be served over 443.
func TestCaddyfileAddressCarriesASchemeOnlyWithoutTLS(t *testing.T) {
	base := VhostSpec{
		ServerName:  "shop.test",
		DocRoot:     `C:\laraenv\www\shop\public`,
		Kind:        "laravel",
		FastCGIPort: 9084,
	}

	plain := string(RenderCaddyfile("shop", base))
	if got := firstLine(plain); got != "http://shop.test {" {
		t.Errorf("non-SSL address line = %q, want %q\n%s", got, "http://shop.test {", plain)
	}

	secure := base
	secure.SSL = true
	secure.CertFile = `C:\laraenv\nginx\certs\shop.crt`
	secure.KeyFile = `C:\laraenv\nginx\certs\shop.key`
	tls := string(RenderCaddyfile("shop", secure))
	if got := firstLine(tls); got != "shop.test {" {
		t.Errorf("SSL address line = %q, want %q\n%s", got, "shop.test {", tls)
	}
}

// The Caddyfile tokenizer splits on whitespace and the main Caddyfile imports
// every project with `import sites/*.caddy`, so one unquoted path with a space
// is an adapter parse error that takes down every site. The logs directory is
// derived from the install root, which on a portable install is whatever
// folder the user picked.
func TestCaddyfileQuotesPathsContainingSpaces(t *testing.T) {
	logs := `D:\My Dev\LaraEnv\caddy\logs`
	conf := string(renderCaddyfile("shop", VhostSpec{
		ServerName:  "shop.test",
		DocRoot:     `D:\My Dev\LaraEnv\www\shop\public`,
		Kind:        "laravel",
		FastCGIPort: 9084,
		SSL:         true,
		CertFile:    `D:\My Dev\LaraEnv\nginx\certs\shop.crt`,
		KeyFile:     `D:\My Dev\LaraEnv\nginx\certs\shop.key`,
	}, logs))

	for _, want := range []string{
		`root * "D:/My Dev/LaraEnv/www/shop/public"`,
		`output file "D:/My Dev/LaraEnv/caddy/logs/shop.access.log"`,
		`tls "D:/My Dev/LaraEnv/nginx/certs/shop.crt" "D:/My Dev/LaraEnv/nginx/certs/shop.key"`,
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func TestCaddyRemoveVhostDeletesTheWrittenFile(t *testing.T) {
	c := NewCaddyConfigurator(t.TempDir())
	spec := VhostSpec{ServerName: "shop.test", DocRoot: "X", Kind: "laravel", FastCGIPort: 9084}

	if err := c.WriteVhostSpec("shop", spec); err != nil {
		t.Fatalf("WriteVhostSpec: %v", err)
	}
	if _, err := os.Stat(c.VhostPath("shop")); err != nil {
		t.Fatalf("vhost file missing before removal: %v", err)
	}

	if err := c.RemoveVhost("shop"); err != nil {
		t.Fatalf("RemoveVhost: %v", err)
	}
	if _, err := os.Stat(c.VhostPath("shop")); !os.IsNotExist(err) {
		t.Fatalf("vhost file still present after removal, stat err = %v", err)
	}
}

// removeVhostFor runs for every project regardless of whether Caddy was ever
// installed — Caddy vhosts are only written when the sites dir exists at
// apply time (see applyVhost). RemoveVhost must be a no-op, not an error, for
// a folder that was never written.
func TestCaddyRemoveVhostOnAnUnwrittenFolderReturnsNil(t *testing.T) {
	c := NewCaddyConfigurator(t.TempDir())

	if err := c.RemoveVhost("never-written"); err != nil {
		t.Fatalf("RemoveVhost on an unwritten folder: %v", err)
	}
}
