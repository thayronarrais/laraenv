package deploy

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Step is one bash invocation we run on the remote during Apply. Most are
// idempotent — re-running the same plan against an already-configured host
// is a no-op (apt install of present packages, git pull on existing clone,
// vhost overwrite of the same content).
type Step struct {
	Title string // human label shown in the log header
	// TitleKey/TitleVars name a catalogue key (under deployments.steps.* in
	// langs/*.json) the desktop GUI renders instead of Title, plus its
	// interpolation vars. Title itself stays plain English always: it is
	// also what cmd/deploy-tui prints directly (that binary has no loaded
	// catalogue — see its own comment on why), so Title must never change
	// meaning based on TitleKey. The GUI (Deployments.tsx) falls back to
	// Title when TitleKey is empty.
	TitleKey  string
	TitleVars map[string]any
	Cmd       string // raw bash command (joined with sudo if NeedsSudo)
	Sudo      bool   // prepend `sudo -n` and bail on a friendly message if denied
	Ignore    bool   // treat non-zero exit as warning, not error (e.g. service reload)
}

// BuildPlan returns the ordered list of steps to apply `cfg` against the
// remote described by `det`. Caller is responsible for actually executing
// them (see ApplyRunner). Pure function — easy to test without SSH.
//
// deployScript is the deploy pipeline body to embed in the build step and
// the autopull cron file. legacy=true means it's the old free-text
// BuildScript (kept byte-identical to the pre-preset behavior); otherwise
// it's an already-rendered preset pipeline that gets the shared
// BuildDeployScript wrapper. Callers resolve+render via RenderDeployPipeline.
func BuildPlan(cfg ApplyConfig, det *DetectionResult, deployScript string, legacy bool) []Step {
	if err := cfg.NormalizeDatabaseAccess(); err != nil {
		return []Step{{Title: "Validate database access", Cmd: "echo " + shQuote(err.Error()) + " >&2; exit 1"}}
	}
	var steps []Step

	deployRoot := "/var/www/" + safeName(cfg.Name)
	domain := cfg.EffectiveDomain()

	// 0. Refresh package index. Skip when nothing needs installing — saves
	// 1-2s on re-runs against an already-configured host.
	// When det is nil (deploy-tui clean-VPS path) we don't know the OS yet
	// but assume apt-based.
	apt := det == nil || det.OSFamily == "ubuntu" || det.OSFamily == "debian"
	if apt && (det == nil || len(missingPackages(cfg, det)) > 0) {
		steps = append(steps, Step{
			Title:    "apt update",
			TitleKey: "deployments.steps.aptUpdate",
			// Idempotent — apt update is always cheap when the list is fresh.
			Cmd:  "apt-get update -qq",
			Sudo: true,
		})
	}

	// 0b. PHP repo — Ubuntu's default repos lag (jammy ships 8.1, noble ships 8.3
	// and never gets 8.4). Use sury.org for both Ubuntu and Debian — same
	// maintainer (Ondřej Surý) as the official Ubuntu PPA but cached on a CDN,
	// without the Python `add-apt-repository` tool that depends on Launchpad's
	// keyserver (which has been returning 504s during outages).
	// Idempotent — re-runs just overwrite the same .list file and re-fetch
	// the (versioned) gpg key.
	needsPHP := cfg.PHPVersion != "" || cfg.ProjectKind == "laravel"
	if needsPHP && (det == nil || det.OSFamily == "ubuntu" || det.OSFamily == "debian") {
		steps = append(steps, Step{
			Title:    "Add sury.org PHP repository (for modern PHP versions)",
			TitleKey: "deployments.steps.addSuryRepo",
			// Guard: if the sury.list file already exists, this whole block
			// is a no-op. Saves ~60s on subsequent runs against the same VPS.
			Cmd: `if [ -f /etc/apt/sources.list.d/php-sury.list ]; then ` +
				`  echo "(sury.org repo already configured — skipping)"; ` +
				`else ` +
				`  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates lsb-release gnupg && ` +
				`  curl -fsSL --retry 5 --retry-delay 3 https://packages.sury.org/php/apt.gpg ` +
				`    -o /etc/apt/trusted.gpg.d/php-sury.gpg && ` +
				`  codename="$(lsb_release -sc)" && ` +
				`  echo "deb https://packages.sury.org/php/ ${codename} main" ` +
				`    > /etc/apt/sources.list.d/php-sury.list && ` +
				`  apt-get update -qq; ` +
				`fi`,
			Sudo: true,
		})
	}

	// 1. Install missing packages. Run `apt-get update` immediately before
	// install: when the sury.org repo step was skipped (already configured)
	// the apt index can be stale relative to packages we now want
	// (php8.4-* might exist in the repo but our cached metadata predates
	// it). Always-fresh update adds 2s and avoids "no installation
	// candidate" mysteries.
	missing := missingPackages(cfg, det)
	if len(missing) > 0 {
		steps = append(steps, Step{
			Title:     "Install missing packages: " + strings.Join(missing, ", "),
			TitleKey:  "deployments.steps.installMissingPackages",
			TitleVars: map[string]any{"packages": strings.Join(missing, ", ")},
			Cmd: "apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq " +
				strings.Join(missing, " "),
			Sudo: true,
		})
	}

	// 1b. Verify the requested PHP binary exists. We don't change the global
	// `php` symlink (some hosts run multiple sites with different PHP
	// versions — flipping update-alternatives breaks the others). Instead,
	// the build step + auto-pull script prepend a per-deployment .laraenv-bin
	// directory to PATH so `php` and `composer` resolve to the right version
	// for THIS site only. nginx's fastcgi_pass is already version-specific
	// via the php<ver>-fpm.sock socket path.
	if needsPHP {
		ver := cfg.PHPVersion
		if ver == "" {
			ver = "8.3"
		}
		steps = append(steps, Step{
			Title:     "Verify php" + ver + " is installed",
			TitleKey:  "deployments.steps.verifyPhpInstalled",
			TitleVars: map[string]any{"version": ver},
			Cmd: fmt.Sprintf(
				`if [ -x /usr/bin/php%[1]s ]; then `+
					`  /usr/bin/php%[1]s --version; `+
					`else `+
					`  echo "ERROR: php%[1]s not installed — sury.org may not have it for this codename" >&2; exit 1; `+
					`fi`,
				ver,
			),
		})
	}

	// 2. Create deploy root.
	steps = append(steps, Step{
		Title:     "Create deploy dir " + deployRoot,
		TitleKey:  "deployments.steps.createDeployDir",
		TitleVars: map[string]any{"path": deployRoot},
		Cmd:       fmt.Sprintf("mkdir -p %s && chown -R $USER:$USER %s", shQuote(deployRoot), shQuote(deployRoot)),
		Sudo:      true,
	})

	// 3. Always wipe + clone fresh. Deploy-tui (and Apply) are bootstrap
	// operations — ongoing updates live in the auto-pull cron, not here.
	// Wiping makes the step deterministic regardless of partial state from
	// previous failed runs (we hit cases where stale .git left an empty
	// working tree that `git reset --hard` couldn't repair).
	//
	// Runtime data survives the wipe: .env (hand-edited creds/API keys) and
	// storage/ (user uploads, logs) are moved into a keep dir on the SAME
	// filesystem before the rm -rf and restored over the fresh clone. Same
	// filesystem matters: mv is instant and storage/ can hold gigabytes —
	// /tmp is often tmpfs (RAM) and copying there could OOM the host. The
	// keep dir is only cleaned AFTER a successful restore, so a clone that
	// fails midway doesn't lose the data — the next apply picks it up.
	if cfg.GitURL != "" {
		branch := cfg.GitBranch
		if branch == "" {
			branch = "main"
		}
		keepDir := "/var/www/.laraenv-keep-" + safeName(cfg.Name)
		steps = append(steps, Step{
			Title:    "Clone repo",
			TitleKey: "deployments.steps.cloneRepo",
			Cmd: fmt.Sprintf(
				`sudo mkdir -p %[4]s && `+
					`if sudo test -f %[1]s/.env; then sudo cp -a %[1]s/.env %[4]s/.env && echo "(preserving .env)"; fi && `+
					`if sudo test -d %[1]s/storage; then sudo rm -rf %[4]s/storage && sudo mv %[1]s/storage %[4]s/storage && echo "(preserving storage/)"; fi && `+
					`sudo rm -rf %[1]s && git clone --branch %[2]s %[3]s %[1]s && `+
					`if sudo test -f %[4]s/.env; then sudo mv %[4]s/.env %[1]s/.env && echo "(restored .env from previous deploy)"; fi && `+
					`if sudo test -d %[4]s/storage; then sudo rm -rf %[1]s/storage && sudo mv %[4]s/storage %[1]s/storage && echo "(restored storage/ from previous deploy)"; fi && `+
					`sudo rm -rf %[4]s && `+
					`echo "--- repo contents ---" && ls -la %[1]s | head -20`,
				shQuote(deployRoot), shQuote(branch), shQuote(cfg.GitURL), shQuote(keepDir),
			),
		})
	}

	// 3b. MySQL bootstrap — when the user picked database=mysql and this is
	// a Laravel project, create the DB + a dedicated user with a random
	// password, then patch the cloned .env with those credentials.
	// Idempotent — CREATE IF NOT EXISTS for the schema, ALTER USER for the
	// password (rotates each apply, by design — passwords stay on the
	// remote .env, never round-trip to the client). Skipped when .env
	// already has APP_KEY set, so re-runs don't reset DB credentials.
	if cfg.Database == "mysql" && cfg.ProjectKind == "laravel" {
		// MySQL identifiers don't accept hyphens without backticks. To dodge
		// that entire class of bug we underscore them out for the DB name
		// + MySQL user. Deploy dir / vhost filename keep their hyphens.
		dbSafeName := strings.ReplaceAll(safeName(cfg.Name), "-", "_")
		dbName := dbSafeName + "_db"
		dbUser := dbSafeName
		if len(dbUser) > 32 {
			dbUser = dbUser[:32]
		}
		// Self-verifying bootstrap script:
		//   1. If .env's DB_PASSWORD already authenticates with MySQL
		//      → skip (idempotent fast path).
		//   2. Otherwise: DROP USER + CREATE USER with fresh password,
		//      verify it authenticates BEFORE patching .env.
		//      If verification fails, exit non-zero so the step shows red
		//      instead of producing a broken .env / DB combo.
		// We strip hyphens out of dbName/dbUser above, so identifiers are pure
		// [a-zA-Z0-9_]. MySQL accepts those unquoted — no backticks needed,
		// which sidesteps a nasty class of bug where bash's heredoc body
		// (unquoted delimiter) interprets `name` as command substitution.
		// Raw template + manual placeholder substitution.
		template := `set -e
cd ` + shQuote(deployRoot) + `
[ -f .env ] || cp .env.example .env
DB_NAME='__DBNAME__'
DB_USER='__DBUSER__'

EXISTING_PASS=$(grep '^DB_PASSWORD=' .env | cut -d= -f2-)
if [ -n "$EXISTING_PASS" ] && mysql -u "$DB_USER" -p"$EXISTING_PASS" -e "USE __DBNAME__; SELECT 1;" >/dev/null 2>&1; then
    echo "(.env credentials authenticate — skipping bootstrap)"
    mysql -e "CREATE DATABASE IF NOT EXISTS __DBNAME__ CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"
    exit 0
fi

echo "Setting up MySQL DB + user from scratch…"
DB_PASS=$(openssl rand -hex 24)
mysql <<SQL_EOF
CREATE DATABASE IF NOT EXISTS __DBNAME__ CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
DROP USER IF EXISTS '$DB_USER'@'localhost';
CREATE USER '$DB_USER'@'localhost' IDENTIFIED BY '$DB_PASS';
GRANT ALL ON __DBNAME__.* TO '$DB_USER'@'localhost';
FLUSH PRIVILEGES;
SQL_EOF

if ! mysql -u "$DB_USER" -p"$DB_PASS" -e "USE __DBNAME__; SELECT 1;" >/dev/null 2>&1; then
    echo "ERROR: created MySQL user but can't authenticate with new password" >&2
    exit 1
fi

# Set or replace a key=value pair in .env. Handles three cases:
#   1. Active line "KEY=..."         → replace value.
#   2. Commented line "# KEY=..."    → uncomment + replace value.
#   3. Missing entirely              → append.
# Required because Laravel 11+ ships .env.example with DB_CONNECTION=sqlite
# and the rest of the DB_* lines commented out — a plain sed-replace only
# patches DB_CONNECTION and leaves Laravel falling back to its hardcoded
# defaults (root@localhost, database 'laravel'), which fails migration.
set_env_var() {
    local key="$1" val="$2"
    # Use a sentinel char unlikely in values for the sed delimiter. Hex pass +
    # plain identifiers don't contain '|', so it's safe here.
    if grep -qE "^${key}=" .env; then
        sed -i "s|^${key}=.*|${key}=${val}|" .env
    elif grep -qE "^[[:space:]]*#[[:space:]]*${key}=" .env; then
        sed -i "s|^[[:space:]]*#[[:space:]]*${key}=.*|${key}=${val}|" .env
    else
        printf '%s=%s\n' "$key" "$val" >> .env
    fi
}
set_env_var DB_CONNECTION mysql
set_env_var DB_HOST 127.0.0.1
set_env_var DB_PORT 3306
set_env_var DB_DATABASE "$DB_NAME"
set_env_var DB_USERNAME "$DB_USER"
set_env_var DB_PASSWORD "$DB_PASS"
echo "DB ready: $DB_NAME / user $DB_USER"
`
		script := strings.NewReplacer(
			"__DBNAME__", dbName,
			"__DBUSER__", dbUser,
		).Replace(template)
		steps = append(steps, Step{
			Title:     "Bootstrap MySQL DB + user for " + cfg.Name,
			TitleKey:  "deployments.steps.bootstrapMysqlDb",
			TitleVars: map[string]any{"name": cfg.Name},
			Cmd:       script,
			Sudo:      true,
		})
	}

	// 3c. Ensure .env exists for Laravel projects. The MySQL bootstrap step
	// above already does this, but it only runs for database=mysql — without
	// this, build scripts that call `php artisan key:generate` (a near-
	// universal post-clone command) fail with "file_get_contents(.env):
	// Failed to open stream" on projects with no DB or non-MySQL DBs.
	// Idempotent: skips if .env already present, errors only if neither
	// .env nor .env.example exists.
	if cfg.ProjectKind == "laravel" {
		steps = append(steps, Step{
			Title:    "Ensure .env exists",
			TitleKey: "deployments.steps.ensureEnvExists",
			Cmd: fmt.Sprintf(
				`cd %[1]s && `+
					`if [ -f .env ]; then echo "(.env already present)"; `+
					`elif [ -f .env.example ]; then cp .env.example .env && echo "Created .env from .env.example"; `+
					`else echo "ERROR: neither .env nor .env.example found in repo" >&2; exit 1; fi`,
				shQuote(deployRoot),
			),
		})
	}

	steps = append(steps, databaseAccessCleanupSteps(cfg.Name, cfg.Database)...)
	if cfg.ProjectKind == "laravel" && cfg.Database == "postgres" && cfg.DatabaseRemoteAccess {
		steps = append(steps, Step{
			Title:    "Bootstrap PostgreSQL DB + user for " + cfg.Name,
			TitleKey: "deployments.steps.bootstrapPostgresDb", TitleVars: map[string]any{"name": cfg.Name},
			Cmd: databaseScript(cfg, databaseBootstrapPostgres), Sudo: true,
		})
	}
	if cfg.Database == "mysql" || cfg.Database == "postgres" {
		steps = append(steps, databaseAccessStep(cfg))
	}

	// 4. Build script (composer / npm / artisan).
	// We pin `php` and `composer` to the per-deployment PHP version by
	// dropping a small `.laraenv-bin` directory at the front of PATH:
	//   .laraenv-bin/php       → /usr/bin/php<VER>
	//   .laraenv-bin/composer  → /usr/bin/php<VER> /usr/bin/composer
	// This way the user's build script can write `composer install` /
	// `php artisan migrate` naturally and they always use the right PHP,
	// even on a host with multiple PHP versions installed for different
	// sites. The global `php` symlink is left alone.
	//
	// COMPOSER_ALLOW_SUPERUSER=1 silences the "do not run as root" nag —
	// for VPS deploys we run as root (or a sudoer) intentionally.
	if deployScript != "" {
		if legacy {
			ver := cfg.PHPVersion
			if ver == "" {
				ver = "8.3"
			}
			steps = append(steps, Step{
				Title:    "Run build script",
				TitleKey: "deployments.steps.runBuildScript",
				Cmd: fmt.Sprintf(
					`export COMPOSER_ALLOW_SUPERUSER=1 && `+
						`cd %[1]s && `+
						`mkdir -p .laraenv-bin && `+
						`ln -sf /usr/bin/php%[2]s .laraenv-bin/php && `+
						`printf '#!/usr/bin/env bash\nexec /usr/bin/php%[2]s /usr/bin/composer "$@"\n' > .laraenv-bin/composer && `+
						`chmod +x .laraenv-bin/composer && `+
						`export PATH="$(pwd)/.laraenv-bin:$PATH" && `+
						`echo "Using $(php --version | head -n1)" && `+
						`%[3]s`,
					shQuote(deployRoot), ver, deployScript,
				),
			})
		} else {
			// Preset pipeline. LARAENV_FORCE=1 because this runs right after
			// a fresh clone: there's no .laraenv-deployed-sha marker yet, and
			// the pipeline's first-deploy path must run everything regardless.
			steps = append(steps, Step{
				Title:    "Run deploy pipeline",
				TitleKey: "deployments.steps.runDeployPipeline",
				Cmd:      "export LARAENV_FORCE=1\n" + BuildDeployScript(cfg, deployScript),
			})
		}
	}

	// 4b. Laravel runtime permissions — php-fpm runs as www-data and needs
	// to write into storage/ (for compiled views, sessions, logs) and
	// bootstrap/cache/ (for routes/config caches). Without this, the first
	// web request fails with "tempnam(): file created in the system's
	// temporary directory" because Laravel's Filesystem::replace() can't
	// stage the compiled file inside the target dir.
	if cfg.ProjectKind == "laravel" {
		steps = append(steps, Step{
			Title:    "Set Laravel runtime permissions (storage, bootstrap/cache)",
			TitleKey: "deployments.steps.setLaravelPermissions",
			Cmd: fmt.Sprintf(
				`chown -R www-data:www-data %[1]s/storage %[1]s/bootstrap/cache && `+
					`chmod -R ug+rwX %[1]s/storage %[1]s/bootstrap/cache`,
				shQuote(deployRoot),
			),
			Sudo: true,
		})
	}

	// 4c. PHP-FPM hardening — only meaningful when nginx is the web server
	// (apache uses mod_php, no FPM involved). Three problems we solve here:
	//
	//  (a) Distro default `www` pool ships pm.max_children=5,
	//      pm.max_requests=0 (no recycling → memory bloat → OOM kill),
	//      request_terminate_timeout=0 (hung workers never freed). Under
	//      real traffic all 5 workers stall, the listen backlog fills,
	//      nginx's connect() to the FPM socket fails → 502 Bad Gateway.
	//
	//  (b) systemd's php-fpm unit ships without Restart=. If FPM crashes
	//      (segfault, OOM kill, failed reload), it stays down until manual
	//      `systemctl restart` over SSH.
	//
	//  (c) Brief FPM reload windows (e.g. after `apt upgrade`) surface as
	//      hard 502s because the vhost has no fastcgi_next_upstream retry.
	//      The vhost itself is fixed in nginxVhostFor; the pool + systemd
	//      pieces are written here.
	//
	// All steps idempotent: same content overwrites in place on re-runs.
	if needsPHP && cfg.WebServer != "apache" {
		ver := cfg.PHPVersion
		if ver == "" {
			ver = "8.3"
		}
		appName := safeName(cfg.Name)
		poolPath := fmt.Sprintf("/etc/php/%s/fpm/pool.d/%s.conf", ver, appName)
		pool := phpFPMPoolFor(appName, ver)

		steps = append(steps, Step{
			Title:     "Write PHP-FPM pool for " + appName,
			TitleKey:  "deployments.steps.writePhpFpmPool",
			TitleVars: map[string]any{"name": appName},
			Cmd: fmt.Sprintf(`cat > %s <<'L4E_POOL_EOF'
%s
L4E_POOL_EOF`, shQuote(poolPath), pool),
			Sudo: true,
		})

		// Disable the bundled default `www` pool so we don't run two pools
		// (default + ours) competing for memory. Renaming to .disabled keeps
		// it recoverable. Idempotent — once renamed, the [ -f ] guard makes
		// re-runs a no-op.
		defaultPool := fmt.Sprintf("/etc/php/%s/fpm/pool.d/www.conf", ver)
		steps = append(steps, Step{
			Title:    "Disable default PHP-FPM www pool",
			TitleKey: "deployments.steps.disableDefaultPhpFpmPool",
			Cmd:      fmt.Sprintf(`if [ -f %[1]s ]; then mv %[1]s %[1]s.disabled; fi`, shQuote(defaultPool)),
			Sudo:     true,
			Ignore:   true,
		})

		fpmUnit := fmt.Sprintf("php%s-fpm", ver)
		steps = append(steps, Step{
			Title:     "systemd Restart=always drop-in for " + fpmUnit,
			TitleKey:  "deployments.steps.systemdDropinFpm",
			TitleVars: map[string]any{"unit": fpmUnit},
			Cmd: fmt.Sprintf(`mkdir -p /etc/systemd/system/%[1]s.service.d && cat > /etc/systemd/system/%[1]s.service.d/lara4ever-restart.conf <<'L4E_DROPIN_EOF'
%s
L4E_DROPIN_EOF`, fpmUnit, systemdRestartDropIn()),
			Sudo: true,
		})

		steps = append(steps, Step{
			Title:    "systemd Restart=always drop-in for nginx",
			TitleKey: "deployments.steps.systemdDropinNginx",
			Cmd: fmt.Sprintf(`mkdir -p /etc/systemd/system/nginx.service.d && cat > /etc/systemd/system/nginx.service.d/lara4ever-restart.conf <<'L4E_DROPIN_EOF'
%s
L4E_DROPIN_EOF`, systemdRestartDropIn()),
			Sudo: true,
		})

		// daemon-reload picks up both drop-ins above; restart (not reload)
		// php-fpm so the pool socket path takes effect — reload doesn't
		// re-bind sockets when the listen path changes.
		steps = append(steps, Step{
			Title:     "Reload systemd + restart php" + ver + "-fpm",
			TitleKey:  "deployments.steps.reloadSystemdRestartFpm",
			TitleVars: map[string]any{"version": ver},
			Cmd:       fmt.Sprintf("systemctl daemon-reload && systemctl restart %s", fpmUnit),
			Sudo:      true,
		})
	}

	// 5. Vhost — branches on WebServer.
	switch cfg.WebServer {
	case "apache":
		// Apache uses mod_php (simpler than FPM). Laravel's bundled
		// public/.htaccess already has the rewrite rules — we just need
		// mod_rewrite enabled and AllowOverride All in the vhost.
		vhost := apacheVhostFor(cfg, deployRoot, domain)
		vhostPath := "/etc/apache2/sites-available/" + safeName(cfg.Name) + ".conf"
		phpVer := cfg.PHPVersion
		if phpVer == "" {
			phpVer = "8.3"
		}
		steps = append(steps, Step{
			Title:     "Enable apache modules (rewrite, php" + phpVer + ")",
			TitleKey:  "deployments.steps.enableApacheModules",
			TitleVars: map[string]any{"version": phpVer},
			Cmd:       fmt.Sprintf("a2enmod rewrite php%s 2>/dev/null || a2enmod rewrite", phpVer),
			Sudo:      true,
		})
		steps = append(steps, Step{
			Title:     "Write apache vhost for " + domain,
			TitleKey:  "deployments.steps.writeApacheVhost",
			TitleVars: map[string]any{"domain": domain},
			Cmd: fmt.Sprintf(`cat > %s <<'L4E_VHOST_EOF'
%s
L4E_VHOST_EOF
a2ensite %s`, shQuote(vhostPath), vhost, shQuote(safeName(cfg.Name))),
			Sudo: true,
		})
		steps = append(steps, Step{
			Title:    "Reload apache",
			TitleKey: "deployments.steps.reloadApache",
			Cmd:      "apache2ctl configtest && systemctl reload apache2",
			Sudo:     true,
		})
	default: // nginx
		vhost := nginxVhostFor(cfg, deployRoot, domain)
		vhostPath := "/etc/nginx/sites-available/" + safeName(cfg.Name) + ".conf"
		linkPath := "/etc/nginx/sites-enabled/" + safeName(cfg.Name) + ".conf"
		steps = append(steps, Step{
			Title:     "Write nginx vhost for " + domain,
			TitleKey:  "deployments.steps.writeNginxVhost",
			TitleVars: map[string]any{"domain": domain},
			Cmd: fmt.Sprintf(`cat > %s <<'L4E_VHOST_EOF'
%s
L4E_VHOST_EOF
ln -sf %s %s`, shQuote(vhostPath), vhost, shQuote(vhostPath), shQuote(linkPath)),
			Sudo: true,
		})
		steps = append(steps, Step{
			Title:    "Reload nginx",
			TitleKey: "deployments.steps.reloadNginx",
			Cmd:      "nginx -t && systemctl reload nginx",
			Sudo:     true,
			Ignore:   false,
		})
	}

	// 6. SSL via Let's Encrypt — only after the vhost is live on port 80,
	//    since certbot uses HTTP-01 challenge which talks to the web server.
	if cfg.EnableSSL {
		certbotPlugin := "python3-certbot-nginx"
		certbotFlag := "--nginx"
		if cfg.WebServer == "apache" {
			certbotPlugin = "python3-certbot-apache"
			certbotFlag = "--apache"
		}
		needCertbot := det == nil || det.Installed["certbot"] == ""
		if needCertbot {
			steps = append(steps, Step{
				Title:    "Install certbot",
				TitleKey: "deployments.steps.installCertbot",
				Cmd:      "DEBIAN_FRONTEND=noninteractive apt-get install -y -qq certbot " + certbotPlugin,
				Sudo:     true,
			})
		}
		email := cfg.LetsEncryptEmail
		if email == "" {
			email = "admin@" + cfg.Domain
		}
		steps = append(steps, Step{
			Title:     "Obtain Let's Encrypt certificate for " + domain,
			TitleKey:  "deployments.steps.obtainCertificate",
			TitleVars: map[string]any{"domain": domain},
			// --keep-until-expiring: if a valid cert already exists for this
			// domain, certbot exits 0 without renewing. Re-running Apply on
			// an already-configured host becomes a no-op for this step
			// instead of wasting an ACME challenge each time.
			Cmd: fmt.Sprintf(
				"certbot %s -d %s --non-interactive --agree-tos --email %s --redirect --keep-until-expiring",
				certbotFlag, shQuote(domain), shQuote(email),
			),
			Sudo: true,
		})
	}

	// 7. Auto-deploy: write a pull script + a cron entry that runs it.
	//    Idempotent — re-running just overwrites the crontab line in place.
	if cfg.AutoDeploy && cfg.GitURL != "" {
		schedule := cfg.AutoDeployCron
		if schedule == "" {
			schedule = "*/5 * * * *"
		}
		scriptPath := deployRoot + "/.laraenv-autopull.sh"
		var script string
		if legacy {
			// Pre-preset template, byte-identical to what older versions
			// wrote: pull + (optional) build script on every change.
			buildBlock := ""
			if deployScript != "" {
				buildBlock = "\n  " + strings.ReplaceAll(deployScript, "\n", "\n  ")
			}
			phpVer := cfg.PHPVersion
			if phpVer == "" {
				phpVer = "8.3"
			}
			script = fmt.Sprintf(`#!/usr/bin/env bash
set -e
cd %s
export COMPOSER_ALLOW_SUPERUSER=1
mkdir -p .laraenv-bin
ln -sf /usr/bin/php%s .laraenv-bin/php
printf '#!/usr/bin/env bash\nexec /usr/bin/php%s /usr/bin/composer "$@"\n' > .laraenv-bin/composer
chmod +x .laraenv-bin/composer
export PATH="$(pwd)/.laraenv-bin:$PATH"
BEFORE=$(git rev-parse HEAD)
git fetch --all --prune
git reset --hard origin/%s
AFTER=$(git rev-parse HEAD)
if [ "$BEFORE" != "$AFTER" ]; then
  echo "[laraenv-autopull] $(date -u +%%FT%%TZ) $BEFORE -> $AFTER"%s
fi
`, shQuote(deployRoot), phpVer, phpVer, cfg.GitBranch, buildBlock)
		} else {
			// Preset mode: the cron runs the same rendered pipeline as the
			// build step and "Deploy now" — the pipeline handles its own
			// fetch/reset and conditional installs. It runs in a subshell so
			// its early `exit 0` ("already up to date") doesn't skip the
			// activity-log line, whose format CloudDeploymentActivity parses.
			pipeline := deployScript
			if pipeline == "" {
				// Auto-deploy without a configured pipeline: plain pull.
				pipeline = "git fetch --all --prune\ngit reset --hard \"origin/" + branchOrMain(cfg) + "\""
			}
			script = fmt.Sprintf(`#!/usr/bin/env bash
set -e
cd %s
BEFORE=$(git rev-parse HEAD)
(
%s
)
AFTER=$(git rev-parse HEAD)
if [ "$BEFORE" != "$AFTER" ]; then
  echo "[laraenv-autopull] $(date -u +%%FT%%TZ) $BEFORE -> $AFTER"
fi
`, shQuote(deployRoot), BuildDeployScript(cfg, pipeline))
		}

		// Write via base64 instead of a heredoc: the script now embeds
		// user-authored bash, and any line matching the heredoc delimiter
		// would silently truncate the file. base64 has no such failure mode.
		b64 := base64.StdEncoding.EncodeToString([]byte(script))
		steps = append(steps, Step{
			Title:    "Install auto-pull script",
			TitleKey: "deployments.steps.installAutopullScript",
			Cmd: fmt.Sprintf(`printf '%%s' %s | base64 -d > %s && chmod +x %s`,
				shQuote(b64), shQuote(scriptPath), shQuote(scriptPath)),
		})
		// Crontab line marker so we replace just our entry on re-runs.
		marker := fmt.Sprintf("# laraenv:%s", safeName(cfg.Name))
		cronLine := fmt.Sprintf("%s %s >> /tmp/laraenv-%s.log 2>&1 %s",
			schedule, shQuote(scriptPath), safeName(cfg.Name), marker)
		steps = append(steps, Step{
			Title:     fmt.Sprintf("Schedule auto-pull (%s)", schedule),
			TitleKey:  "deployments.steps.scheduleAutopull",
			TitleVars: map[string]any{"schedule": schedule},
			Cmd: fmt.Sprintf(
				`( crontab -l 2>/dev/null | grep -v %s ; echo %s ) | crontab -`,
				shQuote(marker), shQuote(cronLine),
			),
		})
	}

	return steps
}

