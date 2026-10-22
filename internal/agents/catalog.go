package agents

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"lara4ever/internal/i18n"
)

const versionProbeTimeout = 3 * time.Second

type providerDefinition struct {
	id           AgentID
	label        string
	lookPath     []string
	knownPaths   func(catalogDirs) []string
	capabilities AgentCapabilities
}

type catalogDirs struct {
	home         string
	localAppData string
	appData      string
}

type executableLookup func(string) (string, error)
type versionProbe func(context.Context, string) (string, error)

// Catalog detects supported agent CLIs once per app session. Refresh performs
// an explicit re-scan after an install or upgrade.
type Catalog struct {
	mu        sync.Mutex
	lookup    executableLookup
	probe     versionProbe
	dirs      catalogDirs
	profiles  []AgentProfile
	loaded    bool
	completed atomic.Value // immutable []AgentProfile; independent of detection mu
}

func NewCatalog() *Catalog {
	home, _ := os.UserHomeDir()
	return newCatalog(exec.LookPath, probeVersion, catalogDirs{
		home:         home,
		localAppData: os.Getenv("LOCALAPPDATA"),
		appData:      os.Getenv("APPDATA"),
	})
}

func newCatalog(lookup executableLookup, probe versionProbe, dirs catalogDirs) *Catalog {
	return &Catalog{lookup: lookup, probe: probe, dirs: dirs}
}

func (c *Catalog) Profiles(ctx context.Context) []AgentProfile {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		c.profiles = c.detect(ctx)
		c.completed.Store(append([]AgentProfile(nil), c.profiles...))
		c.loaded = true
	}
	return append([]AgentProfile(nil), c.profiles...)
}

func (c *Catalog) Refresh(ctx context.Context) []AgentProfile {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.profiles = c.detect(ctx)
	c.completed.Store(append([]AgentProfile(nil), c.profiles...))
	c.loaded = true
	return append([]AgentProfile(nil), c.profiles...)
}

func (c *Catalog) detect(ctx context.Context) []AgentProfile {
	definitions := builtInProviders()
	profiles := make([]AgentProfile, 0, len(definitions))
	for _, definition := range definitions {
		profiles = append(profiles, c.detectOne(ctx, definition))
	}
	return profiles
}

func (c *Catalog) detectOne(ctx context.Context, definition providerDefinition) AgentProfile {
	profile := AgentProfile{
		ID:           definition.id,
		Label:        definition.label,
		Capabilities: definition.capabilities,
	}

	executable := ""
	for _, name := range definition.lookPath {
		path, err := c.lookup(name)
		if err == nil && isRunnableAgentPath(path) {
			executable = path
			break
		}
	}
	if executable == "" && definition.knownPaths != nil {
		for _, path := range definition.knownPaths(c.dirs) {
			if path == "" {
				continue
			}
			info, err := os.Stat(path)
			if err == nil && !info.IsDir() && isRunnableAgentPath(path) {
				executable = path
				break
			}
		}
	}
	if executable == "" {
		profile.Reason = i18n.T("backend.agents.cliNotFound", nil)
		return profile
	}

	profile.Available = true
	profile.Executable = executable
	probeCtx, cancel := context.WithTimeout(ctx, versionProbeTimeout)
	defer cancel()
	version, err := c.probe(probeCtx, executable)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			profile.Reason = i18n.T("backend.agents.versionProbeTimedOut", nil)
		} else {
			profile.Reason = i18n.T("backend.agents.versionUnavailable", map[string]any{"error": err})
		}
		return profile
	}
	profile.Version = strings.TrimSpace(version)
	return profile
}

func builtInProviders() []providerDefinition {
	launchResume := AgentCapabilities{Launch: true, Resume: true}
	launchResumeContext := AgentCapabilities{Launch: true, Resume: true, AdditionalDirectories: true}
	launchResumeUsage := AgentCapabilities{Launch: true, Resume: true, RichStatus: true, Usage: true, AdditionalDirectories: true}
	return []providerDefinition{
		{
			id: AgentClaude, label: "Claude Code", lookPath: []string{"claude.exe", "claude"},
			knownPaths: func(d catalogDirs) []string {
				return []string{joinKnownPath(d.home, ".local", "bin", "claude.exe")}
			},
			capabilities: launchResumeUsage,
		},
		{
			id: AgentCodex, label: "Codex", lookPath: []string{"codex.exe", "codex"},
			knownPaths: func(d catalogDirs) []string {
				return []string{joinKnownPath(d.localAppData, "Programs", "OpenAI", "Codex", "bin", "codex.exe")}
			},
			capabilities: launchResumeUsage,
		},
		{
			id: AgentKimi, label: "Kimi Code", lookPath: []string{"kimi.exe", "kimi"},
			knownPaths: func(d catalogDirs) []string {
				return []string{joinKnownPath(d.home, ".kimi-code", "bin", "kimi.exe")}
			},
			capabilities: launchResumeContext,
		},
		{
			id: AgentOpenCode, label: "OpenCode", lookPath: []string{"opencode.exe", "opencode-cli.exe"},
			knownPaths: func(d catalogDirs) []string {
				return []string{
					joinKnownPath(d.localAppData, "opencode", "opencode-cli.exe"),
					joinKnownPath(d.appData, "npm", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64", "bin", "opencode.exe"),
					joinKnownPath(d.appData, "npm", "node_modules", "opencode-ai", "node_modules", "opencode-windows-x64-baseline", "bin", "opencode.exe"),
				}
			},
			capabilities: launchResume,
		},
	}
}

func joinKnownPath(base string, elements ...string) string {
	if strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(append([]string{base}, elements...)...)
}

func isRunnableAgentPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	// ConPTY ultimately uses CreateProcess on Windows. PowerShell/npm shims
	// (.ps1/.cmd) need another shell and are therefore not safe direct targets;
	// the catalog resolves the native executable shipped beside those shims.
	return ext != ".ps1" && ext != ".cmd" && ext != ".bat"
}

func probeVersion(ctx context.Context, executable string) (string, error) {
	command := exec.CommandContext(ctx, executable, "--version")
	hideUsageWindow(command)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
