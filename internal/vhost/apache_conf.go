package vhost

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"lara4ever/internal/config"
)

// ApacheDefaults returns the values applied when a field in ApacheTuning
// is zero. Mirrors NginxDefaults so the UI hint text and the apply path
// agree.
func ApacheDefaults() config.ApacheTuning {
	return config.ApacheTuning{
		TimeoutSec:          300,
		KeepAliveTimeoutSec: 5,
		LimitRequestBodyMB:  100,
		ProxyTimeoutSec:     300,
	}
}

// ApplyApacheTuning rewrites or injects the global directives controlled
// by ApacheTuning into httpd.conf. Idempotent. Falls back to defaults
// when a field is zero. No-op if Apache is not installed.
func ApplyApacheTuning(httpdConfPath string, t config.ApacheTuning) error {
	d := ApacheDefaults()
	if t.TimeoutSec <= 0 {
		t.TimeoutSec = d.TimeoutSec
	}
	if t.KeepAliveTimeoutSec <= 0 {
		t.KeepAliveTimeoutSec = d.KeepAliveTimeoutSec
	}
	if t.LimitRequestBodyMB < 0 {
		t.LimitRequestBodyMB = d.LimitRequestBodyMB
	}
	if t.ProxyTimeoutSec <= 0 {
		t.ProxyTimeoutSec = d.ProxyTimeoutSec
	}

	b, err := os.ReadFile(httpdConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	conf := string(b)
	original := conf

	// LimitRequestBody is in bytes (0 = unlimited). Convert from MB.
	limitBytes := int64(t.LimitRequestBodyMB) * 1024 * 1024

	dirs := []struct{ key, val string }{
		{"Timeout", fmt.Sprintf("%d", t.TimeoutSec)},
		{"KeepAliveTimeout", fmt.Sprintf("%d", t.KeepAliveTimeoutSec)},
		{"LimitRequestBody", fmt.Sprintf("%d", limitBytes)},
		{"ProxyTimeout", fmt.Sprintf("%d", t.ProxyTimeoutSec)},
	}
	for _, d := range dirs {
		conf = setOrInjectApacheDirective(conf, d.key, d.val)
	}

	if conf == original {
		return nil
	}
	return os.WriteFile(httpdConfPath, []byte(conf), 0644)
}

// setOrInjectApacheDirective rewrites `<Key> <val>` if the directive is
// already present at the top level (line starts with optional whitespace
// then the key). Otherwise appends it inside our laraenv-managed block at
// end of file. Apache global directives are case-insensitive but we keep
// the canonical capitalisation when injecting.
func setOrInjectApacheDirective(content, key, val string) string {
	re := regexp.MustCompile(`(?im)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]+\S.*$`)
	line := fmt.Sprintf("%s %s", key, val)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, line)
	}
	const marker = "# laraenv: tuning"
	if strings.Contains(content, marker) {
		// Append inside the marker block: find the marker and inject right after.
		idx := strings.Index(content, marker)
		insertAt := idx + len(marker)
		return content[:insertAt] + "\n" + line + content[insertAt:]
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + "\n" + marker + "\n" + line + "\n"
}

// EnsureApacheSitesIncluded patches httpd.conf so it loads our per-project
// vhosts from conf/sites/*.conf and has the modules our templates rely on
// (proxy, proxy_fcgi, rewrite, ssl). Idempotent — safe to run on every
// startup. Returns nil if the file doesn't exist (Apache not installed).
func EnsureApacheSitesIncluded(httpdConfPath string) error {
	b, err := os.ReadFile(httpdConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	conf := string(b)
	original := conf

	// Modules we need. Apache distros ship them but usually keep some
	// commented out (LoadModule lines starting with "#"). Uncomment any
	// that are commented, leave already-active ones alone.
	mods := []string{
		"proxy_module modules/mod_proxy.so",
		"proxy_fcgi_module modules/mod_proxy_fcgi.so",
		"proxy_http_module modules/mod_proxy_http.so",
		"proxy_wstunnel_module modules/mod_proxy_wstunnel.so",
		"rewrite_module modules/mod_rewrite.so",
		"ssl_module modules/mod_ssl.so",
		"socache_shmcb_module modules/mod_socache_shmcb.so",
	}
	for _, m := range mods {
		// Match "#LoadModule <name>" possibly with extra whitespace.
		re := regexp.MustCompile(`(?m)^\s*#\s*LoadModule\s+` + regexp.QuoteMeta(m) + `\s*$`)
		conf = re.ReplaceAllString(conf, "LoadModule "+m)
	}

	// Include for our per-project vhosts. Use an unambiguous marker so we
	// only add it once, and place it at the end of file.
	const marker = "# laraenv: per-project vhosts"
	if !strings.Contains(conf, marker) {
		if !strings.HasSuffix(conf, "\n") {
			conf += "\n"
		}
		conf += fmt.Sprintf("\n%s\nIncludeOptional conf/sites/*.conf\n", marker)
	}

	// ServerName directive — Apache complains on startup without one.
	if !regexp.MustCompile(`(?m)^\s*ServerName\s`).MatchString(conf) {
		conf += "\nServerName localhost\n"
	}

	if conf == original {
		return nil
	}
	return os.WriteFile(httpdConfPath, []byte(conf), 0644)
}
