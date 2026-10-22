package php

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

// builtinExtensions returns the list of extensions compiled into php.exe
// itself (no .dll required). Discovered by running `php -n -m`, which loads
// the binary without any ini, so only static built-ins show up.
//
// On Windows PHP, things like bcmath, ctype, date, dom, json, pcre, PDO,
// session, SPL, standard, tokenizer are static built-ins — referencing them
// in php.ini is a no-op, but they should not be flagged as "NO DLL" in the
// UI because the DLL is correctly absent by design.
//
// Errors (php.exe missing, hangs, etc.) return nil — caller falls back to
// the legacy "available iff DLL on disk" behaviour.
func builtinExtensions(versionDir string) map[string]bool {
	exe := filepath.Join(versionDir, "php.exe")
	if _, err := os.Stat(exe); err != nil {
		return nil
	}
	cmd := exec.Command(exe, "-n", "-m")
	// Hide the conhost flash on Windows.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "[") {
			continue
		}
		// Normalise to lowercase to match the keys we build from ini parsing.
		got[strings.ToLower(s)] = true
	}
	return got
}

// Extension is a single PHP extension known to a version.
type Extension struct {
	Name      string `json:"name"`      // e.g. "pdo_mysql"
	Enabled   bool   `json:"enabled"`   // currently enabled in php.ini
	Available bool   `json:"available"` // .dll exists in ext/
}

// ListExtensions returns the union of:
//   - extensions referenced (commented or active) in php.ini
//   - .dll files present in <versionDir>/ext/ (php_<name>.dll)
func ListExtensions(versionDir string) ([]Extension, error) {
	ini := filepath.Join(versionDir, "php.ini")
	iniBytes, err := os.ReadFile(ini)
	if err != nil {
		// No ini? List from ext/ only.
		iniBytes = nil
	}
	content := string(iniBytes)

	enabled := map[string]bool{}
	mentioned := map[string]bool{}

	// Lines starting with `extension=name` or `zend_extension=name` (active).
	// Zend extensions (opcache, xdebug, ionCube, blackfire) use a different
	// directive — both forms must be recognised so the UI shows them as ON.
	reActive := regexp.MustCompile(`(?m)^\s*(?:zend_)?extension\s*=\s*([a-zA-Z0-9_]+)\s*$`)
	for _, m := range reActive.FindAllStringSubmatch(content, -1) {
		enabled[m[1]] = true
		mentioned[m[1]] = true
	}
	// Lines `;extension=name` or `;zend_extension=name` (commented — known but disabled).
	reCommented := regexp.MustCompile(`(?m)^\s*;\s*(?:zend_)?extension\s*=\s*([a-zA-Z0-9_]+)\s*$`)
	for _, m := range reCommented.FindAllStringSubmatch(content, -1) {
		mentioned[m[1]] = true
	}

	// Available = .dll in ext/, OR the extension is statically compiled into
	// php.exe (no DLL needed). The built-in case matters for PHP 7.4 and
	// older where bcmath/ctype/date/etc. ship as part of the binary — without
	// this check they'd render with "NO DLL" forever and the download button
	// would (correctly) fail because the zip doesn't contain such a file.
	available := map[string]bool{}
	extDir := filepath.Join(versionDir, "ext")
	if entries, err := os.ReadDir(extDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := e.Name()
			if !strings.HasSuffix(strings.ToLower(n), ".dll") {
				continue
			}
			n = strings.TrimSuffix(n, ".dll")
			n = strings.TrimSuffix(n, ".DLL")
			n = strings.TrimPrefix(n, "php_")
			available[n] = true
			mentioned[n] = true
		}
	}
	for n := range builtinExtensions(versionDir) {
		available[n] = true
	}

	out := make([]Extension, 0, len(mentioned))
	for n := range mentioned {
		out = append(out, Extension{
			Name:      n,
			Enabled:   enabled[n],
			Available: available[n],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// zendExtensions are loaded via `zend_extension=<name>` instead of the usual
// `extension=<name>` line. Toggling them in php.ini has to use the matching
// directive or PHP silently does nothing. opcache ships with PHP itself;
// xdebug/ionCube/blackfire come in via PECL.
var zendExtensions = map[string]bool{
	"opcache":   true,
	"xdebug":    true,
	"ioncube":   true,
	"blackfire": true,
}

// IsZendExtension reports whether `name` is loaded as a Zend extension. Used
// by SetExtension and by callers seeding php.ini after a PECL install.
func IsZendExtension(name string) bool {
	return zendExtensions[strings.ToLower(name)]
}

// SetExtension toggles a single extension on/off in php.ini for versionDir.
// Picks the right directive (extension= vs zend_extension=) for the
// extension. Creates the ini if missing (via EnsureIni). Idempotent.
func SetExtension(versionDir, name string, enabled bool) error {
	if err := EnsureIni(versionDir); err != nil {
		return err
	}
	ini := filepath.Join(versionDir, "php.ini")
	b, err := os.ReadFile(ini)
	if err != nil {
		return err
	}
	content := string(b)
	q := regexp.QuoteMeta(name)

	directive := "extension"
	if IsZendExtension(name) {
		directive = "zend_extension"
	}
	d := regexp.QuoteMeta(directive)

	reActive := regexp.MustCompile(`(?m)^\s*` + d + `\s*=\s*` + q + `\s*$`)
	reCommented := regexp.MustCompile(`(?m)^\s*;\s*` + d + `\s*=\s*` + q + `\s*$`)

	if enabled {
		if reActive.MatchString(content) {
			// already on
		} else if reCommented.MatchString(content) {
			content = reCommented.ReplaceAllString(content, directive+"="+name)
		} else {
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += directive + "=" + name + "\n"
		}
	} else {
		if reActive.MatchString(content) {
			content = reActive.ReplaceAllString(content, ";"+directive+"="+name)
		}
		// commented or absent — already off
	}

	return os.WriteFile(ini, []byte(content), 0644)
}

// ReadIni returns the raw php.ini content (or "" if missing).
func ReadIni(versionDir string) (string, error) {
	if err := EnsureIni(versionDir); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(versionDir, "php.ini"))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// WriteIni overwrites php.ini with the given content.
func WriteIni(versionDir, content string) error {
	return os.WriteFile(filepath.Join(versionDir, "php.ini"), []byte(content), 0644)
}

// VersionDir returns the install directory for a version (e.g. "8.4" → root\8.4).
func (m *Manager) VersionDir(version string) string {
	return filepath.Join(m.root, version)
}
