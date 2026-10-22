package installer

import (
	"fmt"
	"strings"

	"lara4ever/internal/i18n"
)

// PeclBuild pins one PECL extension version to one PHP series. The pair
// (PHPSeries, Version) feeds into the windows.php.net URL scheme.
type PeclBuild struct {
	PHPSeries string `json:"phpSeries"` // "8.4", "8.3", ... (matches the PHP catalog Version's major.minor)
	Version   string `json:"version"`   // "3.4.0"
}

// PeclExtension describes one extension family (a name + per-PHP-version
// builds + metadata used to seed php.ini correctly).
type PeclExtension struct {
	Name        string      `json:"name"`        // bare name (e.g. "xdebug")
	DisplayName string      `json:"displayName"` // shown in the UI
	Description string      `json:"description"`
	// Zend = loaded via `zend_extension=<name>` instead of `extension=<name>`.
	// xdebug, opcache, ionCube, blackfire fit here.
	Zend   bool        `json:"zend"`
	Builds []PeclBuild `json:"builds"`
}

// PeclCatalog returns the curated list of supported PECL extensions. Versions
// are pinned to known-good Windows builds hosted on windows.php.net/pecl —
// bump entries as upstream ships new releases. Versions for PHP-series 8.4
// trail by a release or two of whatever's "latest" because PECL's Windows
// build pipeline takes a while to catch up.
func PeclCatalog() []PeclExtension {
	return []PeclExtension{
		{
			Name: "xdebug", DisplayName: "Xdebug", Zend: true,
			Description: i18n.T("backend.installer.extXdebugDescription", nil),
			Builds: []PeclBuild{
				{PHPSeries: "8.5", Version: "3.5.3"},
				{PHPSeries: "8.4", Version: "3.4.0"},
				{PHPSeries: "8.3", Version: "3.4.0"},
				{PHPSeries: "8.2", Version: "3.4.0"},
				{PHPSeries: "8.1", Version: "3.3.2"},
				{PHPSeries: "8.0", Version: "3.1.6"},
				{PHPSeries: "7.4", Version: "3.1.6"},
			},
		},
		{
			Name: "redis", DisplayName: "Redis",
			Description: i18n.T("backend.installer.extRedisDescription", nil),
			Builds: []PeclBuild{
				{PHPSeries: "8.5", Version: "6.3.0"},
				{PHPSeries: "8.4", Version: "6.1.0"},
				{PHPSeries: "8.3", Version: "6.1.0"},
				{PHPSeries: "8.2", Version: "6.1.0"},
				{PHPSeries: "8.1", Version: "6.1.0"},
				{PHPSeries: "8.0", Version: "5.3.7"},
				{PHPSeries: "7.4", Version: "5.3.7"},
			},
		},
		{
			Name: "mongodb", DisplayName: "MongoDB",
			Description: i18n.T("backend.installer.extMongodbDescription", nil),
			Builds: []PeclBuild{
				{PHPSeries: "8.5", Version: "2.5.2"},
				{PHPSeries: "8.4", Version: "1.20.1"},
				{PHPSeries: "8.3", Version: "1.20.1"},
				{PHPSeries: "8.2", Version: "1.20.1"},
				{PHPSeries: "8.1", Version: "1.20.1"},
				{PHPSeries: "8.0", Version: "1.16.2"},
				{PHPSeries: "7.4", Version: "1.16.2"},
			},
		},
		{
			Name: "apcu", DisplayName: "APCu",
			Description: i18n.T("backend.installer.extApcuDescription", nil),
			Builds: []PeclBuild{
				{PHPSeries: "8.5", Version: "5.1.28"},
				{PHPSeries: "8.4", Version: "5.1.24"},
				{PHPSeries: "8.3", Version: "5.1.24"},
				{PHPSeries: "8.2", Version: "5.1.23"},
				{PHPSeries: "8.1", Version: "5.1.23"},
				{PHPSeries: "8.0", Version: "5.1.22"},
				{PHPSeries: "7.4", Version: "5.1.22"},
			},
		},
		{
			Name: "imagick", DisplayName: "Imagick",
			Description: i18n.T("backend.installer.extImagickDescription", nil),
			Builds: []PeclBuild{
				{PHPSeries: "8.5", Version: "3.8.1"},
				{PHPSeries: "8.4", Version: "3.7.0"},
				{PHPSeries: "8.3", Version: "3.7.0"},
				{PHPSeries: "8.2", Version: "3.7.0"},
				{PHPSeries: "8.1", Version: "3.7.0"},
				{PHPSeries: "8.0", Version: "3.7.0"},
				{PHPSeries: "7.4", Version: "3.7.0"},
			},
		},
		// Swoole is deliberately absent. Do not restore it without checking
		// the mirror first: windows.php.net/downloads/pecl/releases/swoole
		// stops at 4.3.6 (2019) and even those directories hold no zips, so
		// every build this catalog used to list — 6.0.1 for 8.3/8.4, 5.1.6
		// for 8.1/8.2 — returned 404 for its entire life. Swoole needs
		// epoll/kqueue and has no native Windows build; on Windows the route
		// is WSL. An install button that always fails teaches users to
		// distrust the whole page.
		{
			Name: "memcached", DisplayName: "Memcached",
			Description: i18n.T("backend.installer.extMemcachedDescription", nil),
			Builds: []PeclBuild{
				{PHPSeries: "8.5", Version: "3.4.0"},
				{PHPSeries: "8.3", Version: "3.3.0"},
				{PHPSeries: "8.2", Version: "3.3.0"},
				{PHPSeries: "8.1", Version: "3.2.0"},
				{PHPSeries: "8.0", Version: "3.2.0"},
				{PHPSeries: "7.4", Version: "3.2.0"},
			},
		},
	}
}

