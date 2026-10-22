package deploy

import "strings"

// ApplyConfig is the user-supplied "what should this server look like" spec.
// Stored encrypted in deployment.config (JSON). The Apply runner translates
// it into a concrete command list combined with the detection result.
type ApplyConfig struct {
	// Project metadata.
	ProjectKind string `json:"project_kind"` // "laravel" | "wordpress" | "static" | "custom"
	Name        string `json:"name"`         // used for /var/www/<name> deploy dir + vhost filename

	// Domain + environment. The effective domain used in the vhost is:
	//   production  → Domain
	//   development → "dev." + Domain
	//   staging     → "staging." + Domain
	Domain      string `json:"domain"`
	Environment string `json:"environment"` // "production" | "development" | "staging"

	// Git source. HTTPS only for MVP (no SSH deploy keys yet).
	GitURL    string `json:"git_url"`
	GitBranch string `json:"git_branch"` // default: "main"

	// GitPAT is the user-supplied GitHub Personal Access Token. Optional.
	// When set, the runner splices it into GitURL via SpliceGitPAT before
	// `git clone` so private repos work without manual URL editing.
	// IMPORTANT: callers should clear this field on any persisted copy
	// (deployment.config saved on the server) — it has scope to read the
	// repo and shouldn't round-trip through storage.
	GitPAT string `json:"git_pat,omitempty"`

	// Stack choices. Defaulted from detection results in the form UI but
	// user can override (e.g. install nginx on a host that has apache).
	WebServer            string   `json:"web_server"`  // "nginx" | "apache"
	PHPVersion           string   `json:"php_version"` // "8.3" | "8.4" | "" (skip)
	Database             string   `json:"database"`    // "" | "mysql" | "postgres"
	DatabaseRemoteAccess bool     `json:"database_remote_access"`
	DatabaseAllowedIPs   []string `json:"database_allowed_ips"`

	// Optional post-deploy script. Runs from the project root after pull.
	// Typical Laravel: "composer install --no-dev -o && php artisan migrate --force && php artisan config:cache"
	// Legacy free-text mode: only used when PresetID and ScriptOverride are
	// both empty (see ResolveDeployScript). Kept verbatim for backward compat.
	BuildScript string `json:"build_script"`

	// Deploy pipeline (presets). PresetID references a built-in
	// ("builtin:<slug>") or custom (uuid) deploy preset; PresetVars carries
	// this deployment's values for the preset's declared variables;
	// ScriptOverride is a per-deployment edited copy of the preset body
	// (still containing {tokens}) that takes precedence over PresetID.
	PresetID       string            `json:"preset_id,omitempty"`
	PresetVars     map[string]string `json:"preset_vars,omitempty"`
	ScriptOverride string            `json:"script_override,omitempty"`

	// HTTPS — when true, Apply runs `certbot --nginx -d <domain>` after the
	// vhost is in place. The user's email goes to Let's Encrypt for renewal
	// reminders (required by the ACME spec).
	EnableSSL        bool   `json:"enable_ssl"`
	LetsEncryptEmail string `json:"lets_encrypt_email"`

	// Auto-deploy: install a crontab entry on the remote that pulls the git
	// branch and re-runs the build script every N minutes. Trade-off vs a
	// real webhook: simpler (no public endpoint needed) but a few minutes
	// of latency between git push and the site updating.
	AutoDeploy     bool   `json:"auto_deploy"`
	AutoDeployCron string `json:"auto_deploy_cron"` // 5-field expression; empty → "*/5 * * * *"
}

// EffectiveDomain returns the FQDN the vhost should listen on, factoring in
// the environment prefix. Production → no prefix.
func (c ApplyConfig) EffectiveDomain() string {
	switch c.Environment {
	case "development":
		return "dev." + c.Domain
	case "staging":
		return "staging." + c.Domain
	default:
		return c.Domain
	}
}

// NormalizeGitURL cleans common paste artefacts from a git URL the user typed
// into a form (or copy-pasted from a repo page):
//
//   - leading/trailing whitespace
//   - duplicated `https://` prefixes (paste-into-an-already-prefilled-input)
//   - trailing slashes (git tolerates them but they read ugly in logs)
//   - missing scheme (a bare github.com/owner/repo becomes https://...)
//
// SSH URLs (`git@github.com:...`) and `ssh://` URLs pass through untouched.
func NormalizeGitURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return u
	}
	for strings.HasPrefix(u, "https://https://") || strings.HasPrefix(u, "https://http://") {
		u = strings.TrimPrefix(u, "https://")
	}
	if !strings.HasPrefix(u, "https://") &&
		!strings.HasPrefix(u, "http://") &&
		!strings.HasPrefix(u, "git@") &&
		!strings.HasPrefix(u, "ssh://") {
		u = "https://" + u
	}
	u = strings.TrimRight(u, "/")
	return u
}

// SpliceGitPAT injects a Personal Access Token into an HTTPS git URL so
// `git clone` can authenticate without a credential helper. Returns the URL
// unchanged when:
//   - pat is empty,
//   - the URL is SSH (`git@...` / `ssh://...`),
//   - the URL already contains user info (caller pre-baked the PAT).
//
// Result format: `https://<pat>@github.com/owner/repo.git`
//
// Trade-off: the PAT ends up in `.git/config` on the remote, readable by
// the deploy user. Same threat model as the SSH passwords already kept
// in plaintext on the local config.json — explicit decision per the
// LaraEnv design doc. To rotate, re-run Apply with a new PAT.
func SpliceGitPAT(rawURL, pat string) string {
	if pat == "" || rawURL == "" {
		return rawURL
	}
	if !strings.HasPrefix(rawURL, "https://") {
		return rawURL
	}
	rest := strings.TrimPrefix(rawURL, "https://")
	if strings.Contains(strings.SplitN(rest, "/", 2)[0], "@") {
		return rawURL
	}
	return "https://" + pat + "@" + rest
}
