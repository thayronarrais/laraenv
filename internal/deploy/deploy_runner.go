package deploy

import (
	"fmt"
	"strings"

	xssh "golang.org/x/crypto/ssh"
)

// DetectionResult is the parsed output of a probe pass against a remote host.
// All fields are best-effort — anything we couldn't determine stays empty
// and the UI shows "n/a".
type DetectionResult struct {
	OSFamily  string            `json:"os_family"`  // "ubuntu" | "debian" | "centos" | "alpine" | ...
	OSVersion string            `json:"os_version"` // "22.04" | "11" | ...
	OSPretty  string            `json:"os_pretty"`  // raw PRETTY_NAME from /etc/os-release
	Kernel    string            `json:"kernel"`     // uname -r
	Arch      string            `json:"arch"`       // uname -m
	User      string            `json:"user"`       // whoami
	Hostname  string            `json:"hostname"`
	Installed map[string]string `json:"installed"` // binary name → version string (or "" if just present)
	Log       string            `json:"-"`         // combined log of all probe stdout/stderr
}

// probe is one command we run against the remote, plus how to interpret its
// output. Keep the set small — fast detection beats exhaustive detection.
type probe struct {
	cmd  string
	name string
}

// detectionProbes are run in order over a single SSH client. Most are read-
// only and harmless on any UNIX-y system. We tolerate non-zero exits — a
// missing binary returns "" from `which`, not an error we should surface.
var detectionProbes = []probe{
	{name: "uname-a", cmd: "uname -a"},
	{name: "uname-m", cmd: "uname -m"},
	{name: "uname-r", cmd: "uname -r"},
	{name: "hostname", cmd: "hostname"},
	{name: "whoami", cmd: "whoami"},
	{name: "os-release", cmd: "cat /etc/os-release 2>/dev/null || cat /etc/lsb-release 2>/dev/null || true"},

	// Binaries we care about for the install-or-skip step. `which` exits 1
	// when not found — captured into output as empty string.
	{name: "which-nginx", cmd: "which nginx 2>/dev/null || true"},
	{name: "which-apache", cmd: "which apache2 httpd 2>/dev/null || true"},
	{name: "which-php", cmd: "which php 2>/dev/null || true"},
	{name: "which-composer", cmd: "which composer 2>/dev/null || true"},
	{name: "which-node", cmd: "which node 2>/dev/null || true"},
	{name: "which-npm", cmd: "which npm 2>/dev/null || true"},
	{name: "which-mysql", cmd: "which mysql 2>/dev/null || which mariadb 2>/dev/null || true"},
	{name: "which-postgres", cmd: "which psql 2>/dev/null || true"},
	{name: "which-redis", cmd: "which redis-cli 2>/dev/null || true"},
	{name: "which-git", cmd: "which git 2>/dev/null || true"},

	// Versions, where the binary exists. Stays cheap; non-zero exit is fine.
	{name: "v-php", cmd: "php --version 2>/dev/null | head -n1 || true"},
	{name: "v-composer", cmd: "composer --version 2>/dev/null | head -n1 || true"},
	{name: "v-node", cmd: "node --version 2>/dev/null || true"},
	{name: "v-mysql", cmd: "mysql --version 2>/dev/null || true"},
	{name: "v-nginx", cmd: "nginx -v 2>&1 | head -n1 || true"},
	{name: "v-git", cmd: "git --version 2>/dev/null || true"},
}

// RunDetection opens a fresh SSH session per probe (cheap — they reuse the
// same client/connection) and parses the outputs into a DetectionResult.
// onLine fires once per probe with `> cmd` followed by the captured output;
// the caller (cloud manager / app.go) forwards it as a Wails event so the
// UI can render the live log.
//
// `client` MUST be open when this is called. RunDetection does NOT close it.
//
// i18n: the two lines below ("ERR open session: ...", "$ ...") are
// deliberately left as plain Go string building, not i18n.T. RunDetection is
// called from both app.go (catalogues loaded) and cmd/deploy-tui (no
// catalogue — see that package's own comment on why), so translating them
// would render bare key paths in deploy-tui's log. Also terse log markers
// ("ERR", "$ ") rather than authored sentences.
func RunDetection(client *xssh.Client, onLine func(string)) (*DetectionResult, error) {
	if onLine == nil {
		onLine = func(string) {}
	}
	out := &DetectionResult{Installed: map[string]string{}}
	var logBuf strings.Builder

	run := func(cmd string) string {
		sess, err := client.NewSession()
		if err != nil {
			line := fmt.Sprintf("ERR open session: %v", err)
			onLine(line)
			logBuf.WriteString(line + "\n")
			return ""
		}
		defer sess.Close()
		raw, _ := sess.CombinedOutput(cmd) // tolerate non-zero exits
		text := strings.TrimRight(string(raw), "\r\n ")
		header := fmt.Sprintf("$ %s", cmd)
		onLine(header)
		logBuf.WriteString(header + "\n")
		if text != "" {
			onLine(text)
			logBuf.WriteString(text + "\n")
		}
		return text
	}

	results := map[string]string{}
	for _, p := range detectionProbes {
		results[p.name] = run(p.cmd)
	}

	out.Kernel = results["uname-r"]
	out.Arch = results["uname-m"]
	out.Hostname = firstLine(results["hostname"])
	out.User = firstLine(results["whoami"])

	if osr := results["os-release"]; osr != "" {
		fields := parseOSRelease(osr)
		out.OSFamily = strings.ToLower(fields["ID"])
		out.OSVersion = fields["VERSION_ID"]
		out.OSPretty = fields["PRETTY_NAME"]
		if out.OSPretty == "" {
			out.OSPretty = strings.TrimSpace(fields["DISTRIB_DESCRIPTION"])
		}
	}

	// "which X" returns the path or empty string. Non-empty = present.
	mark := func(bin, whichKey, versionKey string) {
		path := firstLine(results[whichKey])
		if path == "" {
			return
		}
		ver := firstLine(results[versionKey])
		out.Installed[bin] = ver
	}
	mark("nginx", "which-nginx", "v-nginx")
	mark("apache", "which-apache", "")
	mark("php", "which-php", "v-php")
	mark("composer", "which-composer", "v-composer")
	mark("node", "which-node", "v-node")
	mark("npm", "which-npm", "")
	mark("mysql", "which-mysql", "v-mysql")
	mark("postgres", "which-postgres", "")
	mark("redis", "which-redis", "")
	mark("git", "which-git", "v-git")

	out.Log = logBuf.String()
	return out, nil
}

// parseOSRelease handles the simple shell-style KEY="value" / KEY=value
// format used by /etc/os-release on every modern Linux distro.
func parseOSRelease(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		v = strings.Trim(v, `"'`)
		m[k] = v
	}
	return m
}

func firstLine(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