// apacheVhostFor returns a Laravel-friendly Apache 2.4 vhost. Uses mod_php
// (simpler than FPM) and AllowOverride All so Laravel's stock public/.htaccess
// rewrite rules take effect.
func apacheVhostFor(cfg ApplyConfig, deployRoot, domain string) string {
	root := deployRoot
	if cfg.ProjectKind == "laravel" || cfg.ProjectKind == "" {
		root = deployRoot + "/public"
	}
	return fmt.Sprintf(`<VirtualHost *:80>
    ServerName %s
    DocumentRoot %s

    <Directory %s>
        Options -Indexes +FollowSymLinks
        AllowOverride All
        Require all granted
    </Directory>

    ErrorLog ${APACHE_LOG_DIR}/%s-error.log
    CustomLog ${APACHE_LOG_DIR}/%s-access.log combined
</VirtualHost>`, domain, root, root, safeName(cfg.Name), safeName(cfg.Name))
}

// BuildDestroyPlan returns the inverse of BuildPlan — the steps needed to
// erase every trace a successful Apply left on the remote host. Each step
// is idempotent (IF EXISTS, rm -f, etc.) so a partial destroy can be
// re-run safely. The order matters: cron + script first (stop new writes),
// then web layer, then SSL, then DB, then the deploy dir last (point of
// no return for local file changes).
func BuildDestroyPlan(cfg ApplyConfig) []Step {
	steps := databaseAccessCleanupSteps(cfg.Name, "")
	deployRoot := "/var/www/" + safeName(cfg.Name)
	domain := cfg.EffectiveDomain()
	marker := "# laraenv:" + safeName(cfg.Name)

	// 1. Drop the auto-pull cron line (if any). grep -v removes our marker
	// line; piping back through `crontab -` writes the new file.
	steps = append(steps, Step{
		Title:    "Remove auto-pull cron entry",
		TitleKey: "deployments.steps.removeAutopullCron",
		Cmd:      fmt.Sprintf(`( crontab -l 2>/dev/null | grep -v %s || true ) | crontab -`, shQuote(marker)),
		Ignore:   true,
	})

	// 2. Remove the auto-pull script (cosmetic — the deploy dir wipe below
	//    catches it anyway, but having it as a discrete step keeps the log
	//    legible).
	steps = append(steps, Step{
		Title:    "Remove auto-pull script",
		TitleKey: "deployments.steps.removeAutopullScript",
		Cmd:      "rm -f " + shQuote(deployRoot+"/.laraenv-autopull.sh"),
		Sudo:     true,
		Ignore:   true,
	})

	// 3. Web server: disable + remove vhost, reload. Branches on WebServer.
	switch cfg.WebServer {
	case "apache":
		steps = append(steps, Step{
			Title:    "Disable + remove apache vhost",
			TitleKey: "deployments.steps.disableRemoveApacheVhost",
			Cmd: fmt.Sprintf(
				"a2dissite %s 2>/dev/null || true; rm -f /etc/apache2/sites-available/%s.conf",
				shQuote(safeName(cfg.Name)), safeName(cfg.Name),
			),
			Sudo:   true,
			Ignore: true,
		})
		steps = append(steps, Step{
			Title:    "Reload apache",
			TitleKey: "deployments.steps.reloadApache",
			Cmd:      "apache2ctl configtest && systemctl reload apache2",
			Sudo:     true,
			Ignore:   true,
		})
	default:
		// nginx (default + explicit "nginx").
		steps = append(steps, Step{
			Title:    "Remove nginx vhost",
			TitleKey: "deployments.steps.removeNginxVhost",
			Cmd: fmt.Sprintf(
				"rm -f /etc/nginx/sites-enabled/%s.conf /etc/nginx/sites-available/%s.conf",
				safeName(cfg.Name), safeName(cfg.Name),
			),
			Sudo:   true,
			Ignore: true,
		})
		steps = append(steps, Step{
			Title:    "Reload nginx",
			TitleKey: "deployments.steps.reloadNginx",
			Cmd:      "nginx -t && systemctl reload nginx",
			Sudo:     true,
			Ignore:   true,
		})

		// Remove the per-app PHP-FPM pool + slow log + socket. Globs both
		// "php*-fpm/pool.d/<name>.conf" and the slow log so we don't have to
		// remember the original PHP version (the user may have upgraded).
		// Reload (not restart) FPM after — restart would briefly drop other
		// sites sharing the same FPM master.
		appName := safeName(cfg.Name)
		steps = append(steps, Step{
			Title:    "Remove PHP-FPM pool config + slow log",
			TitleKey: "deployments.steps.removePhpFpmPoolConfig",
			Cmd:      fmt.Sprintf(`rm -f /etc/php/*/fpm/pool.d/%[1]s.conf /var/log/php*-fpm-%[1]s.slow.log /var/run/php/php*-fpm-%[1]s.sock`, appName),
			Sudo:     true,
			Ignore:   true,
		})
		steps = append(steps, Step{
			Title:    "Reload PHP-FPM",
			TitleKey: "deployments.steps.reloadPhpFpm",
			Cmd:      `for u in $(systemctl list-units --type=service --no-legend 'php*-fpm.service' | awk '{print $1}'); do systemctl reload "$u" 2>/dev/null || true; done`,
			Sudo:     true,
			Ignore:   true,
		})
	}

	// 4. Revoke Let's Encrypt cert (only if SSL was enabled). Ignore errors —
	//    cert may have already been deleted, never issued, or expired.
	if cfg.EnableSSL && domain != "" {
		steps = append(steps, Step{
			Title:     "Revoke Let's Encrypt certificate for " + domain,
			TitleKey:  "deployments.steps.revokeCertificate",
			TitleVars: map[string]any{"domain": domain},
			Cmd: fmt.Sprintf(
				"certbot delete --cert-name %s --non-interactive 2>/dev/null || true",
				shQuote(domain),
			),
			Sudo:   true,
			Ignore: true,
		})
	}

	// Only PostgreSQL resources created by this deployment are eligible for drop.
	pgCfg := ApplyConfig{Name: cfg.Name, Database: "postgres"}
	steps = append(steps, Step{
		Title: "Drop managed PostgreSQL DB + user", TitleKey: "deployments.steps.dropManagedPostgresDb",
		Cmd: databaseScript(pgCfg, databaseDestroyPostgres), Sudo: true,
	})

	// 5. Drop the MySQL DB + user. Mirrors the bootstrap-step's identifier
	//    munging so we DROP the same names we created.
	if cfg.Database == "mysql" {
		dbSafeName := strings.ReplaceAll(safeName(cfg.Name), "-", "_")
		dbName := dbSafeName + "_db"
		dbUser := dbSafeName
		if len(dbUser) > 32 {
			dbUser = dbUser[:32]
		}
		steps = append(steps, Step{
			Title:     "Drop MySQL DB " + dbName + " + user " + dbUser,
			TitleKey:  "deployments.steps.dropMysqlDb",
			TitleVars: map[string]any{"dbName": dbName, "dbUser": dbUser},
			Cmd: fmt.Sprintf(
				`mysql -e "DROP DATABASE IF EXISTS %s; DROP USER IF EXISTS '%s'@'localhost'; FLUSH PRIVILEGES;"`,
				dbName, dbUser,
			),
			Sudo:   true,
			Ignore: true,
		})
	}

	// 6. Wipe the deploy dir. Last destructive step — everything before is
	//    reversible by re-running Apply. Also removes the sibling keep dir
	//    (preserved .env/storage from a re-apply that failed mid-clone).
	steps = append(steps, Step{
		Title:     "Wipe deploy dir " + deployRoot,
		TitleKey:  "deployments.steps.wipeDeployDir",
		TitleVars: map[string]any{"path": deployRoot},
		Cmd: "rm -rf " + shQuote(deployRoot) + " " +
			shQuote("/var/www/.laraenv-keep-"+safeName(cfg.Name)),
		Sudo: true,
	})

	return steps
}

