package vhost

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"lara4ever/internal/config"
	"lara4ever/internal/i18n"
)

// NginxDefaults returns the values applied when a field in NginxTuning is
// zero. Centralised so the apply path and the UI hint text agree.
func NginxDefaults() config.NginxTuning {
	return config.NginxTuning{
		ServerNamesHashBucketSize: 128,
		ServerNamesHashMaxSize:    1024,
		// 512M so large media uploads aren't rejected at the nginx layer before
		// they even reach PHP. Must stay >= php post_max_size (see PhpTuning).
		ClientMaxBodyMB:       512,
		ClientBodyTimeoutSec:  300,
		FastCGIReadTimeoutSec: 300,
		ProxyReadTimeoutSec:   300,
		KeepaliveTimeoutSec:   75,
		WorkerConnections:     1024,
	}
}

// ApplyNginxTuning rewrites or injects every directive controlled by
// NginxTuning into nginx.conf. Idempotent and safe to call on every
// startup. Falls back to defaults for any zero-valued field.
func ApplyNginxTuning(nginxConfPath string, t config.NginxTuning) error {
	d := NginxDefaults()
	if t.ServerNamesHashBucketSize <= 0 {
		t.ServerNamesHashBucketSize = d.ServerNamesHashBucketSize
	}
	if t.ServerNamesHashMaxSize <= 0 {
		t.ServerNamesHashMaxSize = d.ServerNamesHashMaxSize
	}
	if t.ClientMaxBodyMB <= 0 {
		t.ClientMaxBodyMB = d.ClientMaxBodyMB
	}
	if t.ClientBodyTimeoutSec <= 0 {
		t.ClientBodyTimeoutSec = d.ClientBodyTimeoutSec
	}
	if t.FastCGIReadTimeoutSec <= 0 {
		t.FastCGIReadTimeoutSec = d.FastCGIReadTimeoutSec
	}
	if t.ProxyReadTimeoutSec <= 0 {
		t.ProxyReadTimeoutSec = d.ProxyReadTimeoutSec
	}
	if t.KeepaliveTimeoutSec <= 0 {
		t.KeepaliveTimeoutSec = d.KeepaliveTimeoutSec
	}
	if t.WorkerConnections <= 0 {
		t.WorkerConnections = d.WorkerConnections
	}

	b, err := os.ReadFile(nginxConfPath)
	if err != nil {
		return err
	}
	content := string(b)
	patched := content

	// http {} directives — written/replaced inline.
	httpDirs := []struct {
		key, val string
	}{
		{"server_names_hash_bucket_size", fmt.Sprintf("%d", t.ServerNamesHashBucketSize)},
		{"server_names_hash_max_size", fmt.Sprintf("%d", t.ServerNamesHashMaxSize)},
		{"client_max_body_size", fmt.Sprintf("%dM", t.ClientMaxBodyMB)},
		{"client_body_timeout", fmt.Sprintf("%ds", t.ClientBodyTimeoutSec)},
		{"fastcgi_read_timeout", fmt.Sprintf("%ds", t.FastCGIReadTimeoutSec)},
		{"proxy_read_timeout", fmt.Sprintf("%ds", t.ProxyReadTimeoutSec)},
		{"keepalive_timeout", fmt.Sprintf("%ds", t.KeepaliveTimeoutSec)},
	}
	for _, d := range httpDirs {
		patched, err = setOrInjectHttpDirectiveStr(patched, d.key, d.val)
		if err != nil {
			return err
		}
	}

	// events {} directive (worker_connections lives there, not in http {}).
	patched, err = setOrInjectEventsDirective(patched, "worker_connections", fmt.Sprintf("%d", t.WorkerConnections))
	if err != nil {
		return err
	}

	if patched == content {
		return nil
	}
	return os.WriteFile(nginxConfPath, []byte(patched), 0644)
}

// PhpUpstream describes one nginx upstream{} block: a named pool of php-cgi
// worker ports. A single-port pool behaves exactly like a direct
// `fastcgi_pass 127.0.0.1:<port>`, so Workers=1 is safe.
type PhpUpstream struct {
	Name  string // e.g. "php_8_1" (no dots — nginx upstream names forbid them)
	Ports []int  // e.g. [9081, 9181, 9281, 9381]
}

