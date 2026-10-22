// Package updater provides in-app self-update for the LaraEnv client.
// It checks GitHub Releases on a public repo, downloads the new MSI, verifies
// it against a sha256 sidecar, and hands off to msiexec to apply the upgrade.
//
// An installed copy keeps user data outside the install root (see
// internal/paths), so the WiX MajorUpgrade is non-destructive. A portable
// copy is the opposite: the environment lives in the folder holding the
// executable, the installer would create a second install pointing at an
// empty default root, and the upgrade instead replaces the executable in
// place. Manager.portable decides which.
package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lara4ever/internal/i18n"
	"lara4ever/internal/paths"
)

// Phase describes where we are in the update flow.
const (
	PhaseDownload = "download"
	PhaseVerify   = "verify"
	PhaseReady    = "ready"
	PhaseError    = "error"
)

// Release describes one version fetched from GitHub Releases.
type Release struct {
	Version     string `json:"version"`     // "0.2.0", leading "v" stripped
	Notes       string `json:"notes"`       // release.body (markdown)
	PublishedAt string `json:"publishedAt"` // RFC3339
	MsiName     string `json:"msiName"`
	MsiURL      string `json:"msiUrl"`
	Sha256URL   string `json:"sha256Url"`
	SizeBytes   int64  `json:"sizeBytes"`

	// Portable distributions update by replacing the executable, so the
	// release has to carry it separately from the installer.
	ExeName      string `json:"exeName"`
	ExeURL       string `json:"exeUrl"`
	ExeSha256URL string `json:"exeSha256Url"`
	ExeSizeBytes int64  `json:"exeSizeBytes"`
}

// asset is the download chosen for the running distribution.
type asset struct {
	url    string
	name   string
	shaURL string
	size   int64
}

// assetFor picks the download that matches how this copy was installed.
//
// The portable branch is strict about the checksum: the update replaces the
// running executable, and there is no second line of defence the way msiexec
// has its own integrity checks. The installer branch stays tolerant because
// releases published before the sidecar existed are still valid upgrades.
func (r Release) assetFor(portable bool) (asset, error) {
	if portable {
		if r.ExeURL == "" {
			return asset{}, i18n.Errorf("errors.updater.noExeAsset", map[string]any{"version": r.Version})
		}
		if r.ExeSha256URL == "" {
			return asset{}, i18n.Errorf("errors.updater.noChecksumForExe", map[string]any{"version": r.Version, "exeName": r.ExeName})
		}
		return asset{url: r.ExeURL, name: r.ExeName, shaURL: r.ExeSha256URL, size: r.ExeSizeBytes}, nil
	}
	if r.MsiURL == "" {
		return asset{}, i18n.Errorf("errors.updater.noMsiAsset", map[string]any{"version": r.Version})
	}
	return asset{url: r.MsiURL, name: r.MsiName, shaURL: r.Sha256URL, size: r.SizeBytes}, nil
}

// Progress is emitted periodically during download/verify.
type Progress struct {
	Phase       string `json:"phase"`
	Version     string `json:"version"`
	Bytes       int64  `json:"bytes"`
	Total       int64  `json:"total"`
	Percent     int    `json:"percent"`
	BytesPerSec int64  `json:"bytesPerSec"`
	Path        string `json:"path,omitempty"`    // local MSI path on PhaseReady
	Message     string `json:"message,omitempty"` // human text (errors, etc.)
}

// Manager handles the full update flow.
type Manager struct {
	cacheDir       string
	currentVersion string
	portable       bool
	client         *http.Client

	// locateExe finds the running executable. A seam so the portable guard in
	// Apply can be exercised without moving the test binary.
	locateExe func() (string, error)

	mu   sync.RWMutex
	emit func(Progress)
}

// NewManager prepares the cache dir and returns a manager. emit may be nil at
// construction time and set later via SetEmit (we wire it in app.go's startup
// once the Wails ctx is available).
func NewManager(cacheDir, currentVersion string, portable bool, emit func(Progress)) *Manager {
	if cacheDir == "" {
		cacheDir = filepath.Join(os.TempDir(), "laraenv-updates")
	}
	_ = os.MkdirAll(cacheDir, 0755)
	return &Manager{
		cacheDir:       cacheDir,
		currentVersion: currentVersion,
		portable:       portable,
		client:         &http.Client{Timeout: 30 * time.Minute},
		locateExe:      os.Executable,
		emit:           emit,
	}
}

func (m *Manager) SetEmit(fn func(Progress)) {
	m.mu.Lock()
	m.emit = fn
	m.mu.Unlock()
}

func (m *Manager) emitProgress(p Progress) {
	m.mu.RLock()
	fn := m.emit
	m.mu.RUnlock()
	if fn != nil {
		fn(p)
	}
}

// CurrentVersion returns the version this binary was built with.
func (m *Manager) CurrentVersion() string { return m.currentVersion }

// IsPortable reports how this copy will update: a portable one replaces its
// own executable, an installed one runs the installer. The UI says which, so
// that picking the wrong one is visible before the user commits to it.
func (m *Manager) IsPortable() bool { return m.portable }

// Check fetches the latest release. Caller decides if it's newer than current.
//
// A release that carries nothing this distribution can install is reported as
// an error here rather than at download time, so the UI never offers an
// upgrade it cannot actually apply.
func (m *Manager) Check(ctx context.Context) (*Release, error) {
	rel, err := fetchLatestRelease(ctx, m.client)
	if err != nil {
		return nil, err
	}
	if _, err := rel.assetFor(m.portable); err != nil {
		return nil, err
	}
	return rel, nil
}

