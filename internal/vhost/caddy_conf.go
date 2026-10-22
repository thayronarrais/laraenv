package vhost

import (
	"os"
	"path/filepath"
	"strings"
)

const caddyImportLine = "import sites/*.caddy"

// EnsureCaddyfile makes sure the main Caddyfile exists and imports the
// per-project site files.
//
// The Caddy release ships no configuration, so unlike nginx.conf there is
// usually nothing to patch — the file is created. When one already exists it
// is appended to rather than replaced: the user may have written global
// options into it.
func EnsureCaddyfile(caddyfilePath, sitesDir string) error {
	if err := os.MkdirAll(sitesDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(caddyfilePath), 0755); err != nil {
		return err
	}

	b, err := os.ReadFile(caddyfilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(b)

	if strings.Contains(content, caddyImportLine) {
		return nil
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += caddyImportLine + "\n"
	return os.WriteFile(caddyfilePath, []byte(content), 0644)
}