const (
	phpUpstreamBegin = "    # >>> lara4ever php upstreams (managed) — do not edit"
	phpUpstreamEnd   = "    # <<< lara4ever php upstreams"
)

// EnsurePhpUpstreams maintains a managed region of upstream{} blocks inside the
// http {} block of nginx.conf — one pool per PHP version in use. The whole
// region lives between sentinel comments and is rewritten wholesale on each
// call, so worker-count or version-set changes converge cleanly. Idempotent:
// writes back only when the rendered region differs. Pass an empty slice to
// collapse the region to just the sentinels.
func EnsurePhpUpstreams(nginxConfPath string, ups []PhpUpstream) error {
	b, err := os.ReadFile(nginxConfPath)
	if err != nil {
		return err
	}
	content := string(b)

	var sb strings.Builder
	sb.WriteString(phpUpstreamBegin + "\n")
	for _, u := range ups {
		if u.Name == "" || len(u.Ports) == 0 {
			continue
		}
		sb.WriteString("    upstream " + u.Name + " {\n")
		for _, p := range u.Ports {
			sb.WriteString(fmt.Sprintf("        server 127.0.0.1:%d;\n", p))
		}
		sb.WriteString("    }\n")
	}
	sb.WriteString(phpUpstreamEnd)
	block := sb.String()

	// Replace the existing managed region (sentinels included) wholesale, or
	// inject it just inside `http {` on first run.
	reRegion := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(phpUpstreamBegin) + `.*?` + regexp.QuoteMeta(phpUpstreamEnd))
	var patched string
	if reRegion.MatchString(content) {
		// Literal replace so any '$' in the block isn't treated as a capture ref.
		patched = reRegion.ReplaceAllLiteralString(content, block)
	} else {
		openIdx, err := findBlockOpen(content, "http")
		if err != nil {
			return err
		}
		patched = content[:openIdx+1] + "\n" + block + content[openIdx+1:]
	}
	if patched == content {
		return nil
	}
	return os.WriteFile(nginxConfPath, []byte(patched), 0644)
}

// EnsureSitesIncluded patches the main nginx.conf so the http {} block
// includes our per-project vhosts. Idempotent: returns nil if already done.
//
// The line injected is `include sites/*.conf;` (relative to nginx prefix —
// matches our convention of writing vhosts to <prefix>/conf/sites/).
func EnsureSitesIncluded(nginxConfPath string) error {
	b, err := os.ReadFile(nginxConfPath)
	if err != nil {
		return err
	}
	content := string(b)
	const includeLine = "    include sites/*.conf;"

	if strings.Contains(content, "include sites/*.conf") ||
		strings.Contains(content, "include conf/sites") {
		return nil // already patched
	}

	// Find the http { ... } block. Inject the include just before its closing
	// brace. We locate the first `http {` and then the *matching* closing `}`
	// by counting braces.
	httpIdx := strings.Index(content, "http {")
	if httpIdx < 0 {
		httpIdx = strings.Index(content, "http\t{")
	}
	if httpIdx < 0 {
		return i18n.Errorf("errors.vhost.httpBlockNotFound", map[string]any{"path": nginxConfPath})
	}

	// Walk from the opening brace to find the matching closing brace.
	openIdx := strings.Index(content[httpIdx:], "{")
	if openIdx < 0 {
		return i18n.Errorf("errors.vhost.noBraceAfterHttp", nil)
	}
	openIdx += httpIdx
	depth := 0
	closeIdx := -1
	for i := openIdx; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeIdx = i
				break
			}
		}
		if closeIdx > 0 {
			break
		}
	}
	if closeIdx < 0 {
		return i18n.Errorf("errors.vhost.unmatchedHttpBrace", nil)
	}

	// Insert the include line right before the closing }, preserving newline.
	patched := content[:closeIdx] + "\n" + includeLine + "\n" + content[closeIdx:]
	return os.WriteFile(nginxConfPath, []byte(patched), 0644)
}

