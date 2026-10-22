package projects

import (
	"os"
	"path/filepath"
	"sort"

	"lara4ever/internal/config"
)

type Project struct {
	PhpRuntime     RuntimeResolution `json:"phpRuntime"`
	NodeRuntime    RuntimeResolution `json:"nodeRuntime"`
	Paused         bool              `json:"paused"`
	Name           string            `json:"name"`
	URL            string            `json:"url"`
	Path           string            `json:"path"`
	Kind           string            `json:"kind"`
	DocRoot        string            `json:"docRoot"`
	PhpVersion     string            `json:"phpVersion"`     // manual override; "" → automatic detection
	NodeVersion    string            `json:"nodeVersion"`    // manual override; "" → automatic detection
	SSL            bool              `json:"ssl"`            // HTTPS enabled
	JSFramework    string            `json:"jsFramework"`    // "" if not a JS project
	PackageManager string            `json:"packageManager"` // "" if no package.json
	ProxyPort      int               `json:"proxyPort"`      // 0 → no proxy; >0 → reverse-proxy vhost to localhost:port
	Tags           []string          `json:"tags"`           // free-form categorization labels
}

type Scanner struct {
	cfg *config.Store
}

func NewScanner(cfg *config.Store) *Scanner { return &Scanner{cfg: cfg} }

// Root returns the configured projects root.
func (s *Scanner) Root() string { return s.cfg.Get().Projects.Root }

// TLD returns the configured TLD (without the leading dot).
func (s *Scanner) TLD() string { return s.cfg.Get().Projects.TLD }

func (s *Scanner) List() ([]Project, error) {
	root := s.Root()
	tld := s.TLD()
	if tld == "" {
		tld = "test"
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		_ = os.MkdirAll(root, 0755)
		return []Project{}, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	prefs := s.cfg.Get().Projects
	versions := prefs.PhpVersions
	nodeVersions := prefs.NodeVersions
	proxyPorts := prefs.ProxyPorts
	ssl := prefs.SSL
	tags := prefs.ProjectTags
	out := make([]Project, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name())
		det := Detect(path)
		scheme := "http://"
		if ssl[e.Name()] {
			scheme = "https://"
		}
		out = append(out, Project{
			Paused:         prefs.Paused[e.Name()],
			Name:           e.Name(),
			URL:            scheme + e.Name() + "." + tld,
			Path:           path,
			Kind:           string(det.Kind),
			DocRoot:        det.DocRoot,
			PhpVersion:     versions[e.Name()],
			NodeVersion:    nodeVersions[e.Name()],
			SSL:            ssl[e.Name()],
			JSFramework:    det.JSFramework,
			PackageManager: det.PackageManager,
			ProxyPort:      proxyPorts[e.Name()],
			Tags:           tags[e.Name()],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
