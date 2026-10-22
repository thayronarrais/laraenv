package vhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lara4ever/internal/paths"
)

// nginxLogsForward returns the nginx logs dir with forward slashes, the form
// nginx wants in a config file on Windows.
func nginxLogsForward() string {
	return filepath.ToSlash(paths.NginxRoot()) + "/logs"
}

// nginxPath renders a filesystem path as a single, whitespace-safe nginx
// config token: forward slashes wrapped in double quotes.
//
// nginx splits directive arguments on whitespace, so an unquoted root,
// log path, or certificate path breaks the moment the install root (or a
// portable install's folder name) contains a space, e.g. `D:\My Dev\LaraEnv`.
// nginx accepts double-quoted values for these directives, so quoting is the
// fix — mirroring how apache.go already wraps its DocumentRoot/Directory and
// certificate paths with %q.
func nginxPath(p string) string {
	return `"` + filepath.ToSlash(p) + `"`
}

type NginxConfigurator struct{ sitesDir string }

func NewNginxConfigurator(dir string) *NginxConfigurator {
	return &NginxConfigurator{sitesDir: dir}
}

// VhostSpec describes everything the template needs to render a server block.
type VhostSpec struct {
	ServerName  string // e.g. "myapp.test"
	DocRoot     string // absolute path; backslashes are normalised to forward slashes
	Kind        string // "laravel" | "wordpress" | "public" | "plain"
	LogPrefix   string // basename for log files (typically the project folder name)
	FastCGIPort int    // 0 → defaults to 9000
	// FastCGIUpstream, when set, makes the PHP location pass to a named nginx
	// upstream{} pool (e.g. "php_8_1") instead of a single 127.0.0.1:port — so
	// requests load-balance across the php-cgi worker pool. Falls back to the
	// direct port when empty.
	FastCGIUpstream string
	SSL             bool   // enable HTTPS listener
	CertFile        string // forward-slash path to the .crt (only when SSL=true)
	KeyFile         string // forward-slash path to the .key (only when SSL=true)
	ProxyPort       int    // >0 → reverse-proxy mode: forward to a dual-stack (127.0.0.1 / ::1) upstream on ProxyPort and ignore Kind/DocRoot/FastCGI
}

// proxyUpstreamName derives a safe nginx upstream identifier from the vhost
// folder name (e.g. "cycloops-despesas" → "cycloops_despesas_dev"). Site
// files are included at http{} level, so the name must be unique per site.
func proxyUpstreamName(folder string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(folder) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String() + "_dev"
}

// renderProxyUpstream declares a dual-stack upstream for a local dev server.
// Node 17+ resolves "localhost" to ::1 first, so Vite/Next/Nuxt frequently
// bind only the IPv6 loopback while older tools bind only 127.0.0.1. Listing
// both (IPv6 as backup) lets nginx fail over instead of returning 502.
func renderProxyUpstream(upstream string, port int) string {
	return fmt.Sprintf(`upstream %s {
    server 127.0.0.1:%d;
    server [::1]:%d backup;
}

`, upstream, port, port)
}

// renderProxyServer renders a server{} block that reverse-proxies all
// requests to the named dual-stack upstream (see renderProxyUpstream).
// Includes WebSocket upgrade headers so Vite/Next HMR works through the proxy.
func renderProxyServer(listen, ssl, name, upstream, log string) string {
	return fmt.Sprintf(`server {
    %s
%s    server_name %s;

    location / {
        proxy_pass http://%s;
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        "upgrade";
        proxy_read_timeout 86400;
    }

    access_log %s;
    error_log  %s;
}
`, listen, ssl, name, upstream, nginxPath(log+".access.log"), nginxPath(log+".error.log"))
}

// rewrite block per kind. WordPress wants ?args, Laravel/public/plain use $query_string.
func rewriteFor(kind string) string {
	switch kind {
	case "wordpress":
		return `location / {
        try_files $uri $uri/ /index.php?$args;
    }`
	default: // laravel, public, plain
		return `location / {
        try_files $uri $uri/ /index.php?$query_string;
    }`
	}
}

// renderPHPServer renders a single server{} block (HTTP or HTTPS) for a PHP app.
// fcgiTarget is either a named upstream ("php_8_1") or a direct "127.0.0.1:port".
func renderPHPServer(listen, ssl, name, root, rewrite, fcgiTarget, log string) string {
	return fmt.Sprintf(`server {
    %s
%s    server_name %s;
    root %s;
    index index.php index.html;

    %s

    location ~ \.php$ {
        fastcgi_pass %s;
        fastcgi_index index.php;
        fastcgi_param SCRIPT_FILENAME $document_root$fastcgi_script_name;
        include fastcgi_params;
    }

    access_log %s;
    error_log  %s;
}
`, listen, ssl, name, nginxPath(root), rewrite, fcgiTarget, nginxPath(log+".access.log"), nginxPath(log+".error.log"))
}