// IsNewer returns true if r.Version is strictly greater than the current
// version. Both are expected to be dotted semver-ish (e.g. "0.2.0", "1.0.0").
// Pre-release suffixes (like "-rc1") are treated as equal to the base.
func (m *Manager) IsNewer(r *Release) bool {
	if r == nil || r.Version == "" {
		return false
	}
	return cmpVersion(r.Version, m.currentVersion) > 0
}

// Download streams the MSI to the cache dir and verifies the sha256 sidecar
// (when present). Returns the local path to the verified file.
//
// Emits PhaseDownload progress every ~250ms, then PhaseVerify, then PhaseReady
// with Path set to the cached MSI path. Errors emit PhaseError.
func (m *Manager) Download(ctx context.Context, r *Release) (string, error) {
	if r == nil {
		err := i18n.Errorf("errors.updater.noReleaseToDownload", nil)
		m.emitProgress(Progress{Phase: PhaseError, Message: err.Error()})
		return "", err
	}
	a, err := r.assetFor(m.portable)
	if err != nil {
		m.emitProgress(Progress{Phase: PhaseError, Version: r.Version, Message: err.Error()})
		return "", err
	}

	name := a.name
	if name == "" {
		name = fmt.Sprintf("LaraEnv-%s.msi", r.Version)
	}
	dest := filepath.Join(m.cacheDir, name)

	// Fetch the expected digest first. assetFor already refused a portable
	// upgrade without a sidecar, so an empty digest here can only be an
	// installer upgrade from a release that predates them.
	expected, _ := fetchSha256(ctx, m.client, a.shaURL)

	if err := m.streamDownload(ctx, a, r.Version, dest); err != nil {
		m.emitProgress(Progress{Phase: PhaseError, Version: r.Version, Message: err.Error()})
		return "", err
	}

	m.emitProgress(Progress{Phase: PhaseVerify, Version: r.Version, Message: i18n.T("backend.updater.verifyingIntegrity", nil)})
	if expected != "" {
		got, err := sha256File(dest)
		if err != nil {
			m.emitProgress(Progress{Phase: PhaseError, Version: r.Version, Message: i18n.T("backend.updater.hashFailed", map[string]any{"error": err.Error()})})
			return "", err
		}
		if got != expected {
			_ = os.Remove(dest)
			err := i18n.Errorf("errors.updater.checksumMismatch", map[string]any{"expected": expected[:12], "got": got[:12]})
			m.emitProgress(Progress{Phase: PhaseError, Version: r.Version, Message: err.Error()})
			return "", err
		}
	}

	m.emitProgress(Progress{
		Phase: PhaseReady, Version: r.Version, Percent: 100,
		Bytes: a.size, Total: a.size, Path: dest,
	})
	return dest, nil
}

func (m *Manager) streamDownload(ctx context.Context, a asset, version, dest string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", a.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LaraEnv-Updater")
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return i18n.Errorf("errors.updater.httpErrorDownloading", map[string]any{"status": resp.StatusCode, "name": a.name})
	}

	tmp := dest + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}

	total := resp.ContentLength
	if total <= 0 {
		total = a.size
	}

	pw := &progressWriter{
		total: total,
		emit: func(bytes, bps int64) {
			pct := 0
			if total > 0 {
				pct = int(bytes * 100 / total)
			}
			m.emitProgress(Progress{
				Phase: PhaseDownload, Version: version,
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
	return os.Rename(tmp, dest)
}

// Apply hands off to msiexec asynchronously. Caller must quit the app right
// after a successful call so MajorUpgrade can replace files in Program Files.
// Implementation is OS-specific (see apply_windows.go / apply_other.go).
//
// Before reaching the installer it re-derives portability from the marker
// beside the running executable rather than trusting the flag it was
// constructed with. The two agree by construction; when they did not, 0.4.35
// through 0.4.40 ran the installer over portable copies and left a second
// install resolving to an empty default root, with the user's PHP versions
// and databases on disk but invisible. A refusal is recoverable where that
// was not, so a disagreement stops here.
func (m *Manager) Apply(downloaded string) error {
	if _, err := os.Stat(downloaded); err != nil {
		return i18n.Wrapf(err, "errors.updater.updateNotFound", map[string]any{"path": downloaded})
	}
	target, err := m.locateExe()
	if err != nil {
		return i18n.Wrapf(err, "errors.updater.locateExecutableFailed", nil)
	}
	if m.portable {
		return applySwap(target, downloaded, filepath.Join(filepath.Dir(downloaded), "update.log"))
	}
	if dir := filepath.Dir(target); paths.IsPortableDir(dir) {
		return i18n.Errorf("errors.updater.installerOverPortable", map[string]any{"dir": dir})
	}
	return apply(downloaded)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// cmpVersion compares dotted version strings ("0.2.0" vs "0.10.1"). Returns
// >0 if a is newer, <0 if older, 0 if equal. Non-numeric segments compare as 0
// so "0.2.0-rc1" == "0.2.0" for our purposes.
func cmpVersion(a, b string) int {
	pa := splitVersion(a)
	pb := splitVersion(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		ai, bi := 0, 0
		if i < len(pa) {
			ai = pa[i]
		}
		if i < len(pb) {
			bi = pb[i]
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}

func splitVersion(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	// Drop pre-release suffix: "0.2.0-rc1" → "0.2.0"
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

// progressWriter mirrors the throttled emitter from internal/installer.
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
