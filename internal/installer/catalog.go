package installer

import (
	"lara4ever/internal/i18n"
	"lara4ever/internal/paths"
	"lara4ever/internal/vcruntime"
)

// Kind identifies a source category.
type Kind string

const (
	KindNginx    Kind = "nginx"
	KindApache   Kind = "apache"
	KindCaddy    Kind = "caddy"
	KindMySQL    Kind = "mysql"
	KindPostgres Kind = "postgres"
	KindPgvector Kind = "pgvector"
	KindMailpit  Kind = "mailpit"
	KindPHP      Kind = "php"
	KindPython   Kind = "python"
	KindNodejs   Kind = "nodejs"
	KindRedis    Kind = "redis"
	KindCmder    Kind = "cmder"
	KindComposer Kind = "composer"
	// KindPeclExt is a synthetic kind used only at install time for PECL
	// extension zips (xdebug, redis, …). PECL sources are built on the fly
	// from PeclCatalog(), they don't appear in the regular Catalog() list
	// and don't show up on the Libs/Tools page.
	KindPeclExt Kind = "pecl-ext"
)

// Source describes a downloadable archive and where to install it.
type Source struct {
	Kind    Kind   `json:"kind"`
	Version string `json:"version"`
	URL     string `json:"url"`
	// InstallTo is the absolute target directory (created if missing).
	// For PHP/Python we install into per-version subfolders; others use a single dir.
	InstallTo string `json:"installTo"`
	// Flatten = if the archive contains a single top-level folder, move its
	// contents up into InstallTo (avoids C:\lara4ever\nginx\nginx-1.27.3\nginx.exe).
	Flatten bool `json:"flatten"`
	// Direct = file is downloaded and copied as-is (no zip extraction).
	// Used for single-file payloads like composer.phar. The downloaded file
	// keeps its basename (from the URL) when written to InstallTo.
	Direct bool `json:"direct,omitempty"`
	// Manual = origem bloqueia download direto. App abre BrowserURL e espera
	// o usuário colocar o arquivo no cache manualmente.
	Manual bool `json:"manual,omitempty"`
	// BrowserURL é a página a abrir quando Manual é true (default = URL).
	BrowserURL string `json:"browserUrl,omitempty"`
	// Notes shown in the UI.
	Notes string `json:"notes,omitempty"`
	// Prerelease marks a build that is not a stable release (e.g. a PHP
	// beta/RC). The UI must call this out clearly — as a warning, not a
	// neutral version tag — since a prerelease can behave unexpectedly,
	// lack ecosystem support (PECL extensions, etc.), and isn't meant for
	// production use.
	Prerelease bool   `json:"prerelease,omitempty"`
	LTS        bool   `json:"lts,omitempty"`     // Node release line currently supported as LTS.
	LTSFrom    string `json:"ltsFrom,omitempty"` // First release carrying LTS metadata in this line.
	// RedistURL is the Visual C++ redistributable this build needs, when it
	// differs from the app-wide default (vcruntime.RedistURL). Empty means
	// the default applies. Set this whenever a build uses a newer/older MSVC
	// toolchain than most of the catalog — e.g. PHP 8.6's vs18 build needs
	// vcruntime.RedistURLVS18, not the vs17 redistributable everything else
	// on this list was compiled against.
	RedistURL string `json:"redistUrl,omitempty"`
}