// nginxVhostFor returns a Laravel-friendly server block for the given config.
// Plain HTTP for MVP — Let's Encrypt automation comes in a follow-up.
func nginxVhostFor(cfg ApplyConfig, deployRoot, domain string) string {
	root := deployRoot
	if cfg.ProjectKind == "laravel" || cfg.ProjectKind == "" {
		root = deployRoot + "/public"
	}

	phpFastCGI := ""
	if cfg.PHPVersion != "" || cfg.ProjectKind == "laravel" {
		ver := cfg.PHPVersion
		if ver == "" {
			ver = "8.3"
		}
		// Per-app socket (matches the pool config in 4c) — keeps each site's
		// FPM workers isolated and lets us tune pm.* per project later.
		// fastcgi_next_upstream + retries turn brief FPM reload windows into
		// a 1-2s blip instead of a hard 502.
		phpFastCGI = fmt.Sprintf(`
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:/var/run/php/php%[1]s-fpm-%[2]s.sock;
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;

        fastcgi_connect_timeout 60s;
        fastcgi_send_timeout    300s;
        fastcgi_read_timeout    300s;
        fastcgi_buffers 16 16k;
        fastcgi_buffer_size 32k;

        fastcgi_next_upstream error timeout invalid_header http_500 http_503;
        fastcgi_next_upstream_tries 3;
        fastcgi_next_upstream_timeout 10s;
    }`, ver, safeName(cfg.Name))
	}

	return fmt.Sprintf(`server {
    listen 80;
    listen [::]:80;
    server_name %s;
    root %s;

    index index.php index.html;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }
%s
    location ~ /\.(?!well-known) {
        deny all;
    }

    access_log /var/log/nginx/%s.access.log;
    error_log  /var/log/nginx/%s.error.log;
}`, domain, root, phpFastCGI, safeName(cfg.Name), safeName(cfg.Name))
}