// EnsureNginxHashSizes guarantees the http {} block declares both
// `server_names_hash_bucket_size` and `server_names_hash_max_size`.
// If the directive already exists it's rewritten in place; otherwise it's
// injected right after `http {`. Pass 0 to use defaults (bucket=128,
// max=1024). Without these, nginx fails with "could not build
// server_names_hash, you should increase server_names_hash_bucket_size: 32"
// once vhost names get long or numerous enough (multi-segment .test domains).
func EnsureNginxHashSizes(nginxConfPath string, bucket, max int) error {
	if bucket <= 0 {
		bucket = 128
	}
	if max <= 0 {
		max = 1024
	}
	b, err := os.ReadFile(nginxConfPath)
	if err != nil {
		return err
	}
	content := string(b)

	patched, err := setOrInjectHttpDirective(content, "server_names_hash_bucket_size", bucket)
	if err != nil {
		return err
	}
	patched, err = setOrInjectHttpDirective(patched, "server_names_hash_max_size", max)
	if err != nil {
		return err
	}
	if patched == content {
		return nil
	}
	return os.WriteFile(nginxConfPath, []byte(patched), 0644)
}

// setOrInjectHttpDirective rewrites `<key> N;` if present anywhere in the
// file, otherwise inserts it just after the opening `http {`. The directive
// is written with 4-space indentation to match our other inserts.
func setOrInjectHttpDirective(content, key string, val int) (string, error) {
	return setOrInjectHttpDirectiveStr(content, key, fmt.Sprintf("%d", val))
}

// setOrInjectHttpDirectiveStr is the string-valued cousin: handles values
// like "100M" or "300s" that nginx accepts for size/time directives.
// Match pattern accepts any non-`;` token after the key.
func setOrInjectHttpDirectiveStr(content, key, val string) (string, error) {
	line := fmt.Sprintf("    %s %s;", key, val)
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]+[^;]+;.*$`)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, line), nil
	}
	openIdx, err := findBlockOpen(content, "http")
	if err != nil {
		return content, err
	}
	return content[:openIdx+1] + "\n" + line + content[openIdx+1:], nil
}

// setOrInjectEventsDirective is the same idea for the `events {}` block,
// which is where worker_connections lives. nginx.conf always has an
// events {} block — bundled distro defaults include it — so we can
// safely require it.
func setOrInjectEventsDirective(content, key, val string) (string, error) {
	line := fmt.Sprintf("    %s %s;", key, val)
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]+[^;]+;.*$`)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, line), nil
	}
	openIdx, err := findBlockOpen(content, "events")
	if err != nil {
		return content, err
	}
	return content[:openIdx+1] + "\n" + line + content[openIdx+1:], nil
}

// findBlockOpen returns the index of the opening `{` of the named top-level
// block (http, events, …). Returns an error if the block isn't present.
func findBlockOpen(content, block string) (int, error) {
	idx := strings.Index(content, block+" {")
	if idx < 0 {
		idx = strings.Index(content, block+"\t{")
	}
	if idx < 0 {
		return 0, i18n.Errorf("errors.vhost.blockNotFound", map[string]any{"block": block})
	}
	openIdx := strings.Index(content[idx:], "{")
	if openIdx < 0 {
		return 0, i18n.Errorf("errors.vhost.noBraceAfterBlock", map[string]any{"block": block})
	}
	return idx + openIdx, nil
}

// EnsureDefaultServerName makes sure the bundled default server block doesn't
// hijack requests for our .test sites. We rename its server_name to
// "_default_" so it only matches when nothing else does.
// Optional safety net — failing is fine.
func EnsureDefaultServerName(nginxConfPath string) error {
	b, err := os.ReadFile(nginxConfPath)
	if err != nil {
		return err
	}
	content := string(b)
	re := regexp.MustCompile(`(?m)^\s*server_name\s+localhost;`)
	if !re.MatchString(content) {
		return nil
	}
	patched := re.ReplaceAllString(content, "    server_name  localhost _default_;")
	return os.WriteFile(nginxConfPath, []byte(patched), 0644)
}
