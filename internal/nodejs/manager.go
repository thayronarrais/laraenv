package nodejs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"lara4ever/internal/i18n"
)

// createNoWindow suppresses the conhost flash on the mklink helper spawn.
const createNoWindow = 0x08000000

type Version struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Active  bool   `json:"active"`
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
		out = append(out, Version{
			Version: e.Name(),
			Path:    p,
			Active:  p == currentTarget,
		})
	}
	return out, nil
}

// VersionDir returns the install dir for a specific version (no validation).
// Caller is responsible for checking it exists.
func (m *Manager) VersionDir(version string) string {
	return filepath.Join(m.root, version)
}

// ResolvePath returns the bin dir for `version` if it exists, else falls back
// to the active version's bin dir, else "".
func (m *Manager) ResolvePath(version string) string {
	if version != "" {
		p := m.VersionDir(version)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := m.ActivePath(); err == nil {
		return p
	}
	return ""
}

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

func (m *Manager) ActiveExe() string {
	p, err := m.ActivePath()
	if err != nil || p == "" {
		return ""
	}
	exe := filepath.Join(p, "node.exe")
	if _, err := os.Stat(exe); err != nil {
		return ""
	}
	return exe
}

func (m *Manager) SetActive(version string) error {
	target := filepath.Join(m.root, version)
	if _, err := os.Stat(target); err != nil {
		return i18n.Wrapf(err, "errors.nodejs.versionNotFound", nil)
	}
	link := filepath.Join(m.root, "current")
	if _, err := os.Lstat(link); err == nil {
		if err := os.Remove(link); err != nil {
			return i18n.Wrapf(err, "errors.nodejs.removeOldLinkFailed", nil)
		}
	}
	return createDirLink(target, link)
}

// createDirLink: see internal/php/manager.go for the rationale. Symlink first
// (admin/Developer Mode), junction fallback for standard users.
func createDirLink(target, link string) error {
	if err := os.Symlink(target, link); err == nil {
		return nil
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return i18n.Errorf("errors.nodejs.createDirectoryLinkFailed", map[string]any{"link": link, "target": target, "error": err.Error(), "output": strings.TrimSpace(string(out))})
	}
	return nil
}
