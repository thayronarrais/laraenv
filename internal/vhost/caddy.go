package vhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lara4ever/internal/paths"
)

// CaddyConfigurator writes per-project Caddyfiles. Like the Nginx and Apache
// configurators it writes whether or not Caddy is the server currently
// running, so switching the active web server needs no regeneration.
type CaddyConfigurator struct{ sitesDir string }

func NewCaddyConfigurator(dir string) *CaddyConfigurator {
	return &CaddyConfigurator{sitesDir: dir}
}

func (c *CaddyConfigurator) VhostPath(folder string) string {
	return filepath.Join(c.sitesDir, folder+".caddy")
}

func (c *CaddyConfigurator) WriteVhostSpec(folder string, spec VhostSpec) error {
	if err := os.MkdirAll(c.sitesDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(c.VhostPath(folder), RenderCaddyfile(folder, spec), 0644)
}

func (c *CaddyConfigurator) RemoveVhost(folder string) error {
	if err := os.Remove(c.VhostPath(folder)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// caddyPath renders a filesystem path as a single Caddyfile token: forward
// slashes (the form a Caddyfile wants on Windows) wrapped in quotes.
//
// The quoting is not cosmetic. The Caddyfile tokenizer splits on whitespace,
// and the main Caddyfile pulls every project in with `import sites/*.caddy`,
// so a single unquoted path containing a space is an adapter parse error that
// stops Caddy loading *any* site — not just that one. The logs directory is
// derived from the install root and lands in every generated file, so on a
// portable install extracted to something like `D:\My Dev\LaraEnv` nothing
// would work at all.
func caddyPath(p string) string {
	return `"` + filepath.ToSlash(p) + `"`
}

// caddyLogFile is the access log token for one site, under the given Caddy
// logs directory.
func caddyLogFile(logsDir, prefix string) string {
	return caddyPath(filepath.Join(logsDir, prefix+".access.log"))
}

// RenderCaddyfile builds the site block for one project.
//
// The three shapes match the Nginx renderer: a reverse proxy for a running
// dev server, a file server for a static project, and PHP for everything
// else. TLS renders the certificate the spec carries — the same files Nginx
// and Apache use — never `tls internal`, which would stand up a second local
// CA and change the chain whenever the user switches servers.
//
// The site address carries an explicit scheme when there is no certificate.
// Caddy turns Automatic HTTPS on for any site address that is a bare hostname
// other than localhost/*.localhost/*.local — `.test` is not exempt — which
// would add an HTTP→HTTPS redirect on :80 and start an ACME order that can
// never succeed for a local TLD. Since SSL is off by default that is the
// common project, and it would simply never load. `http://` in the address
// scopes the site to :80 and disables Automatic HTTPS for it, without the
// global `auto_https off` that would also break the TLS variant below.
func RenderCaddyfile(folder string, spec VhostSpec) []byte {
	return renderCaddyfile(folder, spec, paths.CaddyLogs())
}

// renderCaddyfile is RenderCaddyfile with the logs directory injected, so a
// test can render under a directory whose name contains a space — the install
// root is resolved once at process start and cannot be moved mid-test.
func renderCaddyfile(folder string, spec VhostSpec, logsDir string) []byte {
	log := spec.LogPrefix
	if log == "" {
		log = folder
	}
	tls := spec.SSL && spec.CertFile != "" && spec.KeyFile != ""

	address := spec.ServerName
	if !tls {
		address = "http://" + address
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s {\n", address)

	if tls {
		fmt.Fprintf(&b, "    tls %s %s\n",
			caddyPath(spec.CertFile), caddyPath(spec.KeyFile))
	}

	switch {
	case spec.ProxyPort > 0:
		// reverse_proxy handles the WebSocket upgrade itself, so HMR works
		// without the header block the nginx template has to spell out.
		//
		// Both loopback families are listed, for the reason the nginx
		// renderer declares a dual-stack upstream: Node 17+ resolves
		// "localhost" to ::1 first, so Vite/Next/Nuxt frequently bind only
		// the IPv6 loopback while older tools bind only 127.0.0.1. Caddy
		// takes multiple upstreams and fails over instead of returning 502.
		fmt.Fprintf(&b, "    reverse_proxy 127.0.0.1:%d [::1]:%d\n",
			spec.ProxyPort, spec.ProxyPort)
	case spec.Kind == "plain" && !hasIndexPHP(spec.DocRoot):
		fmt.Fprintf(&b, "    root * %s\n", caddyPath(spec.DocRoot))
		b.WriteString("    file_server\n")
	default:
		port := spec.FastCGIPort
		if port == 0 {
			port = 9000
		}
		fmt.Fprintf(&b, "    root * %s\n", caddyPath(spec.DocRoot))
		fmt.Fprintf(&b, "    php_fastcgi 127.0.0.1:%d\n", port)
		b.WriteString("    file_server\n")
	}

	fmt.Fprintf(&b, "    log {\n        output file %s\n    }\n", caddyLogFile(logsDir, log))
	b.WriteString("}\n")
	return []byte(b.String())
}
