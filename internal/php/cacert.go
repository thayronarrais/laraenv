package php

import (
	"bytes"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
	"lara4ever/internal/localtls"
)

// cacertURL is curl.se's mirror of Mozilla's root CA bundle — the de-facto
// CA store for windows PHP installs (windows.php.net zips ship without one).
const cacertURL = "https://curl.se/ca/cacert.pem"

var caCertMu sync.Mutex

// EnsureCaCert makes the PHP install at versionDir capable of doing HTTPS
// out of the box: drops Mozilla's CA bundle at extras/ssl/cacert.pem and
// patches php.ini's curl.cainfo / openssl.cafile to point at it.
//
// Without this, every HTTPS call from cURL or OpenSSL streams fails with
// "cURL error 60: SSL certificate problem" — the windows.php.net zips don't
// bundle a cacert and php.ini ships with both directives commented out, so
// PHP has no idea where to find trusted roots.
//
// Existing bundles and custom active php.ini paths are preserved. Optional
// localCAPaths add public local roots without replacing existing trust roots.
// Missing local CAs are skipped; invalid existing material is reported.
func EnsureCaCert(versionDir string, localCAPaths ...string) error {
	if versionDir == "" {
		return nil
	}
	caCertMu.Lock()
	defer caCertMu.Unlock()
	var localRoots []byte
	for _, path := range localCAPaths {
		if path == "" {
			continue
		}
		public, err := localtls.ReadCA(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read local public CA: %w", err)
		}
		localRoots = append(localRoots, public...)
	}
	sslDir := filepath.Join(versionDir, "extras", "ssl")
	cacert := filepath.Join(sslDir, "cacert.pem")

	bundle, err := os.ReadFile(cacert)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(sslDir, 0755); err != nil {
			return err
		}
		if err := downloadCaCert(cacert); err != nil {
			return i18n.Wrapf(err, "errors.php.downloadCacertFailed", nil)
		}
		bundle, err = os.ReadFile(cacert)
	}
	if err != nil {
		return err
	}
	if _, err := localtls.PublicCertificates(bundle); err != nil {
		return fmt.Errorf("existing PHP CA bundle is invalid; preserved for recovery: %w", err)
	}
	merged := appendMissingCertificates(bundle, localRoots)
	if !bytes.Equal(bundle, merged) {
		if err := writeCaFile(cacert, merged); err != nil {
			return err
		}
	}

	return patchCaCertIni(versionDir, cacert)
}

func appendMissingCertificates(bundle, additions []byte) []byte {
	seen := map[string]bool{}
	for rest := bundle; len(rest) > 0; {
		block, tail := pem.Decode(rest)
		if block == nil {
			break
		}
		seen[string(block.Bytes)] = true
		rest = tail
	}
	out := append([]byte(nil), bundle...)
	for rest := additions; len(rest) > 0; {
		block, tail := pem.Decode(rest)
		if block == nil {
			break
		}
		if !seen[string(block.Bytes)] {
			if len(out) > 0 && out[len(out)-1] != '\n' {
				out = append(out, '\n')
			}
			out = append(out, pem.EncodeToMemory(block)...)
			seen[string(block.Bytes)] = true
		}
		rest = tail
	}
	return out
}

func writeCaFile(path string, data []byte) error {
	mode := os.FileMode(0644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return atomicfile.Write(path, data, mode)
}

func downloadCaCert(dst string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequest("GET", cacertURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LaraEnv (cacert fetcher)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return i18n.Errorf("errors.php.httpStatus", map[string]any{"status": resp.StatusCode})
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(resp.Body, 8*1024*1024+1)); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if len(data) > 8*1024*1024 {
		os.Remove(tmp)
		return fmt.Errorf("downloaded PHP CA bundle exceeds size limit")
	}
	if _, err := localtls.PublicCertificates(data); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("downloaded PHP CA bundle is invalid: %w", err)
	}
	return os.Rename(tmp, dst)
}

// patchCaCertIni guarantees php.ini has curl.cainfo and openssl.cafile set
// to cacertPath. Forward slashes are used in the value so that anyone who
// reads the ini doesn't have to worry about backslash escaping. Re-writes
// the file only when something actually changes.
func patchCaCertIni(versionDir, cacertPath string) error {
	ini := filepath.Join(versionDir, "php.ini")
	if _, err := os.Stat(ini); err != nil {
		// No php.ini yet — EnsureIni hasn't run. Caller (afterPhpInstalled)
		// orders EnsureIni first so this path means a partial/old install
		// that we shouldn't auto-fix here.
		return nil
	}
	b, err := os.ReadFile(ini)
	if err != nil {
		return err
	}
	content := string(b)
	desiredPath := strings.ReplaceAll(cacertPath, `\`, `/`)

	changed := false
	for _, key := range []string{"curl.cainfo", "openssl.cafile"} {
		desiredLine := key + ` = "` + desiredPath + `"`

		already := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) +
			`\s*=\s*"` + regexp.QuoteMeta(desiredPath) + `"`)
		if already.MatchString(content) {
			continue
		}
		// An active non-empty external path belongs to the user. In
		// particular, never replace a corporate/private trust bundle.
		active := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
		if active.MatchString(content) {
			custom := false
			for _, line := range active.FindAllString(content, -1) {
				_, value, _ := strings.Cut(line, "=")
				value = strings.TrimSpace(value)
				if semicolon := strings.IndexByte(value, ';'); semicolon >= 0 {
					value = strings.TrimSpace(value[:semicolon])
				}
				if strings.Trim(value, `"' `) != "" {
					custom = true
					break
				}
			}
			if custom {
				continue
			}
			content = active.ReplaceAllStringFunc(content, func(string) string { return desiredLine })
			changed = true
			continue
		}
		// Commented-out form → uncomment with our path.
		commented := regexp.MustCompile(`(?m)^\s*;\s*` + regexp.QuoteMeta(key) + `\s*=.*$`)
		if commented.MatchString(content) {
			content = commented.ReplaceAllStringFunc(content, func(string) string { return desiredLine })
			changed = true
			continue
		}
		// Not present at all — append.
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += desiredLine + "\n"
		changed = true
	}

	if !changed {
		return nil
	}
	return os.WriteFile(ini, []byte(content), 0644)
}

// RebaseCaCertIni is used only when relocation has a known previous root.
// Unlike generic setup, it can prove the old generated path's identity and
// repair it without treating an arbitrary custom CA path as app-owned.
func RebaseCaCertIni(versionDir, previousVersionDir string) error {
	path := filepath.Join(versionDir, "php.ini")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	previous := strings.ReplaceAll(filepath.Join(previousVersionDir, "extras", "ssl", "cacert.pem"), `\`, `/`)
	current := strings.ReplaceAll(filepath.Join(versionDir, "extras", "ssl", "cacert.pem"), `\`, `/`)
	line := regexp.MustCompile(`(?mi)^([\t ]*(?:curl\.cainfo|openssl\.cafile)[\t ]*=[\t ]*)(?:"([^"\r\n]*)"|'([^'\r\n]*)'|([^;\r\n]*))([^\r\n]*)`)
	changed := false
	content := line.ReplaceAllStringFunc(string(data), func(original string) string {
		parts := line.FindStringSubmatch(original)
		value := parts[2] + parts[3] + parts[4]
		if !strings.EqualFold(strings.ReplaceAll(strings.TrimSpace(value), `\`, `/`), previous) {
			return original
		}
		changed = true
		return parts[1] + `"` + current + `"` + parts[5]
	})
	if !changed {
		return nil
	}
	return atomicfile.Write(path, []byte(content), 0644)
}
