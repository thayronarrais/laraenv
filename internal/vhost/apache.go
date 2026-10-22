package vhost

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lara4ever/internal/paths"
)

// ApacheConfigurator writes per-project <VirtualHost> blocks for Apache
// httpd. Apache and Nginx cannot both bind to port 80 at once — the user
// is expected to enable only one webserver on the Dashboard. The vhost
// files are written either way so toggling between them doesn't lose
// configuration.
type ApacheConfigurator struct{ sitesDir string }

func NewApacheConfigurator(dir string) *ApacheConfigurator {
	return &ApacheConfigurator{sitesDir: dir}
}

// WriteVhostSpec renders the Apache VirtualHost(s) for spec to a file
// named <folder>.conf inside sitesDir. The shape mirrors the nginx
// configurator (same VhostSpec) so callers can target both with a single
// spec object.
func RenderApacheVhost(folder string, spec VhostSpec) []byte {
	if spec.LogPrefix == "" {
		spec.LogPrefix = folder
	}
	root := strings.ReplaceAll(spec.DocRoot, `\`, `/`)
	port := spec.FastCGIPort
	if port == 0 {
		port = 9000
	}

	isProxy := spec.ProxyPort > 0
	isStatic := !isProxy && spec.Kind == "plain" && !hasIndexPHP(spec.DocRoot)

	var conf strings.Builder
	switch {
	case isProxy:
		conf.WriteString(renderApacheProxyVHost("80", "", spec.ServerName, spec.ProxyPort, spec.LogPrefix))
	case isStatic:
		conf.WriteString(renderApacheStaticVHost("80", "", spec.ServerName, root, spec.LogPrefix))
	default:
		conf.WriteString(renderApachePHPVHost("80", "", spec.ServerName, root, spec.Kind, port, spec.LogPrefix))
	}

	if spec.SSL && spec.CertFile != "" && spec.KeyFile != "" {
		sslBlock := fmt.Sprintf(
			"    SSLEngine on\n    SSLCertificateFile %q\n    SSLCertificateKeyFile %q\n",
			spec.CertFile, spec.KeyFile,
		)
		conf.WriteString("\n")
		switch {
		case isProxy:
			conf.WriteString(renderApacheProxyVHost("443", sslBlock, spec.ServerName, spec.ProxyPort, spec.LogPrefix))
		case isStatic:
			conf.WriteString(renderApacheStaticVHost("443", sslBlock, spec.ServerName, root, spec.LogPrefix))
		default:
			conf.WriteString(renderApachePHPVHost("443", sslBlock, spec.ServerName, root, spec.Kind, port, spec.LogPrefix))
		}
	}

	return []byte(conf.String())
}

func (a *ApacheConfigurator) VhostPath(folder string) string {
	return filepath.Join(a.sitesDir, folder+".conf")
}

func (a *ApacheConfigurator) WriteVhostSpec(folder string, spec VhostSpec) error {
	if err := os.MkdirAll(a.sitesDir, 0755); err != nil {
		return err
	}
	return os.WriteFile(a.VhostPath(folder), RenderApacheVhost(folder, spec), 0644)
}

func (a *ApacheConfigurator) RemoveVhost(folder string) error {
	path := filepath.Join(a.sitesDir, folder+".conf")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// renderApachePHPVHost produces a VirtualHost that hands .php requests to
// php-fpm via mod_proxy_fcgi. Laravel/WordPress rewrite rules use
// FallbackResource which is the canonical Apache way to do
// "try_files $uri $uri/ /index.php" without RewriteEngine pyrotechnics.
func renderApachePHPVHost(port, sslBlock, name, root, kind string, fcgiPort int, log string) string {
	fallback := "/index.php"
	return fmt.Sprintf(`<VirtualHost *:%s>
    ServerName %s
    DocumentRoot %q
%s
    <Directory %q>
        AllowOverride All
        Require all granted
        DirectoryIndex index.php index.html
        FallbackResource %s
    </Directory>

    <FilesMatch \.php$>
        SetHandler "proxy:fcgi://127.0.0.1:%d"
    </FilesMatch>

    ErrorLog "%s/%s.error.log"
    CustomLog "%s/%s.access.log" common
</VirtualHost>
`, port, name, root, sslBlock, root, fallback, fcgiPort,
		apacheLogsForward(), log, apacheLogsForward(), log)
}

func renderApacheStaticVHost(port, sslBlock, name, root, log string) string {
	return fmt.Sprintf(`<VirtualHost *:%s>
    ServerName %s
    DocumentRoot %q
%s
    <Directory %q>
        AllowOverride All
        Require all granted
        DirectoryIndex index.html index.htm
    </Directory>

    ErrorLog "%s/%s.error.log"
    CustomLog "%s/%s.access.log" common
</VirtualHost>
`, port, name, root, sslBlock, root,
		apacheLogsForward(), log, apacheLogsForward(), log)
}

// renderApacheProxyVHost reverse-proxies to a local dev server. WebSocket
// upgrade is handled via mod_rewrite — the standard idiom for Apache HMR.
// The target is "localhost" rather than 127.0.0.1 on purpose: mod_proxy
// tries every address the name resolves to, so a dev server bound only to
// ::1 (Node 17+ default) or only to 127.0.0.1 both work.
func renderApacheProxyVHost(port, sslBlock, name string, target int, log string) string {
	return fmt.Sprintf(`<VirtualHost *:%s>
    ServerName %s
%s
    ProxyPreserveHost On
    ProxyRequests Off

    RewriteEngine On
    RewriteCond %%{HTTP:Upgrade} =websocket [NC]
    RewriteRule /(.*) ws://localhost:%d/$1 [P,L]

    ProxyPass        / http://localhost:%d/
    ProxyPassReverse / http://localhost:%d/

    ErrorLog "%s/%s.error.log"
    CustomLog "%s/%s.access.log" common
</VirtualHost>
`, port, name, sslBlock, target, target, target,
		apacheLogsForward(), log, apacheLogsForward(), log)
}

// apacheLogsForward returns the Apache logs dir using forward slashes,
// the form Apache prefers in config files on Windows.
func apacheLogsForward() string {
	// The cycle this used to fear does not exist: internal/paths imports
	// nothing but os and path/filepath.
	return filepath.ToSlash(paths.ApacheLogs())
}