// phpFPMPoolFor returns a per-application PHP-FPM pool config. Defaults are
// tuned for a small/medium VPS (~1.5GB free for PHP) and to eliminate the
// classic 502 triggers: pool starvation, worker memory bloat, hung requests.
//
// Tunables here (max_children=25, max_requests=500, terminate_timeout=300s)
// are intentionally hard-coded for now — the values cover the 95% case.
// Promoted to ApplyConfig fields if/when a deploy needs different sizing.
func phpFPMPoolFor(appName, ver string) string {
	return fmt.Sprintf(`[%[1]s]
user = www-data
group = www-data
listen = /var/run/php/php%[2]s-fpm-%[1]s.sock
listen.owner = www-data
listen.group = www-data
listen.mode = 0660
listen.backlog = 8192

pm = dynamic
pm.max_children = 25
pm.start_servers = 4
pm.min_spare_servers = 2
pm.max_spare_servers = 8
pm.max_requests = 500

request_terminate_timeout = 300
request_slowlog_timeout = 10s
slowlog = /var/log/php%[2]s-fpm-%[1]s.slow.log

catch_workers_output = yes
decorate_workers_output = no
`, appName, ver)
}

// systemdRestartDropIn returns the contents of a systemd unit drop-in that
// makes the targeted service restart automatically after any failure mode
// (crash, OOM kill, failed reload). 2-second backoff is short enough to
// recover before nginx's fastcgi_next_upstream window expires.
// StartLimitIntervalSec=0 disables the rate limiter so a flapping service
// keeps trying to recover instead of giving up after 5 starts.
func systemdRestartDropIn() string {
	return `[Service]
Restart=always
RestartSec=2s
StartLimitIntervalSec=0
`
}

