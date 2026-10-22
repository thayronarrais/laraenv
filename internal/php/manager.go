package php

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"lara4ever/internal/i18n"
)

// createNoWindow suppresses the conhost flash for short-lived helper spawns
// (mklink for the "current" junction). 0x08000000 == CREATE_NO_WINDOW.
const createNoWindow = 0x08000000

type Version struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Active  bool   `json:"active"`
}

// PortFor returns the FastCGI port for a given PHP major.minor version folder.
// Convention: 9000 + (major*10 + minor). Examples:
//   "8.4"     → 9084
//   "8.1"     → 9081
//   "7.4.33"  → 9074  (only major.minor are used)
//   "5.6"     → 9056
func PortFor(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return 9000
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major == 0 {
		return 9000
	}
	return 9000 + major*10 + minor
}

// ReplicaPort returns the FastCGI port for worker replica i of a version.
// Worker 0 uses the base PortFor(version); each further worker adds a stride of
// 100, so versions never collide (8.1: 9081/9181/9281/9381; 8.4: 9084/9184/…).
// The stride comfortably exceeds any sane worker count, and the base port's low
// two digits keep versions apart.
func ReplicaPort(version string, i int) int {
	return PortFor(version) + i*100
}

// UpstreamName returns the nginx upstream block name for a version's worker
// pool: major.minor with dots replaced by underscores, prefixed "php_"
// (e.g. "8.1" → "php_8_1"). nginx upstream names can't contain dots.
func UpstreamName(version string) string {
	parts := strings.Split(version, ".")
	mm := version
	if len(parts) >= 2 {
		mm = parts[0] + "_" + parts[1]
	}
	return "php_" + strings.ReplaceAll(mm, ".", "_")
}

// CgiBinary returns the absolute path to php-cgi.exe for a version folder
// (e.g. "8.4" → C:\lara4ever\php\8.4\php-cgi.exe).
func (m *Manager) CgiBinary(version string) string {
	return filepath.Join(m.root, version, "php-cgi.exe")
}

type Manager struct{ root string }

func NewManager(root string) *Manager { return &Manager{root: root} }

func (m *Manager) List() ([]Version, error) {
	if _, err := os.Stat(m.root); os.IsNotExist(err) {
		return []Version{}, nil
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil, err
	}
	currentLink := filepath.Join(m.root, "current")
	currentTarget, _ := os.Readlink(currentLink)
	if currentTarget != "" && !filepath.IsAbs(currentTarget) {
		currentTarget = filepath.Join(m.root, currentTarget)
	}

	out := make([]Version, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "current" {
			continue
		}
		p := filepath.Join(m.root, e.Name())
		// Skip incomplete/stale install dirs — a folder without php.exe is
		// not a usable PHP. Hides leftover directories from interrupted
		// installs (where a previous catalog had the patch version as the
		// folder name) so the picker doesn't offer broken options.
		if _, err := os.Stat(filepath.Join(p, "php.exe")); err != nil {
			continue
		}
		out = append(out, Version{
			Version: e.Name(),
			Path:    p,
			Active:  p == currentTarget,
		})
	}
	return out, nil
}

// ActiveExe returns the absolute path to the currently active php.exe, or ""
// if no version is selected. Always ensures the version has a usable php.ini
// (created from the bundled template + common extensions enabled).
func (m *Manager) ActiveExe() string {
	p, err := m.ActivePath()
	if err != nil || p == "" {
		return ""
	}
	exe := filepath.Join(p, "php.exe")
	if _, err := os.Stat(exe); err != nil {
		return ""
	}
	_ = EnsureIni(p)
	return exe
}

// ExeFor returns the absolute path to php.exe for a specific installed
// version. Empty version falls back to ActiveExe (so callers can pass "" to
// mean "whatever is active"). Returns "" if the version isn't installed or
// the binary is missing. Ensures php.ini before returning.
func (m *Manager) ExeFor(version string) string {
	if version == "" {
		return m.ActiveExe()
	}
	dir := filepath.Join(m.root, version)
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	exe := filepath.Join(dir, "php.exe")
	if _, err := os.Stat(exe); err != nil {
		return ""
	}
	_ = EnsureIni(dir)
	return exe
}