func renderStaticServer(listen, ssl, name, root, log string) string {
	return fmt.Sprintf(`server {
    %s
%s    server_name %s;
    root %s;
    index index.html index.htm;

    location / {
        try_files $uri $uri/ =404;
    }

    access_log %s;
    error_log  %s;
}
`, listen, ssl, name, nginxPath(root), nginxPath(log+".access.log"), nginxPath(log+".error.log"))
}

// WriteVhostSpec renders the nginx server block(s) for a project.
// folder is the file name (folder + ".conf").
func RenderNginxVhost(folder string, spec VhostSpec) []byte {
	return renderNginxVhost(folder, spec, nginxLogsForward())
}

// renderNginxVhost is RenderNginxVhost with the logs directory injected, so a
// test can render under a directory whose name contains a space — the install
// root is resolved once at process start and cannot be moved mid-test.
func renderNginxVhost(folder string, spec VhostSpec, logsDir string) []byte {
	if spec.LogPrefix == "" {
		spec.LogPrefix = folder
	}
	// Renderers receive the full log path, not just a name: nginx resolves a
	// relative one against its own prefix, and hardcoding an absolute root
	// here is what pinned every generated vhost to C:\laraenv.
	logBase := logsDir + "/" + spec.LogPrefix
	root := strings.ReplaceAll(spec.DocRoot, `\`, `/`)
	port := spec.FastCGIPort
	if port == 0 {
		port = 9000
	}
	// Prefer the named worker-pool upstream; fall back to the direct port.
	fcgiTarget := fmt.Sprintf("127.0.0.1:%d", port)
	if spec.FastCGIUpstream != "" {
		fcgiTarget = spec.FastCGIUpstream
	}

	isProxy := spec.ProxyPort > 0
	isStatic := !isProxy && spec.Kind == "plain" && !hasIndexPHP(spec.DocRoot)
	rewrite := rewriteFor(spec.Kind)

	upstream := proxyUpstreamName(folder)

	var conf strings.Builder
	switch {
	case isProxy:
		conf.WriteString(renderProxyUpstream(upstream, spec.ProxyPort))
		conf.WriteString(renderProxyServer("listen 80;", "", spec.ServerName, upstream, logBase))
	case isStatic:
		conf.WriteString(renderStaticServer("listen 80;", "", spec.ServerName, root, logBase))
	default:
		conf.WriteString(renderPHPServer("listen 80;", "", spec.ServerName, root, rewrite, fcgiTarget, logBase))
	}
	if spec.SSL && spec.CertFile != "" && spec.KeyFile != "" {
		sslBlock := fmt.Sprintf("    ssl_certificate     %s;\n    ssl_certificate_key %s;\n", nginxPath(spec.CertFile), nginxPath(spec.KeyFile))
		conf.WriteString("\n")
		switch {
		case isProxy:
			conf.WriteString(renderProxyServer("listen 443 ssl;", sslBlock, spec.ServerName, upstream, logBase))
		case isStatic:
			conf.WriteString(renderStaticServer("listen 443 ssl;", sslBlock, spec.ServerName, root, logBase))
		default:
			conf.WriteString(renderPHPServer("listen 443 ssl;", sslBlock, spec.ServerName, root, rewrite, fcgiTarget, logBase))
		}
	}

	return []byte(conf.String())
}

func (n *NginxConfigurator) VhostPath(folder string) string {
	return filepath.Join(n.sitesDir, folder+".conf")
}

func (n *NginxConfigurator) WriteVhostSpec(folder string, spec VhostSpec) error {
	if err := os.MkdirAll(n.sitesDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(n.VhostPath(folder), RenderNginxVhost(folder, spec), 0644)
}

// WriteVhost is the legacy entrypoint kept for backwards compat.
func (n *NginxConfigurator) WriteVhost(folder, host string) error {
	return n.WriteVhostSpec(folder, VhostSpec{
		ServerName: host,
		DocRoot:    filepath.ToSlash(paths.ProjectsRoot()) + "/" + folder + "/public",
		Kind:       "laravel",
		LogPrefix:  folder,
	})
}

func (n *NginxConfigurator) RemoveVhost(folder string) error {
	path := filepath.Join(n.sitesDir, folder+".conf")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func hasIndexPHP(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "index.php"))
	return err == nil
}
