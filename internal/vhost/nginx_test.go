package vhost

import (
	"strings"
	"testing"
)

// A dev server started by Node 17+ often binds only to ::1 (IPv6 loopback)
// while nginx dialled 127.0.0.1, producing a 502. The proxy vhost must
// declare a dual-stack upstream so either binding works.
func TestRenderNginxVhostProxyUsesDualStackUpstream(t *testing.T) {
	out := string(RenderNginxVhost("cycloops-despesas", VhostSpec{
		ServerName: "cycloops-despesas.test",
		ProxyPort:  3000,
		SSL:        true,
		CertFile:   "C:/laraenv/nginx/certs/cycloops-despesas.test.crt",
		KeyFile:    "C:/laraenv/nginx/certs/cycloops-despesas.test.key",
	}))

	if strings.Count(out, "upstream cycloops_despesas_dev {") != 1 {
		t.Fatalf("expected exactly one upstream block, got:\n%s", out)
	}
	for _, want := range []string{
		"server 127.0.0.1:3000;",
		"server [::1]:3000 backup;",
		"listen 80;",
		"listen 443 ssl;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "proxy_pass http://cycloops_despesas_dev;"); got != 2 {
		t.Errorf("expected both listeners to proxy_pass the upstream, got %d in:\n%s", got, out)
	}
	if strings.Contains(out, "proxy_pass http://127.0.0.1") {
		t.Errorf("proxy_pass must not dial 127.0.0.1 directly:\n%s", out)
	}
	if !strings.Contains(out, `proxy_set_header Connection        "upgrade";`) {
		t.Errorf("websocket upgrade headers must be preserved:\n%s", out)
	}
}

// nginx splits directive arguments on whitespace, so an unquoted root, log,
// or certificate path breaks the moment the install root — or a portable
// install's folder name — contains a space, e.g. `D:\My Dev\LaraEnv`. This
// was confirmed live in a Windows Sandbox run: with a space in the install
// path, Caddy served the site correctly and nginx did not.
func TestRenderNginxVhostQuotesPathsContainingSpaces(t *testing.T) {
	logs := `D:\My Dev\LaraEnv\nginx\logs`
	conf := string(renderNginxVhost("shop", VhostSpec{
		ServerName:  "shop.test",
		DocRoot:     `D:\My Dev\LaraEnv\www\shop\public`,
		Kind:        "laravel",
		FastCGIPort: 9084,
		SSL:         true,
		CertFile:    `D:\My Dev\LaraEnv\nginx\certs\shop.crt`,
		KeyFile:     `D:\My Dev\LaraEnv\nginx\certs\shop.key`,
	}, logs))

	for _, want := range []string{
		`root "D:/My Dev/LaraEnv/www/shop/public";`,
		`access_log "D:/My Dev/LaraEnv/nginx/logs/shop.access.log";`,
		`error_log  "D:/My Dev/LaraEnv/nginx/logs/shop.error.log";`,
		`ssl_certificate     "D:/My Dev/LaraEnv/nginx/certs/shop.crt";`,
		`ssl_certificate_key "D:/My Dev/LaraEnv/nginx/certs/shop.key";`,
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
}

func TestProxyUpstreamNameIsSafeIdentifier(t *testing.T) {
	cases := map[string]string{
		"cycloops-despesas": "cycloops_despesas_dev",
		"my.app":            "my_app_dev",
		"Plain":             "plain_dev",
		"a b":               "a_b_dev",
	}
	for folder, want := range cases {
		if got := proxyUpstreamName(folder); got != want {
			t.Errorf("proxyUpstreamName(%q) = %q, want %q", folder, got, want)
		}
	}
}