// Common extensions to enable in a fresh php.ini. Composer needs openssl;
// most modern web apps need the rest. Kept in sync with the eager-fetch
// list in app.go (refreshPhpFpmServices flow) so toggling them in the UI
// never hits a missing-DLL error on a fresh install.
var defaultExts = []string{
	"openssl", "curl", "mbstring", "fileinfo", "intl", "gd",
	"pdo_mysql", "pdo_pgsql", "pdo_sqlite", "sqlite3",
	"zip", "exif", "sockets", "soap", "bcmath", "gmp",
	"sodium", "xsl",
}

// EnsureIni guarantees the PHP install at versionDir has a php.ini with sane
// defaults — copies from php.ini-development if missing, then uncomments the
// common extensions and sets extension_dir = "ext".
// Idempotent: safe to call repeatedly.
func EnsureIni(versionDir string) error {
	ini := filepath.Join(versionDir, "php.ini")
	if _, err := os.Stat(ini); err != nil {
		template := filepath.Join(versionDir, "php.ini-development")
		if _, err := os.Stat(template); err != nil {
			template = filepath.Join(versionDir, "php.ini-production")
		}
		if _, err := os.Stat(template); err != nil {
			return nil // nothing to seed from; older PHP zips may differ
		}
		b, err := os.ReadFile(template)
		if err != nil {
			return err
		}
		if err := os.WriteFile(ini, b, 0644); err != nil {
			return err
		}
	}

	// Patch the ini: uncomment defaults and set extension_dir.
	b, err := os.ReadFile(ini)
	if err != nil {
		return err
	}
	content := string(b)
	changed := false

	// extension_dir must be ABSOLUTE: PHP on Windows resolves a relative
	// `extension_dir = "ext"` against the current working directory, not the
	// PHP install dir. So `php` invoked from a project folder fails to load
	// every extension with "tried: ext\php_<name>.dll (not found)" even
	// though the DLLs are present in the install. Forward slashes work fine
	// in php.ini and avoid escape ambiguities.
	desiredExtDir := strings.ReplaceAll(filepath.Join(versionDir, "ext"), `\`, `/`)
	desiredLine := `extension_dir = "` + desiredExtDir + `"`
	reAlready := regexp.MustCompile(`(?m)^\s*extension_dir\s*=\s*"` + regexp.QuoteMeta(desiredExtDir) + `"`)
	if !reAlready.MatchString(content) {
		// Replace any existing active extension_dir line (relative or stale absolute).
		reActive := regexp.MustCompile(`(?m)^\s*extension_dir\s*=\s*"[^"]*"`)
		if reActive.MatchString(content) {
			content = reActive.ReplaceAllString(content, desiredLine)
		} else {
			// Try to uncomment the canonical commented form.
			reCommented := regexp.MustCompile(`(?m)^\s*;\s*extension_dir\s*=\s*"ext"`)
			if reCommented.MatchString(content) {
				content = reCommented.ReplaceAllString(content, desiredLine)
			} else {
				if !strings.HasSuffix(content, "\n") {
					content += "\n"
				}
				content += desiredLine + "\n"
			}
		}
		changed = true
	}

	// Build the set of extensions that actually have a loadable DLL in ext/.
	// On Windows an `extension=<x>` line is only meaningful when php_<x>.dll
	// exists: statically-compiled built-ins (bcmath, ctype, …) ship inside
	// php.exe with no DLL, and a genuinely missing DLL can never load. In both
	// cases an active line just emits a startup warning on every invocation
	// without changing behaviour. Gating on DLL presence is the single rule
	// that keeps the ini warning-free by construction.
	hasDLL := map[string]bool{}
	if entries, err := os.ReadDir(filepath.Join(versionDir, "ext")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := strings.ToLower(e.Name())
			if !strings.HasSuffix(n, ".dll") {
				continue
			}
			n = strings.TrimSuffix(n, ".dll")
			n = strings.TrimPrefix(n, "php_")
			hasDLL[n] = true
		}
	}

	for _, ext := range defaultExts {
		// Only enable an extension we can actually load. Built-ins (bcmath) and
		// versions whose zip ships no DLL are skipped — enabling them would only
		// produce "Unable to load dynamic library" on startup.
		if !hasDLL[strings.ToLower(ext)] {
			continue
		}
		// Match `;extension=openssl` (with optional whitespace) and uncomment.
		re := regexp.MustCompile(`(?m)^\s*;\s*extension\s*=\s*` + regexp.QuoteMeta(ext) + `\s*$`)
		if re.MatchString(content) {
			content = re.ReplaceAllString(content, "extension="+ext)
			changed = true
			continue
		}
		// Already uncommented? Skip.
		if regexp.MustCompile(`(?m)^\s*extension\s*=\s*` + regexp.QuoteMeta(ext) + `\s*$`).MatchString(content) {
			continue
		}
		// Not present at all — append.
		if !strings.Contains(content, "\nextension="+ext+"\n") {
			content += "\nextension=" + ext + "\n"
			changed = true
		}
	}

	// Self-heal existing inis: comment out any active extension/zend_extension
	// line that cannot load (no DLL in ext/) and drop duplicate active lines.
	// Both are the exact sources of the PHP startup warnings, and commenting a
	// built-in like bcmath is safe — the functions remain available. Idempotent:
	// once prefixed with ';' the line no longer matches reExtLine.
	reExtLine := regexp.MustCompile(`^(\s*)((?:zend_)?extension)\s*=\s*([a-zA-Z0-9_]+)\s*$`)
	// Unix `.so` paths (e.g. the template's `extension=/path/to/extension/mysqli.so`
	// documentation example, left active by older PHP zips) can never load on
	// Windows. A real Windows path ends in .dll, so this only catches dead lines.
	reSoLine := regexp.MustCompile(`^(\s*)(?:zend_)?extension\s*=\s*"?[^"\r\n]*\.so"?\s*$`)
	seenActive := map[string]bool{}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if m := reSoLine.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + ";" + strings.TrimSpace(line) + "   ; lara4ever: Unix .so path, not loadable on Windows — commented to avoid startup warning"
			changed = true
			continue
		}
		m := reExtLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.ToLower(m[3])
		key := m[2] + "=" + name
		switch {
		case !hasDLL[name]:
			lines[i] = m[1] + ";" + strings.TrimSpace(line) + "   ; lara4ever: no DLL in ext/ (built-in or missing) — commented to avoid startup warning"
			changed = true
		case seenActive[key]:
			lines[i] = m[1] + ";" + strings.TrimSpace(line) + "   ; lara4ever: duplicate — already loaded above"
			changed = true
		default:
			seenActive[key] = true
		}
	}
	content = strings.Join(lines, "\n")

	if changed {
		return os.WriteFile(ini, []byte(content), 0644)
	}
	return nil
}