// missingPackages diff'd against detection. Returns apt package names.
func missingPackages(cfg ApplyConfig, det *DetectionResult) []string {
	have := map[string]bool{}
	if det != nil {
		for k := range det.Installed {
			have[k] = true
		}
	}
	var out []string

	if !have["git"] {
		out = append(out, "git")
	}

	if cfg.WebServer == "" || cfg.WebServer == "nginx" {
		if !have["nginx"] {
			out = append(out, "nginx")
		}
	} else if cfg.WebServer == "apache" {
		if !have["apache"] {
			out = append(out, "apache2")
		}
	}

	if cfg.PHPVersion != "" || cfg.ProjectKind == "laravel" {
		ver := cfg.PHPVersion
		if ver == "" {
			ver = "8.3"
		}
		// have["php"] is true if ANY php is installed. We need to check
		// the SPECIFIC requested version — a host with php8.3 from a
		// previous deploy doesn't satisfy a request for php8.4.
		// detection.Installed["php"] holds the version line (e.g.
		// "PHP 8.3.6 (cli) (built: ...)"); look for "PHP <ver>." in it.
		hasRequestedPHP := false
		if det != nil {
			hasRequestedPHP = strings.Contains(det.Installed["php"], "PHP "+ver+".")
		}
		if !hasRequestedPHP {
			out = append(out,
				"php"+ver,
				"php"+ver+"-fpm",
				"php"+ver+"-cli",
				"php"+ver+"-mbstring",
				"php"+ver+"-xml",
				"php"+ver+"-curl",
				"php"+ver+"-bcmath",
				"php"+ver+"-zip",
				"php"+ver+"-intl",
			)
			if cfg.Database == "mysql" {
				out = append(out, "php"+ver+"-mysql")
			}
			if cfg.Database == "postgres" {
				out = append(out, "php"+ver+"-pgsql")
			}
		}
		if !have["composer"] && cfg.ProjectKind == "laravel" {
			out = append(out, "composer")
		}
	}

	switch cfg.Database {
	case "mysql":
		if !have["mysql"] {
			out = append(out, "mysql-server")
		}
	case "postgres":
		if !have["postgres"] {
			out = append(out, "postgresql")
		}
	}

	if cfg.ProjectKind == "laravel" && !have["node"] {
		// Laravel projects almost always have a Vite/Mix build step.
		out = append(out, "nodejs", "npm")
	}

	return out
}

// branchOrMain returns the configured git branch, defaulting to "main".
func branchOrMain(cfg ApplyConfig) string {
	if cfg.GitBranch == "" {
		return "main"
	}
	return cfg.GitBranch
}

// safeName strips characters that could break shell quoting / paths.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// shQuote wraps in single quotes for safe inclusion in a bash command.
// Single-quote inside the value is escaped via the standard '\” trick.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