// FindPeclBuild returns the build pinned for a given (extension, PHP series)
// pair. ok=false if the extension isn't in the catalog or there's no build
// pinned for that PHP series.
func FindPeclBuild(extName, phpSeries string) (PeclExtension, PeclBuild, bool) {
	for _, e := range PeclCatalog() {
		if e.Name != extName {
			continue
		}
		for _, b := range e.Builds {
			if b.PHPSeries == phpSeries {
				return e, b, true
			}
		}
	}
	return PeclExtension{}, PeclBuild{}, false
}

// PeclSeries returns the PHP series ("8.4", "7.4", …) for a full version
// string ("8.4.20"). Empty input returns empty.
func PeclSeries(phpVersion string) string {
	parts := strings.SplitN(phpVersion, ".", 3)
	if len(parts) < 2 {
		return phpVersion
	}
	return parts[0] + "." + parts[1]
}

// peclVCTag returns the VC compiler tag windows.php.net/pecl uses for a
// given PHP series. The mapping mirrors what `windows.php.net/downloads/releases`
// uses for the matching PHP zip — PECL builds always use the same toolchain
// as the PHP build they target.
func peclVCTag(phpSeries string) string {
	switch phpSeries {
	case "8.6":
		return "vs18"
	case "8.5", "8.4":
		return "vs17"
	case "8.0", "8.1", "8.2", "8.3":
		return "vs16"
	case "7.2", "7.3", "7.4":
		return "vc15"
	case "7.0", "7.1":
		return "vc14"
	case "5.6":
		return "vc11"
	}
	return ""
}

// peclZipURL builds the canonical PECL zip URL for a given build. The
// scheme is documented at https://windows.php.net/downloads/pecl/releases/ —
// every build follows it. NTS x64 is hard-coded to match the PHP series we
// ship in the catalog.
func peclZipURL(extName string, b PeclBuild) string {
	vc := peclVCTag(b.PHPSeries)
	if vc == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://windows.php.net/downloads/pecl/releases/%s/%s/php_%s-%s-%s-nts-%s-x64.zip",
		extName, b.Version, extName, b.Version, b.PHPSeries, vc,
	)
}

// PeclSourceFor materializes a Source describing the PECL zip for one
// (extension, PHP version) pair. InstallTo points at the PHP install's ext/
// folder so installPecl can extract php_<ext>.dll there directly. ok=false
// if there's no catalog entry for the pair.
func PeclSourceFor(extName, phpVersion, phpExtDir string) (Source, PeclExtension, bool) {
	series := PeclSeries(phpVersion)
	ext, build, found := FindPeclBuild(extName, series)
	if !found {
		return Source{}, PeclExtension{}, false
	}
	url := peclZipURL(extName, build)
	if url == "" {
		return Source{}, PeclExtension{}, false
	}
	return Source{
		Kind:      KindPeclExt,
		Version:   build.Version,
		URL:       url,
		InstallTo: phpExtDir,
	}, ext, true
}