// PhpTuningDefaults returns the upload/limit defaults applied when a field is
// left empty/zero. Generous on purpose so large media uploads (hundreds of MB)
// work out of the box without the user touching php.ini. Centralised so the
// apply path, the worker-pool size and the UI hint text agree.
//   uploadMax, postMax, memLimit are php.ini size strings ("512M");
//   maxExec, maxInput are seconds; workers is the php-cgi pool size.
func PhpTuningDefaults() (uploadMax, postMax, memLimit string, maxExec, maxInput, workers int) {
	return "512M", "512M", "512M", 300, 300, 4
}

// EnsurePhpTuning sets the upload/runtime limit directives in the php.ini at
// versionDir, overriding whatever the bundled php.ini ships with (which varies
// wildly per version — e.g. post_max_size=8M, upload_max_filesize=2M on 8.1).
// Empty size strings / non-positive seconds fall back to PhpTuningDefaults.
// Idempotent: rewrites each directive in place (active or commented) and writes
// back only when something changed, so repeated startup calls are byte-stable.
func EnsurePhpTuning(versionDir, uploadMax, postMax, memLimit string, maxExec, maxInput int) error {
	dUpload, dPost, dMem, dExec, dInput, _ := PhpTuningDefaults()
	if strings.TrimSpace(uploadMax) == "" {
		uploadMax = dUpload
	}
	if strings.TrimSpace(postMax) == "" {
		postMax = dPost
	}
	if strings.TrimSpace(memLimit) == "" {
		memLimit = dMem
	}
	if maxExec <= 0 {
		maxExec = dExec
	}
	if maxInput <= 0 {
		maxInput = dInput
	}

	ini := filepath.Join(versionDir, "php.ini")
	b, err := os.ReadFile(ini)
	if err != nil {
		// No php.ini yet (EnsureIni couldn't seed one) — nothing to tune.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := string(b)

	directives := []struct{ key, val string }{
		{"upload_max_filesize", uploadMax},
		{"post_max_size", postMax},
		{"memory_limit", memLimit},
		{"max_execution_time", strconv.Itoa(maxExec)},
		{"max_input_time", strconv.Itoa(maxInput)},
	}
	changed := false
	for _, d := range directives {
		var did bool
		content, did = setIniDirective(content, d.key, d.val)
		changed = changed || did
	}

	if changed {
		return os.WriteFile(ini, []byte(content), 0644)
	}
	return nil
}

// setIniDirective replaces the value of `key = ...` in a php.ini, handling both
// active and commented (`;key = ...`) forms, and appends the directive if it is
// absent. Returns the new content and whether anything changed.
func setIniDirective(content, key, val string) (string, bool) {
	desired := key + " = " + val
	// Already set to exactly the desired value? No-op.
	reExact := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(desired) + `\s*$`)
	if reExact.MatchString(content) {
		return content, false
	}
	// Match an active or commented assignment of this key, anywhere on the line.
	re := regexp.MustCompile(`(?m)^\s*;?\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
	if re.MatchString(content) {
		return re.ReplaceAllString(content, desired), true
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return content + desired + "\n", true
}

// ActivePath returns the absolute path to the currently active PHP install
// (i.e. what `current` symlink points at), or "" if none is set.
func (m *Manager) ActivePath() (string, error) {
	link := filepath.Join(m.root, "current")
	target, err := os.Readlink(link)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(m.root, target)
	}
	return target, nil
}

// EnsureActive guarantees there's a usable `current` symlink whenever at
// least one PHP version is installed: if nothing is active (or the current
// symlink dangles at a deleted version), the highest-numbered installed
// version is activated and returned. Returns "" when no PHP is installed
// or when an active version is already in place. Safe to call on every
// startup — idempotent.
func (m *Manager) EnsureActive() (string, error) {
	if active, err := m.ActivePath(); err == nil && active != "" {
		if _, err := os.Stat(active); err == nil {
			return "", nil
		}
	}
	versions, err := m.List()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", nil
	}
	pick := versions[0].Version
	for _, v := range versions[1:] {
		if compareVersions(v.Version, pick) > 0 {
			pick = v.Version
		}
	}
	if err := m.SetActive(pick); err != nil {
		return "", err
	}
	return pick, nil
}

// compareVersions does a numeric major.minor.patch compare on dotted PHP
// version folder names ("8.4" vs "8.3.7"). Returns -1, 0, 1.
func compareVersions(a, b string) int {
	pa := strings.Split(a, ".")
	pb := strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var ai, bi int
		if i < len(pa) {
			ai, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			bi, _ = strconv.Atoi(pb[i])
		}
		if ai != bi {
			if ai < bi {
				return -1
			}
			return 1
		}
	}
	return 0
}

func (m *Manager) SetActive(version string) error {
	target := filepath.Join(m.root, version)
	if _, err := os.Stat(target); err != nil {
		return i18n.Wrapf(err, "errors.php.versionNotFound", nil)
	}
	// Make sure this version has a usable php.ini before we activate it.
	_ = EnsureIni(target)
	link := filepath.Join(m.root, "current")
	if _, err := os.Lstat(link); err == nil {
		if err := os.Remove(link); err != nil {
			return i18n.Wrapf(err, "errors.php.removeOldLinkFailed", nil)
		}
	}
	return createDirLink(target, link)
}

// createDirLink points `link` at directory `target`. Tries os.Symlink first
// (so users with admin or Developer Mode get a real symlink), and on failure
// falls back to a Windows directory junction via `mklink /J`. Junctions
// don't require SeCreateSymbolicLinkPrivilege so the fallback works for
// every standard user. os.Readlink reads both reparse-point types, so
// downstream code keeps working without changes.
func createDirLink(target, link string) error {
	if err := os.Symlink(target, link); err == nil {
		return nil
	}
	// `mklink /J <link> <target>` — junction, no privilege required. Use
	// SysProcAttr-free exec.Command; cmd.exe is on PATH on every Windows.
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return i18n.Errorf("errors.php.createDirectoryLinkFailed", map[string]any{"link": link, "target": target, "error": err.Error(), "output": strings.TrimSpace(string(out))})
	}
	return nil
}