// RedistURLForPHPSeries returns the Visual C++ redistributable URL required
// by an installed PHP major.minor series (e.g. "8.6"), looked up from the
// catalog entry whose InstallTo matches that series. Returns "" when the
// series needs only the default redistributable (vcruntime.RedistURL) or
// isn't found — callers should treat "" as "use the default", exactly like
// vcruntime.Check does when given an empty redistURL.
func RedistURLForPHPSeries(series string) string {
	if series == "" {
		return ""
	}
	want := paths.Root() + `\php\` + series
	for _, s := range Catalog() {
		if s.Kind == KindPHP && s.InstallTo == want {
			return s.RedistURL
		}
	}
	return ""
}

// Catalog returns the curated list of installable sources.
// URLs are pinned to known-good versions; bump as needed.
func Catalog() []Source {
	root := paths.Root()
	return []Source{
		// ===== Nginx (Windows) =====
		{Kind: KindNginx, Version: "1.30.0",
			URL:       "https://nginx.org/download/nginx-1.30.0.zip",
			InstallTo: root + `\nginx`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.nginxStable", nil)},
		{Kind: KindNginx, Version: "1.29.8",
			URL:       "https://nginx.org/download/nginx-1.29.8.zip",
			InstallTo: root + `\nginx`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.nginxMainline", nil)},

		// ===== Caddy (Windows) =====
		{Kind: KindCaddy, Version: "2.11.4",
			URL:       "https://github.com/caddyserver/caddy/releases/download/v2.11.4/caddy_2.11.4_windows_amd64.zip",
			InstallTo: root + `\caddy`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.caddySingleBinary", nil)},

		// ===== Mailpit (SMTP catcher para dev) =====
		{Kind: KindMailpit, Version: "1.29.7",
			URL:       "https://github.com/axllent/mailpit/releases/download/v1.29.7/mailpit-windows-amd64.zip",
			InstallTo: root + `\mailpit`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.mailpitPorts", nil)},
		{Kind: KindMailpit, Version: "1.29.6",
			URL:       "https://github.com/axllent/mailpit/releases/download/v1.29.6/mailpit-windows-amd64.zip",
			InstallTo: root + `\mailpit`, Flatten: false},

		// ===== PostgreSQL (EnterpriseDB binaries) =====
		{Kind: KindPostgres, Version: "18.3",
			URL:       "https://www.enterprisedb.com/download-postgresql-binaries",
			InstallTo: root + `\postgres`, Flatten: true,
			Manual:     true,
			BrowserURL: "https://www.enterprisedb.com/download-postgresql-binaries",
			Notes:      i18n.T("backend.installer.notes.postgresManualDownload", nil)},
		{Kind: KindPostgres, Version: "17.9",
			URL:       "https://www.enterprisedb.com/download-postgresql-binaries",
			InstallTo: root + `\postgres`, Flatten: true,
			Manual:     true,
			BrowserURL: "https://www.enterprisedb.com/download-postgresql-binaries",
			Notes:      i18n.T("backend.installer.notes.postgresPreviousStable", nil)},
		{Kind: KindPostgres, Version: "16.13",
			URL:       "https://www.enterprisedb.com/download-postgresql-binaries",
			InstallTo: root + `\postgres`, Flatten: true,
			Manual:     true,
			BrowserURL: "https://www.enterprisedb.com/download-postgresql-binaries",
			Notes:      i18n.T("backend.installer.notes.postgresLtsPatches2028", nil)},
		{Kind: KindPostgres, Version: "15.17",
			URL:       "https://www.enterprisedb.com/download-postgresql-binaries",
			InstallTo: root + `\postgres`, Flatten: true,
			Manual:     true,
			BrowserURL: "https://www.enterprisedb.com/download-postgresql-binaries",
			Notes:      i18n.T("backend.installer.notes.postgresPatchesUntil2027", nil)},

		// ===== pgvector (extensão de similaridade vetorial p/ PostgreSQL) =====
		// Binários Windows compilados por nós (MSVC) e publicados como assets
		// no repo de releases. Um zip por major do PG — a extensão é ABI-bound.
		// Extrai por cima da árvore \postgres (lib\vector.dll + share\extension\*);
		// IsInstalled usa vector.control como sentinela, nunca o marker file.
		{Kind: KindPgvector, Version: "0.8.6-pg18",
			URL:       "https://github.com/thayronarrais/laraenv/releases/download/pgvector-0.8.6/pgvector-0.8.6-pg18-win-x64.zip",
			InstallTo: root + `\postgres`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.pgvectorPg18", nil)},
		{Kind: KindPgvector, Version: "0.8.6-pg17",
			URL:       "https://github.com/thayronarrais/laraenv/releases/download/pgvector-0.8.6/pgvector-0.8.6-pg17-win-x64.zip",
			InstallTo: root + `\postgres`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.pgvectorGeneric", map[string]any{"pg": "17"})},
		{Kind: KindPgvector, Version: "0.8.6-pg16",
			URL:       "https://github.com/thayronarrais/laraenv/releases/download/pgvector-0.8.6/pgvector-0.8.6-pg16-win-x64.zip",
			InstallTo: root + `\postgres`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.pgvectorGeneric", map[string]any{"pg": "16"})},
		{Kind: KindPgvector, Version: "0.8.6-pg15",
			URL:       "https://github.com/thayronarrais/laraenv/releases/download/pgvector-0.8.6/pgvector-0.8.6-pg15-win-x64.zip",
			InstallTo: root + `\postgres`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.pgvectorGeneric", map[string]any{"pg": "15"})},

		// ===== Apache (httpd via Apache Lounge) =====
		// Apache Lounge bloqueia downloads automatizados (hotlink-protected).
		// O app abre a página oficial; baixe o zip e coloque em
		// C:\lara4ever\cache\apache-<versão>.zip — o instalador pega de lá.
		{Kind: KindApache, Version: "2.4.62",
			URL:       "https://www.apachelounge.com/download/VS17/binaries/httpd-2.4.62-240904-win64-VS17.zip",
			InstallTo: root + `\apache`, Flatten: true,
			Manual:     true,
			BrowserURL: "https://www.apachelounge.com/download/",
			Notes:      i18n.T("backend.installer.notes.apacheLoungeBlocked", map[string]any{"version": "2.4.62"})},

		// ===== MySQL Community Server =====
		// dev.mysql.com/get/ redireciona para o CDN. URL muda por patch — quando
		// uma versão der 404, atualize para a mais recente em
		// https://dev.mysql.com/downloads/mysql/
		{Kind: KindMySQL, Version: "9.7.0",
			URL:       "https://dev.mysql.com/get/Downloads/MySQL-9.7/mysql-9.7.0-winx64.zip",
			InstallTo: root + `\mysql`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.mysqlCurrentLts", nil)},
		{Kind: KindMySQL, Version: "8.4.9",
			URL:       "https://dev.mysql.com/get/Downloads/MySQL-8.4/mysql-8.4.9-winx64.zip",
			InstallTo: root + `\mysql`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.mysql84Lts", nil)},
		{Kind: KindMySQL, Version: "8.0.46",
			URL:       "https://dev.mysql.com/get/Downloads/MySQL-8.0/mysql-8.0.46-winx64.zip",
			InstallTo: root + `\mysql`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.mysqlInitInsecure", nil)},

		// ===== PHP (Windows, Non-Thread-Safe) =====
		// VS/VC compiler varia por branch:
		//   5.3 = VC9 (x86 only)   |   5.4 = VC9 (x86 only)
		//   5.5/5.6 = VC11         |   7.0/7.1 = VC14
		//   7.2/7.3/7.4 = VC15     |   8.0/8.1/8.2/8.3 = vs16     |   8.4/8.5 = vs17
		//   8.6 = vs18 (PHP 8.6 has no stable release yet — only qa/ betas)
		// Versões antigas vivem em /releases/archives/.
		{Kind: KindPHP, Version: "8.6.0beta3",
			URL:       "https://windows.php.net/downloads/qa/archives/php-8.6.0beta3-nts-Win32-vs18-x64.zip",
			InstallTo: root + `\php\8.6`, Flatten: false,
			Prerelease: true,
			RedistURL:  vcruntime.RedistURLVS18,
			Notes:      i18n.T("backend.installer.notes.phpBetaWarning", nil)},
		{Kind: KindPHP, Version: "8.5.10",
			URL:       "https://windows.php.net/downloads/releases/php-8.5.10-nts-Win32-vs17-x64.zip",
			InstallTo: root + `\php\8.5`, Flatten: false},
		{Kind: KindPHP, Version: "8.4.25",
			URL:       "https://windows.php.net/downloads/releases/archives/php-8.4.25-nts-Win32-vs17-x64.zip",
			InstallTo: root + `\php\8.4`, Flatten: false},
		{Kind: KindPHP, Version: "8.3.33",
			URL:       "https://windows.php.net/downloads/releases/php-8.3.33-nts-Win32-vs16-x64.zip",
			InstallTo: root + `\php\8.3`, Flatten: false},
		{Kind: KindPHP, Version: "8.2.33",
			URL:       "https://windows.php.net/downloads/releases/php-8.2.33-nts-Win32-vs16-x64.zip",
			InstallTo: root + `\php\8.2`, Flatten: false},
		{Kind: KindPHP, Version: "8.1.34",
			URL:       "https://windows.php.net/downloads/releases/php-8.1.34-nts-Win32-vs16-x64.zip",
			InstallTo: root + `\php\8.1`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.phpSecurityFixOnlyEol2025", nil)},
		{Kind: KindPHP, Version: "8.0.30",
			URL:       "https://windows.php.net/downloads/releases/archives/php-8.0.30-nts-Win32-vs16-x64.zip",
			InstallTo: root + `\php\8.0`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.phpEndOfLifeNoPatches", nil)},
		{Kind: KindPHP, Version: "7.4.33",
			URL:       "https://windows.php.net/downloads/releases/archives/php-7.4.33-nts-Win32-vc15-x64.zip",
			InstallTo: root + `\php\7.4`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.eolRequiresRedist", map[string]any{"redist": "VC15"})},
		{Kind: KindPHP, Version: "7.3.33",
			URL:       "https://windows.php.net/downloads/releases/archives/php-7.3.33-nts-Win32-VC15-x64.zip",
			InstallTo: root + `\php\7.3`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.eolRequiresRedist", map[string]any{"redist": "VC15"})},
		{Kind: KindPHP, Version: "7.2.34",
			URL:       "https://windows.php.net/downloads/releases/archives/php-7.2.34-nts-Win32-VC15-x64.zip",
			InstallTo: root + `\php\7.2`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.eolRequiresRedist", map[string]any{"redist": "VC15"})},
		{Kind: KindPHP, Version: "7.1.33",
			URL:       "https://windows.php.net/downloads/releases/archives/php-7.1.33-nts-Win32-VC14-x64.zip",
			InstallTo: root + `\php\7.1`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.eolRequiresRedist", map[string]any{"redist": "VC14"})},
		{Kind: KindPHP, Version: "7.0.33",
			URL:       "https://windows.php.net/downloads/releases/archives/php-7.0.33-nts-Win32-VC14-x64.zip",
			InstallTo: root + `\php\7.0`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.eolRequiresRedist", map[string]any{"redist": "VC14"})},
		{Kind: KindPHP, Version: "5.6.40",
			URL:       "https://windows.php.net/downloads/releases/archives/php-5.6.40-nts-Win32-VC11-x64.zip",
			InstallTo: root + `\php\5.6`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.eolRequiresRedist", map[string]any{"redist": "VC11"})},

		// ===== Python (embeddable Windows x64) =====
		{Kind: KindPython, Version: "3.13.1",
			URL:       "https://www.python.org/ftp/python/3.13.1/python-3.13.1-embed-amd64.zip",
			InstallTo: root + `\python\3.13`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.pythonEmbeddable", nil)},
		{Kind: KindPython, Version: "3.12.7",
			URL:       "https://www.python.org/ftp/python/3.12.7/python-3.12.7-embed-amd64.zip",
			InstallTo: root + `\python\3.12`, Flatten: false},

		// ===== Node.js (Windows x64, official) =====
		// Per-version subfolders so múltiplas versões coexistem; o symlink
		// C:\lara4ever\nodejs\current aponta para a ativa (gerenciada por
		// internal/nodejs/manager.go).
		{Kind: KindNodejs, Version: "24.15.0", LTS: true, LTSFrom: "24.11.0",
			URL:       "https://nodejs.org/dist/v24.15.0/node-v24.15.0-win-x64.zip",
			InstallTo: root + `\nodejs\24.15.0`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.nodejsLatestLts", nil)},
		{Kind: KindNodejs, Version: "22.22.2", LTS: true, LTSFrom: "22.11.0",
			URL:       "https://nodejs.org/dist/v22.22.2/node-v22.22.2-win-x64.zip",
			InstallTo: root + `\nodejs\22.22.2`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.nodejsJodLts", nil)},
		{Kind: KindNodejs, Version: "20.20.2",
			URL:       "https://nodejs.org/dist/v20.20.2/node-v20.20.2-win-x64.zip",
			InstallTo: root + `\nodejs\20.20.2`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.nodejsEolLegacy", nil)},

		// ===== Redis (Windows port — tporadowski/redis) =====
		// Build oficial do Redis não tem suporte a Windows; usamos o fork
		// estável fixado abaixo (BSD-3-Clause e termos dos componentes).
		{Kind: KindRedis, Version: "5.0.14.1",
			URL:       "https://github.com/tporadowski/redis/releases/download/v5.0.14.1/Redis-x64-5.0.14.1.zip",
			InstallTo: root + `\redis`, Flatten: true,
			Notes: i18n.T("backend.installer.notes.redisWindowsPort", nil)},

		// ===== Composer (PHP dependency manager) =====
		// Single-file PHAR — uses Direct (no zip extraction). Installer also
		// drops composer.bat and a POSIX wrapper so the terminal can invoke
		// `composer` directly once the active PHP is on PATH.
		{Kind: KindComposer, Version: "2.8.5",
			URL:       "https://getcomposer.org/download/2.8.5/composer.phar",
			InstallTo: root + `\composer`, Direct: true,
			Notes: i18n.T("backend.installer.notes.composerLatestStable", nil)},
		{Kind: KindComposer, Version: "2.2.25",
			URL:       "https://getcomposer.org/download/2.2.25/composer.phar",
			InstallTo: root + `\composer`, Direct: true,
			Notes: i18n.T("backend.installer.notes.composer22Lts", nil)},

		// ===== Cmder (terminal opcional para Projects) =====
		{Kind: KindCmder, Version: "1.3.25",
			URL:       "https://github.com/cmderdev/cmder/releases/download/v1.3.25/cmder.zip",
			InstallTo: root + `\cmder`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.cmderFull", nil)},
		{Kind: KindCmder, Version: "1.3.25-mini",
			URL:       "https://github.com/cmderdev/cmder/releases/download/v1.3.25/cmder_mini.zip",
			InstallTo: root + `\cmder`, Flatten: false,
			Notes: i18n.T("backend.installer.notes.cmderMini", nil)},
	}
}
