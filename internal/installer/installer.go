package installer

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"lara4ever/internal/i18n"
	"lara4ever/internal/runtimeinventory"
)

// Progress is emitted periodically during a download/extract.
type Progress struct {
	Kind        Kind   `json:"kind"`
	Version     string `json:"version"`
	Phase       string `json:"phase"` // "download" | "extract" | "done" | "error"
	Bytes       int64  `json:"bytes"`
	Total       int64  `json:"total"`
	Percent     int    `json:"percent"`
	BytesPerSec int64  `json:"bytesPerSec"`
	Message     string `json:"message,omitempty"`
}

// Manager downloads and installs sources from the catalog.
type Manager struct {
	cacheDir string
	emit     func(Progress)
}

func NewManager(cacheDir string, emit func(Progress)) *Manager {
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "laraenv-cache")
	}
	_ = os.MkdirAll(cacheDir, 0755)
	return &Manager{cacheDir: cacheDir, emit: emit}
}

// IsInstalled checks if the given Source's exact version is installed.
//
// Many kinds (mysql, nginx, postgres, redis, mailpit) share a single InstallTo
// across all versions — so "dir non-empty" would falsely report every version
// of those kinds as installed once any one was installed. We solve this by
// writing a `.lara4ever-version` marker file after every extract. PHP and Node
// instead use executable metadata: a catalog marker cannot verify which patch
// is actually installed in a managed runtime folder.
func (m *Manager) IsInstalled(s Source) bool {
	if s.Kind == KindPHP || s.Kind == KindNodejs {
		version, verified := installedRuntimeVersion(s)
		return verified && normalizeRuntimeVersion(version) == normalizeRuntimeVersion(s.Version)
	}
	if s.Kind == KindPgvector {
		return isPgvectorInstalled(s)
	}
	b, err := os.ReadFile(filepath.Join(s.InstallTo, markerFile))
	if err != nil {
		// Backwards-compat: if marker is missing but dir is populated AND this
		// kind uses a per-version subfolder (python), treat it as
		// installed so existing installs aren't lost. Shared-folder kinds get
		// the strict marker check (false until the next install rewrites it).
		if entries, e := os.ReadDir(s.InstallTo); e == nil && len(entries) > 0 {
			return strings.Contains(strings.ToLower(s.InstallTo), `\`+s.Version)
		}
		return false
	}
	return strings.TrimSpace(string(b)) == s.Version
}

func installedRuntimeVersion(s Source) (string, bool) {
	metadata, err := runtimeinventory.Read(filepath.Join(s.InstallTo, sentinels[s.Kind]))
	version := strings.TrimSpace(metadata.FullVersion)
	return version, err == nil && version != ""
}

func normalizeRuntimeVersion(version string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(version)), "v")
}

const markerFile = ".lara4ever-version"

// isPgvectorInstalled reports whether the pgvector extension files are present
// in the shared postgres tree AND the installed PostgreSQL major matches the
// entry's target major (pgvector builds are ABI-bound to one PG major).
// Version format for pgvector sources: "<extver>-pg<major>", e.g. "0.8.6-pg18".
func isPgvectorInstalled(s Source) bool {
	if _, err := os.Stat(filepath.Join(s.InstallTo, "share", "extension", "vector.control")); err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(s.InstallTo, markerFile))
	if err != nil {
		return false
	}
	pgVersion := strings.TrimSpace(string(b))
	major, _, _ := strings.Cut(pgVersion, ".")
	_, wantMajor, ok := strings.Cut(s.Version, "-pg")
	return ok && major == wantMajor
}

func writeMarker(installTo, version string) error {
	return os.WriteFile(filepath.Join(installTo, markerFile), []byte(version), 0644)
}

// sentinels lists the relative path of a "must exist" binary for each kind.
// Used by BackfillMarkers to detect installs that predate the marker file.
var sentinels = map[Kind]string{
	KindNginx:    "nginx.exe",
	KindApache:   `bin\httpd.exe`,
	KindMySQL:    `bin\mysqld.exe`,
	KindPostgres: `bin\postgres.exe`,
	KindMailpit:  "mailpit.exe",
	KindRedis:    "redis-server.exe",
	KindPHP:      "php.exe",
	KindPython:   "python.exe",
	KindNodejs:   "node.exe",
	KindCmder:    "Cmder.exe",
	KindComposer: "composer.phar",
}

// BackfillMarkers walks the catalog and, for any Source whose InstallTo
// contains the kind's sentinel binary but no marker file, writes one with the
// catalog version for legacy kinds. PHP and Node use only the actual runtime
// version and repair outdated markers. For shared folders the first catalog
// entry is claimed; Python entries each claim their own folder. Idempotent.
func BackfillMarkers(catalog []Source) {
	firstShared := map[Kind]Source{}
	perVersion := map[Kind]bool{
		KindPython: true,
	}

	for _, s := range catalog {
		sentinel, ok := sentinels[s.Kind]
		if !ok {
			continue
		}
		if s.Kind == KindPHP || s.Kind == KindNodejs {
			version, verified := installedRuntimeVersion(s)
			if verified {
				marker, err := os.ReadFile(filepath.Join(s.InstallTo, markerFile))
				if err != nil || strings.TrimSpace(string(marker)) != version {
					_ = writeMarker(s.InstallTo, version)
				}
			}
			continue
		}
		if perVersion[s.Kind] {
			// Per-version dir: claim each Source individually.
			marker := filepath.Join(s.InstallTo, markerFile)
			if _, err := os.Stat(marker); err == nil {
				continue
			}
			if _, err := os.Stat(filepath.Join(s.InstallTo, sentinel)); err != nil {
				continue
			}
			_ = writeMarker(s.InstallTo, s.Version)
			continue
		}
		// Shared dir: only claim the first catalog entry per kind.
		if _, seen := firstShared[s.Kind]; seen {
			continue
		}
		firstShared[s.Kind] = s
	}
	for _, s := range firstShared {
		marker := filepath.Join(s.InstallTo, markerFile)
		if _, err := os.Stat(marker); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.InstallTo, sentinels[s.Kind])); err != nil {
			continue
		}
		_ = writeMarker(s.InstallTo, s.Version)
	}
}

// InstallFromFile copies a user-picked zip into the cache and extracts it.
// Validates that the file is a real zip first.
func (m *Manager) InstallFromFile(s Source, srcPath string) error {
	m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "download", Message: i18n.T("backend.installer.copyingFile", nil)})
	if err := validateZip(srcPath); err != nil {
		err = i18n.Wrapf(err, "errors.installer.notAValidZip", nil)
		m.emitErr(s, err)
		return err
	}
	cachePath := filepath.Join(m.cacheDir, fmt.Sprintf("%s-%s.zip", s.Kind, s.Version))
	if err := copyFile(srcPath, cachePath); err != nil {
		m.emitErr(s, err)
		return err
	}
	m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "extract", Message: i18n.T("backend.installer.extracting", nil)})
	if err := m.extract(s, cachePath); err != nil {
		m.emitErr(s, err)
		return err
	}
	writeMarkerUnlessOverlay(s)
	m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "done", Percent: 100})
	return nil
}

// writeMarkerUnlessOverlay writes the version marker, except for kinds that
// extract INTO another kind's tree (pgvector → postgres): overwriting the
// host's marker would make the host report as not installed.
func writeMarkerUnlessOverlay(s Source) {
	if s.Kind == KindPgvector {
		return
	}
	_ = writeMarker(s.InstallTo, s.Version)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// Install downloads (with caching) and extracts a source.
func (m *Manager) Install(s Source) error {
	ext := ".zip"
	if s.Direct {
		// Preserve the URL's real extension (e.g. .phar) for the cache file.
		if u := filepath.Ext(s.URL); u != "" {
			ext = u
		} else {
			ext = ".bin"
		}
	}
	cachePath := filepath.Join(m.cacheDir, fmt.Sprintf("%s-%s%s", s.Kind, s.Version, ext))

	cacheValid := false
	if _, err := os.Stat(cachePath); err == nil {
		if s.Direct {
			cacheValid = true
		} else if vErr := validateZip(cachePath); vErr == nil {
			cacheValid = true
		} else if !s.Manual {
			// Cache corrompido (ex.: HTML salvo no lugar de zip). Apaga e refaz.
			// Para sources Manual, NÃO apaga — o usuário precisa repor o arquivo.
			_ = os.Remove(cachePath)
		} else {
			err := i18n.Errorf("errors.installer.cachedFileNotValidZip", map[string]any{"path": cachePath})
			m.emitErr(s, err)
			return err
		}
	}
	if cacheValid {
		m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "download",
			Percent: 100, Message: i18n.T("backend.installer.cacheHit", nil)})
	} else if s.Manual {
		err := i18n.Errorf("errors.installer.manualSourceMissing", map[string]any{"path": cachePath})
		m.emitErr(s, err)
		return err
	} else {
		if err := m.download(s, cachePath); err != nil {
			m.emitErr(s, err)
			return err
		}
	}

	if s.Direct {
		m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "extract", Message: i18n.T("backend.installer.installing", nil)})
		if err := m.installDirect(s, cachePath); err != nil {
			m.emitErr(s, err)
			return err
		}
	} else {
		m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "extract", Message: i18n.T("backend.installer.extracting", nil)})
		if err := m.extract(s, cachePath); err != nil {
			m.emitErr(s, err)
			return err
		}
	}
	writeMarkerUnlessOverlay(s)

	m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "done", Percent: 100})
	return nil
}

// installDirect copies the cached single-file payload into InstallTo (keeping
// the URL's basename) and writes any companion wrapper scripts. Used for
// sources flagged Direct (e.g. composer.phar).
func (m *Manager) installDirect(s Source, cachePath string) error {
	if err := os.MkdirAll(s.InstallTo, 0755); err != nil {
		return err
	}
	dst := filepath.Join(s.InstallTo, filepath.Base(s.URL))
	if err := copyFile(cachePath, dst); err != nil {
		return err
	}
	if s.Kind == KindComposer {
		if err := writeComposerWrappers(s.InstallTo); err != nil {
			return err
		}
	}
	return nil
}

// writeComposerWrappers drops a Windows .bat and a POSIX shell wrapper into
// the Composer install dir so users can type `composer ...` from any of the
// supported terminals (PowerShell, cmd, Git Bash, Cmder) once PHP is on PATH.
func writeComposerWrappers(installTo string) error {
	bat := "@echo off\r\nphp \"%~dp0composer.phar\" %*\r\n"
	if err := os.WriteFile(filepath.Join(installTo, "composer.bat"), []byte(bat), 0755); err != nil {
		return err
	}
	sh := "#!/bin/sh\ndir=$(dirname \"$0\")\nexec php \"$dir/composer.phar\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(installTo, "composer"), []byte(sh), 0755); err != nil {
		return err
	}
	return nil
}

func (m *Manager) emitErr(s Source, err error) {
	m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "error", Message: err.Error()})
}

func (m *Manager) download(s Source, dest string) error {
	urls := candidateURLs(s.URL)
	client := &http.Client{Timeout: 30 * time.Minute}

	var resp *http.Response
	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequest("GET", u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		// Mimic a browser — Apache Lounge & similar block bare clients.
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
		req.Header.Set("Accept", "application/octet-stream,application/zip,*/*")
		req.Header.Set("Referer", refererFor(u))
		r, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if r.StatusCode >= 400 {
			r.Body.Close()
			lastErr = i18n.Errorf("errors.installer.httpStatus", map[string]any{"status": r.StatusCode})
			continue
		}
		// Sanity: content-type should not be HTML (means we got an interstitial page).
		ct := r.Header.Get("Content-Type")
		if strings.Contains(strings.ToLower(ct), "text/html") {
			r.Body.Close()
			lastErr = i18n.Errorf("errors.installer.serverReturnedHtml", map[string]any{"contentType": ct})
			continue
		}
		resp = r
		break
	}
	if resp == nil {
		return i18n.Errorf("errors.installer.downloadFailed", map[string]any{"kind": s.Kind, "version": s.Version, "reason": downloadFailureReason(lastErr)})
	}
	defer resp.Body.Close()

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	total := resp.ContentLength
	pw := &progressWriter{
		total: total,
		emit: func(bytes int64, bps int64) {
			pct := 0
			if total > 0 {
				pct = int(bytes * 100 / total)
			}
			m.emit(Progress{
				Kind: s.Kind, Version: s.Version, Phase: "download",
				Bytes: bytes, Total: total, Percent: pct, BytesPerSec: bps,
			})
		},
	}
	if _, err := io.Copy(io.MultiWriter(out, pw), resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	if !s.Direct {
		// Validate that we got a real zip (PK\x03\x04 magic).
		if err := validateZip(tmp); err != nil {
			os.Remove(tmp)
			return i18n.Wrapf(err, "errors.installer.downloadedFileNotValidZip", nil)
		}
	}

	return os.Rename(tmp, dest)
}

func validateZip(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 4)
	if _, err := f.Read(head); err != nil {
		return err
	}
	if !(head[0] == 'P' && head[1] == 'K' && (head[2] == 0x03 || head[2] == 0x05 || head[2] == 0x07)) {
		return i18n.Errorf("errors.installer.notAZip", map[string]any{"magic": fmt.Sprintf("%x", head)})
	}
	return nil
}

func refererFor(u string) string {
	switch {
	case strings.Contains(u, "apachelounge.com"):
		return "https://www.apachelounge.com/download/"
	case strings.Contains(u, "windows.php.net"):
		return "https://windows.php.net/download/"
	case strings.Contains(u, "nginx.org"):
		return "https://nginx.org/en/download.html"
	case strings.Contains(u, "mysql.com"):
		return "https://dev.mysql.com/downloads/mysql/"
	case strings.Contains(u, "python.org"):
		return "https://www.python.org/downloads/windows/"
	}
	return ""
}

func (m *Manager) extract(s Source, archivePath string) error {
	return m.extractWithCatalogNotices(s, archivePath, catalogNoticeRegistry)
}

func (m *Manager) extractWithCatalogNotices(s Source, archivePath string, registry fs.FS) error {
	if err := os.MkdirAll(s.InstallTo, 0755); err != nil {
		return err
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(archive, info.Size())
	if err != nil {
		return err
	}

	// Detect a single top-level dir for optional flattening.
	var topPrefix string
	if s.Flatten {
		topPrefix = detectSingleTopDir(zr.File)
	}
	// Upstream payloads must not replace retained notices, including Windows
	// case/terminal-dot aliases. Inspect the entire archive before any writes.
	for _, file := range zr.File {
		name := strings.TrimPrefix(file.Name, topPrefix)
		if reservedLegalPath(name) {
			return fmt.Errorf("archive contains reserved legal output path: %q", file.Name)
		}
	}
	if err := preserveCatalogLegal(s, io.NewSectionReader(archive, 0, info.Size()), registry); err != nil {
		return err
	}
	if err := preserveSupplementalLegal(s); err != nil {
		return err
	}

	for _, f := range zr.File {
		name := f.Name
		if topPrefix != "" {
			name = strings.TrimPrefix(name, topPrefix)
			if name == "" {
				continue
			}
		}
		target := filepath.Join(s.InstallTo, name)
		// Guard against zip-slip
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(s.InstallTo)+string(os.PathSeparator)) &&
			filepath.Clean(target) != filepath.Clean(s.InstallTo) {
			return i18n.Errorf("errors.installer.unsafePathInArchive", map[string]any{"name": f.Name})
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		out.Close()
	}
	return nil
}

// FetchBundledExtension extracts a single extension DLL out of the PHP zip
// described by `s` and copies it into `destExtDir` (typically the install's
// `ext/` folder). Reuses the cache: if the original install zip is still
// present, no network call happens. Otherwise downloads it once and caches it
// for subsequent extension fetches.
//
// Returns a clear error if the requested DLL isn't bundled in the zip — the
// extension is most likely a PECL one (xdebug, redis, imagick, …) and a
// different code path will be needed for those.
//
// extName is the bare extension name as shown in the UI (e.g. "bcmath",
// "gd"). The matching zip entry is `ext/php_<extName>.dll` (case-insensitive).
func (m *Manager) FetchBundledExtension(s Source, extName, destExtDir string) error {
	cachePath := filepath.Join(m.cacheDir, fmt.Sprintf("%s-%s.zip", s.Kind, s.Version))

	cacheValid := false
	if _, err := os.Stat(cachePath); err == nil {
		if vErr := validateZip(cachePath); vErr == nil {
			cacheValid = true
		} else {
			_ = os.Remove(cachePath)
		}
	}
	if !cacheValid {
		if err := m.download(s, cachePath); err != nil {
			m.emitErr(s, err)
			return err
		}
	} else {
		m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "download",
			Percent: 100, Message: i18n.T("backend.installer.cacheHit", nil)})
	}

	m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "extract",
		Message: i18n.T("backend.installer.extractingDll", map[string]any{"name": extName})})

	zr, err := zip.OpenReader(cachePath)
	if err != nil {
		m.emitErr(s, err)
		return err
	}
	defer zr.Close()

	wantLower := strings.ToLower("ext/php_" + extName + ".dll")
	if err := os.MkdirAll(destExtDir, 0755); err != nil {
		m.emitErr(s, err)
		return err
	}

	for _, f := range zr.File {
		// Zip entries from windows.php.net always use forward slashes.
		if strings.ToLower(f.Name) != wantLower {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			m.emitErr(s, err)
			return err
		}
		dst := filepath.Join(destExtDir, "php_"+extName+".dll")
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			m.emitErr(s, err)
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			os.Remove(dst)
			m.emitErr(s, err)
			return err
		}
		rc.Close()
		out.Close()
		m.emit(Progress{Kind: s.Kind, Version: s.Version, Phase: "done", Percent: 100,
			Message: extName + ".dll installed"})
		return nil
	}

	err = i18n.Errorf("errors.installer.extensionNotBundled", map[string]any{"name": extName, "version": s.Version})
	m.emitErr(s, err)
	return err
}

// InstallPecl downloads and installs a PECL extension for the given PHP
// install. extName is the bare extension name (e.g. "xdebug"). On success
// `php_<extName>.dll` lands in <phpVersionDir>/ext/ and any companion runtime
// DLLs (libssh2, ImageMagick, …) land next to php.exe.
//
// Progress events use Kind=KindPeclExt and Version=extName so UI subscribers
// can key by extension name (each PECL extension has its own progress lane).
// Idempotent on success — re-installing an existing version overwrites the
// DLLs in place.
func (m *Manager) InstallPecl(extName, phpVersion, phpVersionDir string) error {
	series := PeclSeries(phpVersion)
	extDir := filepath.Join(phpVersionDir, "ext")
	src, _, ok := PeclSourceFor(extName, phpVersion, extDir)
	if !ok {
		err := i18n.Errorf("errors.installer.noPeclBuildPinned", map[string]any{"name": extName, "series": series})
		m.emit(Progress{Kind: KindPeclExt, Version: extName, Phase: "error", Message: err.Error()})
		return err
	}

	cachePath := filepath.Join(m.cacheDir, fmt.Sprintf("pecl-%s-%s-%s.zip", extName, series, src.Version))
	cacheValid := false
	if _, err := os.Stat(cachePath); err == nil {
		if vErr := validateZip(cachePath); vErr == nil {
			cacheValid = true
		} else {
			_ = os.Remove(cachePath)
		}
	}
	if cacheValid {
		m.emit(Progress{Kind: KindPeclExt, Version: extName, Phase: "download",
			Percent: 100, Message: i18n.T("backend.installer.cacheHit", nil)})
	} else {
		// Reuse the standard download() — it emits Progress with the source's
		// Kind/Version, which we set to (KindPeclExt, extName) so the UI keys
		// progress per-extension.
		dl := Source{Kind: KindPeclExt, Version: extName, URL: src.URL}
		if err := m.download(dl, cachePath); err != nil {
			m.emit(Progress{Kind: KindPeclExt, Version: extName, Phase: "error", Message: err.Error()})
			return err
		}
	}

	m.emit(Progress{Kind: KindPeclExt, Version: extName, Phase: "extract", Message: i18n.T("backend.installer.installing", nil)})
	if err := extractPeclZip(cachePath, extName, extDir, phpVersionDir); err != nil {
		m.emit(Progress{Kind: KindPeclExt, Version: extName, Phase: "error", Message: err.Error()})
		return err
	}

	m.emit(Progress{Kind: KindPeclExt, Version: extName, Phase: "done", Percent: 100,
		Message: extName + " " + src.Version + " installed"})
	return nil
}

// extractPeclZip walks a PECL zip and copies:
//   - php_<extName>.dll → extDir
//   - any other top-level *.dll → phpVersionDir (companion runtime libs:
//     libssh2.dll for ssh2, libmemcached.dll for memcached, ImageMagick's
//     CORE_RL_*.dll / IM_MOD_*.dll for imagick).
//
// Legal documents are retained separately with an archive fingerprint, including
// nested companion notices. Other nested payloads and .lib/.exp/.pdb are ignored.
func extractPeclZip(cachePath, extName, extDir, phpVersionDir string) error {
	archive, err := os.Open(cachePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(archive, info.Size())
	if err != nil {
		return err
	}
	if err := preservePeclLegal(zr.File, io.NewSectionReader(archive, 0, info.Size()), extName, phpVersionDir); err != nil {
		return err
	}

	if err := os.MkdirAll(extDir, 0755); err != nil {
		return err
	}

	primary := strings.ToLower("php_" + extName + ".dll")
	foundPrimary := false

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// Only top-level entries — skip anything nested in a subdir.
		if strings.Contains(filepath.ToSlash(f.Name), "/") {
			continue
		}
		base := strings.ToLower(filepath.Base(f.Name))
		if !strings.HasSuffix(base, ".dll") {
			continue
		}

		var dst string
		if base == primary {
			dst = filepath.Join(extDir, "php_"+extName+".dll")
			foundPrimary = true
		} else {
			dst = filepath.Join(phpVersionDir, filepath.Base(f.Name))
		}
		if err := copyZipEntry(f, dst); err != nil {
			return err
		}
	}

	if !foundPrimary {
		return i18n.Errorf("errors.installer.peclZipMissingDll", map[string]any{"name": extName})
	}
	return nil
}

func copyZipEntry(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, rc); err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

// EnsureBundledExtensions guarantees the named extension DLLs are present in
// destExtDir for the PHP source `s`. Skips DLLs already on disk and silently
// skips names not bundled in the zip (caller decides whether that's an
// error). Reuses the cached PHP zip; opens it once and extracts every
// missing DLL in a single pass.
//
// Used as a safety net right after PHP install: even though extract() copies
// the entire zip (including ext/), this defends against partial extracts,
// user-deleted DLLs, and any future "lite" install that skips ext/.
func (m *Manager) EnsureBundledExtensions(s Source, names []string, destExtDir string) error {
	missing := make([]string, 0, len(names))
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(destExtDir, "php_"+n+".dll")); err != nil {
			missing = append(missing, n)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	cachePath := filepath.Join(m.cacheDir, fmt.Sprintf("%s-%s.zip", s.Kind, s.Version))
	if _, err := os.Stat(cachePath); err != nil {
		// No cache — without the zip we can't restore DLLs without re-downloading
		// the whole thing. Caller (post-install hook) just ran extract from this
		// same cache, so this branch only triggers if the cache was wiped between
		// install and our call. Safe to no-op; UI can re-fetch on demand.
		return nil
	}
	if vErr := validateZip(cachePath); vErr != nil {
		return nil
	}

	zr, err := zip.OpenReader(cachePath)
	if err != nil {
		return err
	}
	defer zr.Close()

	if err := os.MkdirAll(destExtDir, 0755); err != nil {
		return err
	}

	want := make(map[string]string, len(missing)) // lowercased zip path → bare ext name
	for _, n := range missing {
		want[strings.ToLower("ext/php_"+n+".dll")] = n
	}

	for _, f := range zr.File {
		extName, ok := want[strings.ToLower(f.Name)]
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		dst := filepath.Join(destExtDir, "php_"+extName+".dll")
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			os.Remove(dst)
			return err
		}
		rc.Close()
		out.Close()
	}
	return nil
}

// candidateURLs returns the primary URL plus mirror/fallback URLs to try.
// For windows.php.net /releases/ we also try /releases/archives/ (and vice-versa)
// because PHP migrates older patch versions to /archives/ when new ones ship.
func candidateURLs(primary string) []string {
	out := []string{primary}
	const rel = "windows.php.net/downloads/releases/"
	const arc = "windows.php.net/downloads/releases/archives/"
	if strings.Contains(primary, arc) {
		// archive → also try the live releases dir
		out = append(out, strings.Replace(primary, arc, rel, 1))
	} else if strings.Contains(primary, rel) {
		// live releases → also try archives
		out = append(out, strings.Replace(primary, rel, arc, 1))
	}
	return out
}

func detectSingleTopDir(files []*zip.File) string {
	tops := make(map[string]struct{})
	for _, f := range files {
		// Skip empty and macOS junk.
		if f.Name == "" || strings.HasPrefix(f.Name, "__MACOSX/") {
			continue
		}
		idx := strings.Index(f.Name, "/")
		if idx <= 0 {
			return "" // file at root → no flatten
		}
		tops[f.Name[:idx+1]] = struct{}{}
		if len(tops) > 1 {
			return ""
		}
	}
	for k := range tops {
		return k
	}
	return ""
}

type progressWriter struct {
	bytes     atomic.Int64
	total     int64
	last      time.Time
	lastBytes int64
	emit      func(bytes int64, bps int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	cur := p.bytes.Add(int64(n))
	now := time.Now()
	if p.last.IsZero() {
		p.last = now
		p.lastBytes = cur
	}
	if now.Sub(p.last) >= 250*time.Millisecond {
		dur := now.Sub(p.last).Seconds()
		bps := int64(float64(cur-p.lastBytes) / dur)
		p.emit(cur, bps)
		p.last = now
		p.lastBytes = cur
	}
	return n, nil
}

// downloadFailureReason turns a transport error into something a user can act
// on. Go wraps HTTP failures in *url.Error, which carries the URL it was
// fetching — and for a GitHub release that is a signed asset URL running to
// several hundred characters. Reported verbatim, the useful part ("no such
// host") ends up buried past a wall of query parameters, and the reader has
// no way to tell a network blip from a broken catalog entry.
//
// The URL still belongs in the log; it does not belong in the sentence the
// user reads.
//
// errors.As interaction (i18n conversion note): the only errors ever passed
// in here are lastErr from download()'s mirror loop — either a raw error
// from http.NewRequest/client.Do (untouched, never an i18n.Error), or one of
// this file's own i18n.Errorf("errors.installer.httpStatus", ...) /
// i18n.Errorf("errors.installer.serverReturnedHtml", ...) values, neither of
// which was ever wrapped via %w even before this package used i18n. So the
// *url.Error/*net.OpError/*net.DNSError errors.As checks below only ever see
// genuine stdlib network errors, never a translated *i18n.Error — converting
// this package's fmt.Errorf calls to i18n.Errorf/Wrapf does not change what
// reaches them.
// i18n conversion note: this returns a plain string (fmt.Sprintf, never
// fmt.Errorf) that download() embeds as the {{reason}} var of
// errors.installer.downloadFailed — so the prose itself is what needed
// translating, not an error wrapper. i18n.T is used instead of i18n.Errorf
// because this is not itself the error value.
func downloadFailureReason(err error) string {
	if err == nil {
		return i18n.T("errors.installer.downloadReasonUnknown", nil)
	}

	// Unwrap the URL wrapper, which is where the long signed link lives.
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		err = urlErr.Err
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return i18n.T("errors.installer.downloadReasonDnsFailed", map[string]any{"host": dnsErr.Name})
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return i18n.T("errors.installer.downloadReasonTimeout", nil)
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return i18n.T("errors.installer.downloadReasonNetworkFailed", map[string]any{"error": opErr.Err})
	}

	return err.Error()
}
