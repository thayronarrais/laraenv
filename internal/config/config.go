package config

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/credentials"
	"lara4ever/internal/i18n"
	"lara4ever/internal/paths"
)

// Stack describes which services should appear on the Dashboard.
// Each key is a service name (matching services.Manager registration);
// value true = visible on Dashboard. Multiple services of the same role
// (nginx + apache, mysql + postgres) can coexist — the user decides.
type Stack struct {
	Enabled map[string]bool `json:"enabled"`
}

// Projects holds the projects-related preferences.
type Projects struct {
	Paused                map[string]bool         `json:"paused,omitempty"`
	TLD                   string                  `json:"tld"`                     // e.g. "test"
	Root                  string                  `json:"root"`                    // e.g. C:\laraenv\www
	PhpVersions           map[string]string       `json:"phpVersions"`             // project name → "8.4"
	NodeVersions          map[string]string       `json:"nodeVersions"`            // project name → "22.22.2"
	SSL                   map[string]bool         `json:"ssl"`                     // project name → SSL enabled
	Terminals             map[string]string       `json:"terminals"`               // project name → terminal kind ("powershell", "cmd", "gitbash", "cmder")
	DefaultTerminal       string                  `json:"defaultTerminal"`         // fallback when project has no override
	ProxyPorts            map[string]int          `json:"proxyPorts"`              // project name → reverse-proxy target port (0/missing = no proxy)
	ProjectTags           map[string][]string     `json:"projectTags"`             // project name → free-form labels/categories
	QueueArgs             map[string]string       `json:"queueArgs"`               // project name → last extra args passed to `php artisan queue:work`
	SSHHosts              map[string]SSHHost      `json:"sshHosts"`                // host name → SSH connection config
	DefaultPackageManager string                  `json:"defaultPackageManager"`   // npm/pnpm/yarn/bun for create-* templates
	Editors               map[string]string       `json:"editors"`                 // project name → editor kind ("vscode", "cursor", …)
	DefaultEditor         string                  `json:"defaultEditor"`           // fallback when project has no override
	CustomEditors         map[string]string       `json:"customEditors"`           // project name → custom CLI command
	DefaultCustomEditor   string                  `json:"defaultCustomEditor"`     // global custom CLI command
	DevCommands           map[string]string       `json:"devCommands"`             // project name → custom dev command ("" = default npm run dev)
	DefaultDevCommand     string                  `json:"defaultDevCommand"`       // optional global default dev command
	DeployPresets         map[string]DeployPreset `json:"deployPresets,omitempty"` // preset id → custom deploy script preset
}

// DeployPreset is a reusable deploy pipeline: a bash script with {token}
// placeholders that get substituted from the deployment's ApplyConfig plus
// the user-supplied values for the preset's declared Variables. Built-in
// presets ship with the app (id "builtin:*", never persisted here); custom
// ones live in config.json and sync to the cloud as encrypted resources.
type DeployPreset struct {
	ID          string      `json:"id"`   // uuid for custom; "builtin:<slug>" for built-ins
	Name        string      `json:"name"` // user-facing label
	Description string      `json:"description,omitempty"`
	Script      string      `json:"script"`              // bash with {placeholder} tokens
	Variables   []PresetVar `json:"variables,omitempty"` // declared custom vars (valued per deployment)
	BuiltIn     bool        `json:"builtIn,omitempty"`   // computed at list time; never persisted/synced
	UpdatedAt   string      `json:"updatedAt,omitempty"` // RFC3339 — sync display only
}

// PresetVar declares one custom variable a preset expects. The per-deployment
// value is stored in ApplyConfig.PresetVars, keyed by Key.
type PresetVar struct {
	Key     string `json:"key"`               // [a-z_][a-z0-9_]* — must not collide with built-in var names
	Label   string `json:"label,omitempty"`   // form label; falls back to Key
	Default string `json:"default,omitempty"` // value used when the deployment doesn't set one
}

// SSHHost is one saved SSH target managed from the SSH page.
// Password remains available in memory for SSH, the UI and encrypted cloud
// sync. config.json uses a separate Windows user-protected representation.
type SSHHost struct {
	Name                string   `json:"name"`    // unique key (also used as dir name under C:\laraenv\ssh\)
	Address             string   `json:"address"` // host or IP
	Port                int      `json:"port"`    // default 22
	User                string   `json:"user"`
	Tags                []string `json:"tags"`                          // free-form labels/categories
	Tag                 string   `json:"tag,omitempty"`                 // legacy single-tag field; folded into Tags on load
	AuthMode            string   `json:"authMode"`                      // "password" | "key"
	Password            string   `json:"password,omitempty"`            // when authMode == "password"
	PasswordUnavailable bool     `json:"passwordUnavailable,omitempty"` // re-enter after moving to another Windows account
	KeyName             string   `json:"keyName,omitempty"`             // private-key filename inside SSHHostDir(Name)
	// ProxyJump is the Name of another saved SSHHost to tunnel through
	// (equivalent to ssh -J <jump>). Empty = direct connection. The jump
	// host uses its own configured auth — we don't ask credentials twice.
	ProxyJump string `json:"proxyJump,omitempty"`
	// ProxyCommand is an advanced escape hatch: an arbitrary command whose
	// stdin/stdout becomes the connection (equivalent to ssh -o ProxyCommand=).
	// Takes precedence over ProxyJump when set. %h and %p are substituted
	// with the target host and port (matching OpenSSH).
	ProxyCommand string `json:"proxyCommand,omitempty"`
}

// Services holds per-service tuning knobs the user can edit from
// Settings > Services. Zero values mean "use the built-in default".
type Services struct {
	Nginx  NginxTuning  `json:"nginx"`
	Apache ApacheTuning `json:"apache"`
	Php    PhpTuning    `json:"php"`
}

// PhpTuning maps to php.ini upload/runtime limits plus the php-cgi worker-pool
// size. Empty size strings / zero ints fall back to php.PhpTuningDefaults.
// Sizes are php.ini strings ("512M"); times are seconds; Workers is the number
// of php-cgi processes per version (so a single large upload can't freeze the
// whole site — php-cgi handles one request at a time).
type PhpTuning struct {
	UploadMaxFilesize string `json:"uploadMaxFilesize"` // → upload_max_filesize
	PostMaxSize       string `json:"postMaxSize"`       // → post_max_size
	MemoryLimit       string `json:"memoryLimit"`       // → memory_limit
	MaxExecutionTime  int    `json:"maxExecutionTime"`  // → max_execution_time (s)
	MaxInputTime      int    `json:"maxInputTime"`      // → max_input_time (s)
	Workers           int    `json:"workers"`           // php-cgi pool size per version (clamped 1..16)
}

// NginxTuning maps to nginx.conf http {} directives. Zero values fall
// back to sensible defaults (see DefaultNginxTuning). Sizes are in MB,
// timeouts in seconds. Bucket must be a power of 2.
type NginxTuning struct {
	ServerNamesHashBucketSize int `json:"serverNamesHashBucketSize"`
	ServerNamesHashMaxSize    int `json:"serverNamesHashMaxSize"`
	ClientMaxBodyMB           int `json:"clientMaxBodyMB"`       // → client_max_body_size <N>M
	ClientBodyTimeoutSec      int `json:"clientBodyTimeoutSec"`  // → client_body_timeout <N>s
	FastCGIReadTimeoutSec     int `json:"fastcgiReadTimeoutSec"` // → fastcgi_read_timeout <N>s
	ProxyReadTimeoutSec       int `json:"proxyReadTimeoutSec"`   // → proxy_read_timeout <N>s
	KeepaliveTimeoutSec       int `json:"keepaliveTimeoutSec"`   // → keepalive_timeout <N>s
	WorkerConnections         int `json:"workerConnections"`     // → events { worker_connections <N>; }
}

// ApacheTuning maps to global directives in httpd.conf — the things that
// commonly cause 504s and rejected uploads in dev. Zero values fall back
// to defaults (see DefaultApacheTuning).
type ApacheTuning struct {
	TimeoutSec          int `json:"timeoutSec"`          // → Timeout <N>
	KeepAliveTimeoutSec int `json:"keepAliveTimeoutSec"` // → KeepAliveTimeout <N>
	LimitRequestBodyMB  int `json:"limitRequestBodyMB"`  // → LimitRequestBody <bytes> (0 = unlimited)
	ProxyTimeoutSec     int `json:"proxyTimeoutSec"`     // → ProxyTimeout <N>
}

type Config struct {
	// InstallRoot is where this environment last ran. Compared against the
	// resolved root on every start to notice that the whole folder moved.
	InstallRoot string `json:"installRoot,omitempty"`

	// Language is the UI language code, matching a langs/<code>.json that
	// shipped ("en", "pt-BR"). Empty means "never chosen": the app asks
	// Windows on first run and falls back to English.
	Language string `json:"language,omitempty"`

	Stack    Stack    `json:"stack"`
	Projects Projects `json:"projects"`
	Services Services `json:"services"`
	// Experimental controls features that are intentionally opt-in while their
	// provider and Windows behavior matrices are still being validated.
	Experimental Experimental `json:"experimental"`
	// Crons is the persisted list of scheduled jobs. Stored as a slice of
	// raw JSON to avoid importing internal/cron from config (cycle). The
	// app re-hydrates them through cron.Manager.Start.
	Crons []map[string]any `json:"crons,omitempty"`
	// TerminalWorkspace is the persisted floating-groups terminal layout
	// (groups, positions, tabs, split trees + per-pane spawn descriptors).
	// Stored as raw JSON owned entirely by the frontend; the backend only
	// round-trips it so the canvas can be restored on next launch.
	TerminalWorkspace json.RawMessage `json:"terminalWorkspace,omitempty"`
	// DocumentWorkspace contains local document references, never buffers.
	DocumentWorkspace json.RawMessage `json:"documentWorkspace,omitempty"`
	// Cloud holds the user's session for the LaraEnv Cloud (Pro tier).
	// Zero value = not logged in.
	Cloud Cloud `json:"cloud"`
	// CommandCenter holds the command-palette config: the rebindable palette
	// hotkey and the user's custom commands (with optional per-command hotkeys).
	CommandCenter CommandCenter `json:"commandCenter"`
}

type Experimental struct {
	MCPEnabled    bool `json:"mcpEnabled"`
	AgentSessions bool `json:"agentSessions"`
	// AgentHooksDisabled opts out of the provider lifecycle hooks that make
	// agents started inside LaraEnv terminals appear in the Agents page. The
	// hooks are wanted by default whenever AgentSessions is enabled; the zero
	// value (false) means "hooks wanted".
	AgentHooksDisabled bool `json:"agentHooksDisabled"`
}

// CommandShortcut is a single user-defined command shown in the command
// palette and optionally bound to a global hotkey. Action determines which
// of the remaining fields are meaningful.
type CommandShortcut struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Action    string `json:"action"`              // "terminal" | "service" | "navigate" | "openTerminal"
	Command   string `json:"command,omitempty"`   // terminal: command line to run
	Shell     string `json:"shell,omitempty"`     // terminal kind (e.g. "powershell"); empty = default
	Project   string `json:"project,omitempty"`   // optional project scope for terminal/openTerminal
	Service   string `json:"service,omitempty"`   // service action target
	ServiceOp string `json:"serviceOp,omitempty"` // start|stop|reload|startAll|stopAll|reloadStack
	Page      string `json:"page,omitempty"`      // navigate target
	Hotkey    string `json:"hotkey,omitempty"`    // optional, e.g. "Ctrl+Shift+1"
}

// CommandCenter is the persisted command-palette configuration.
type CommandCenter struct {
	PaletteHotkey string `json:"paletteHotkey"` // in-app palette key, default "Ctrl+K"
	// GlobalHotkey is an OS-wide hotkey (e.g. "Win+Shift+K") that summons the
	// palette even when the window is hidden/minimised. Empty = disabled.
	GlobalHotkey string            `json:"globalHotkey"`
	Commands     []CommandShortcut `json:"commands"`
}

// Cloud is the persisted state of the user's session against laraenv-server.
//
// Security note: SyncKey is the AES-256 key derived from the user's password
// via Argon2id. We store it locally so the user doesn't have to re-enter their
// password every launch. This file (`config.json`) is in the user's profile
// directory — same threat model as the Sanctum token and the existing SSH
// passwords already kept here in plaintext.
type Cloud struct {
	Enabled     bool   `json:"enabled"` // absent in legacy profiles means disabled
	BaseURL     string `json:"baseUrl"`
	Token       string `json:"token,omitempty"`
	UserID      int    `json:"userId,omitempty"`
	Email       string `json:"email,omitempty"`
	Name        string `json:"name,omitempty"`
	GithubID    string `json:"githubId,omitempty"`
	Plan        string `json:"plan,omitempty"`    // "free" | "pro"
	KdfSalt     string `json:"kdfSalt,omitempty"` // hex (32 bytes)
	SyncKey     string `json:"syncKey,omitempty"` // base64 (32 bytes) — derived; absent for github-only accounts
	SyncEnabled bool   `json:"syncEnabled,omitempty"`
}

var DefaultConfig = Config{
	Stack: Stack{
		Enabled: map[string]bool{
			"nginx":   true,
			"mysql":   true,
			"mailpit": true,
		},
	},
	Projects: Projects{
		TLD: "test",
		// Root is deliberately absent: the install root is resolved at
		// runtime, so it cannot be a compile-time default. NewStore fills
		// it in from paths.ProjectsRoot.
	},
	Cloud: Cloud{
		// Production server. New installs use this if they opt in; existing
		// installs keep whatever URL they previously persisted to config.json
		// (Settings → Account → "change server" lets the user override).
		BaseURL: "https://laraenv.com",
	},
}

type Store struct {
	mu              sync.Mutex
	path            string
	data            Config
	passwords       passwordProtector
	lockedPasswords map[string]string // opaque disk blobs unavailable to this Windows account

	// relocatedFrom is the install root this environment was last used at,
	// set only when it differs from where it is running now.
	relocatedFrom string
}

// RelocatedFrom reports the previous install root when the environment has
// been moved since it last ran, or "" when it has not. The caller uses it to
// fix up the state files and PHP inis, which this package does not own.
func (s *Store) RelocatedFrom() string { return s.relocatedFrom }

// reconcileRoot detects that the whole environment was moved - a portable
// copy carried to another drive, or simply a renamed folder - and follows it.
//
// Only stored paths that actually live under the old root move. A projects
// folder the user deliberately keeps elsewhere is not part of the environment
// and must survive untouched.
//
// Most derived configuration repairs itself on the next boot: the startup
// vhost sweep rewrites every vhost, hosts entry and certificate, and
// EnsureCaCert rewrites the PHP CA paths. That self-healing runs off
// Projects.Root, though, so leaving this one field stale silently disables
// all of it - the project list comes back empty and nothing gets rebuilt.
// It reports whether anything changed, so the caller can persist. Writing the
// new root back is not optional: without it the next start would detect the
// same move again, redo the fixups and re-notify, forever.
func (s *Store) reconcileRoot() bool {
	current := paths.Root()
	previous := s.data.InstallRoot

	if paths.SameRoot(previous, current) {
		return false
	}
	if previous == "" {
		s.data.InstallRoot = current
		return true
	}

	if moved, ok := paths.Rebase(previous, current, s.data.Projects.Root); ok {
		s.data.Projects.Root = moved
	}
	s.data.InstallRoot = current
	s.relocatedFrom = previous
	return true
}

func NewStore(path string) (*Store, error) {
	return newStore(path, credentials.System{})
}

func newStore(path string, protector passwordProtector) (*Store, error) {
	s := &Store{path: path, data: DefaultConfig, passwords: protector, lockedPasswords: map[string]string{}}
	needsSave := false
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil {
		c, locked, legacy, err := decodeDiskConfig(b, protector)
		if err != nil {
			return nil, fmt.Errorf("read configuration without discarding saved credentials: %w", err)
		}
		s.lockedPasswords = locked
		// Backfill defaults for any zero-value field added in newer versions.
		if c.Projects.TLD == "" {
			c.Projects.TLD = DefaultConfig.Projects.TLD
		}
		// Backfill the dashboard-visibility map for configs that predate the
		// Stack.Enabled field (nil = never set). An explicit empty map means
		// the user hid everything on purpose — leave that alone.
		if c.Stack.Enabled == nil {
			c.Stack.Enabled = map[string]bool{}
			for k, v := range DefaultConfig.Stack.Enabled {
				c.Stack.Enabled[k] = v
			}
		}
		// Migration: legacy install root C:\lara4ever\... → C:\laraenv\...
		// A recorded install root belongs to relocation handling below; rewriting
		// its project path first would prevent rebasing it from the original root.
		migrated := legacy
		if c.InstallRoot == "" {
			if rewritten := rewriteLegacyRoot(c.Projects.Root); rewritten != c.Projects.Root {
				c.Projects.Root = rewritten
				migrated = true
			}
		}
		// Migration: legacy single-tag SSH hosts → multi-tag. Older configs
		// stored `"tag": "PROD"`; fold that into the new `Tags` slice and
		// drop the legacy field on next save.
		for name, h := range c.Projects.SSHHosts {
			if len(h.Tags) == 0 && h.Tag != "" {
				h.Tags = []string{h.Tag}
				migrated = true
			}
			if h.Tag != "" {
				h.Tag = ""
				migrated = true
			}
			c.Projects.SSHHosts[name] = h
		}
		s.data = c
		needsSave = migrated
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Covers both a fresh install (no config.json, so the block above never
	// runs) and a config.json written before this key existed. Not persisted
	// here — the value is written on the next save, same as before.
	if s.data.Projects.Root == "" {
		s.data.Projects.Root = paths.ProjectsRoot()
	}
	needsSave = s.reconcileRoot() || needsSave
	if needsSave {
		if err := s.save(); err != nil {
			return nil, fmt.Errorf("persist configuration migration; original file preserved: %w", err)
		}
	}
	return s, nil
}

// rewriteLegacyRoot replaces the legacy `C:\lara4ever\` install root with
// the current `C:\laraenv\` while preserving the drive case and rest of the
// path. Matching is case-insensitive and restricted to the historical prefix;
// a custom directory named lara4ever elsewhere is not an old install root.
func rewriteLegacyRoot(p string) string {
	const legacyRoot = `C:\lara4ever\`
	if !strings.HasPrefix(strings.ToLower(p), strings.ToLower(legacyRoot)) {
		return p
	}
	return p[:len(`C:\`)] + `laraenv\` + p[len(legacyRoot):]
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Callers (GUI, background workers and MCP) must never iterate maps that
	// setters can mutate after this lock is released.
	out := s.data
	out.Stack.Enabled = maps.Clone(out.Stack.Enabled)
	p := &out.Projects
	p.Paused = maps.Clone(p.Paused)
	p.PhpVersions = maps.Clone(p.PhpVersions)
	p.NodeVersions = maps.Clone(p.NodeVersions)
	p.SSL = maps.Clone(p.SSL)
	p.Terminals = maps.Clone(p.Terminals)
	p.ProxyPorts = maps.Clone(p.ProxyPorts)
	p.QueueArgs = maps.Clone(p.QueueArgs)
	p.Editors = maps.Clone(p.Editors)
	p.CustomEditors = maps.Clone(p.CustomEditors)
	p.DevCommands = maps.Clone(p.DevCommands)
	p.ProjectTags = maps.Clone(p.ProjectTags)
	for k, v := range p.ProjectTags {
		p.ProjectTags[k] = slices.Clone(v)
	}
	p.SSHHosts = maps.Clone(p.SSHHosts)
	for k, v := range p.SSHHosts {
		v.Tags = slices.Clone(v.Tags)
		p.SSHHosts[k] = v
	}
	p.DeployPresets = maps.Clone(p.DeployPresets)
	for k, v := range p.DeployPresets {
		v.Variables = slices.Clone(v.Variables)
		p.DeployPresets[k] = v
	}
	out.CommandCenter.Commands = slices.Clone(out.CommandCenter.Commands)
	out.TerminalWorkspace = slices.Clone(out.TerminalWorkspace)
	out.DocumentWorkspace = slices.Clone(out.DocumentWorkspace)
	out.Crons = make([]map[string]any, len(s.data.Crons))
	for i, v := range s.data.Crons {
		out.Crons[i] = cloneJSONMap(v)
	}
	return out
}

func cloneJSONMap(in map[string]any) map[string]any {
	out := maps.Clone(in)
	for k, v := range out {
		switch x := v.(type) {
		case map[string]any:
			out[k] = cloneJSONMap(x)
		case []any:
			out[k] = cloneJSONSlice(x)
		}
	}
	return out
}
func cloneJSONSlice(in []any) []any {
	out := slices.Clone(in)
	for i, v := range out {
		switch x := v.(type) {
		case map[string]any:
			out[i] = cloneJSONMap(x)
		case []any:
			out[i] = cloneJSONSlice(x)
		}
	}
	return out
}

func (s *Store) SetMCPEnabled(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.Experimental.MCPEnabled
	s.data.Experimental.MCPEnabled = enabled
	if err := s.save(); err != nil {
		s.data.Experimental.MCPEnabled = previous
		return err
	}
	return nil
}

func (s *Store) SetProjectPaused(name string, paused bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Projects.Paused == nil {
		s.data.Projects.Paused = map[string]bool{}
	}
	previous := s.data.Projects.Paused[name]
	if paused {
		s.data.Projects.Paused[name] = true
	} else {
		delete(s.data.Projects.Paused, name)
	}
	if err := s.save(); err != nil {
		s.data.Projects.Paused[name] = previous
		return err
	}
	return nil
}

func (s *Store) save() error {
	b, err := encodeDiskConfig(s.data, s.lockedPasswords, s.passwords)
	if err != nil {
		return err
	}
	return atomicfile.WritePreservingPermissions(s.path, b, 0600)
}

func (s *Store) SetStack(st Stack) error {
	s.mu.Lock()
	s.data.Stack = st
	err := s.save()
	s.mu.Unlock()
	return err
}

func (s *Store) SetProjects(p Projects) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous := s.data.Projects
	p.SSHHosts = maps.Clone(p.SSHHosts)
	for name, host := range p.SSHHosts {
		_, locked := s.lockedPasswords[name]
		host.PasswordUnavailable = locked && host.Password == ""
		p.SSHHosts[name] = host
	}
	s.data.Projects = p
	if err := s.save(); err != nil {
		s.data.Projects = previous
		return err
	}
	for name := range s.lockedPasswords {
		if host, exists := p.SSHHosts[name]; !exists || host.Password != "" {
			delete(s.lockedPasswords, name)
		}
	}
	return nil
}

// SetProjectSSL toggles HTTPS for a single project.
func (s *Store) SetProjectSSL(name string, enabled bool) error {
	s.mu.Lock()
	if s.data.Projects.SSL == nil {
		s.data.Projects.SSL = map[string]bool{}
	}
	if enabled {
		s.data.Projects.SSL[name] = true
	} else {
		delete(s.data.Projects.SSL, name)
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetServiceEnabled toggles a single service's visibility on the Dashboard.
func (s *Store) SetServiceEnabled(name string, enabled bool) error {
	s.mu.Lock()
	if s.data.Stack.Enabled == nil {
		s.data.Stack.Enabled = map[string]bool{}
	}
	if enabled {
		s.data.Stack.Enabled[name] = true
	} else {
		delete(s.data.Stack.Enabled, name)
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetSSHHost upserts an SSH host. Empty name returns an error.
func (s *Store) SetSSHHost(h SSHHost) error {
	if h.Name == "" {
		return i18n.Errorf("errors.config.sshHostNameRequired", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, locked := s.lockedPasswords[h.Name]; locked && h.Password == "" {
		h.PasswordUnavailable = true
		if h.AuthMode != "key" {
			return fmt.Errorf("saved SSH password for host %q is unavailable for this Windows account; re-enter and save the password", h.Name)
		}
	} else {
		h.PasswordUnavailable = false
	}
	if s.data.Projects.SSHHosts == nil {
		s.data.Projects.SSHHosts = map[string]SSHHost{}
	}
	previous, existed := s.data.Projects.SSHHosts[h.Name]
	s.data.Projects.SSHHosts[h.Name] = h
	if err := s.save(); err != nil {
		if existed {
			s.data.Projects.SSHHosts[h.Name] = previous
		} else {
			delete(s.data.Projects.SSHHosts, h.Name)
		}
		return err
	}
	if h.Password != "" {
		delete(s.lockedPasswords, h.Name)
	}
	return nil
}

// RemoveSSHHost deletes a host entry from the config.
func (s *Store) RemoveSSHHost(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.data.Projects.SSHHosts[name]
	delete(s.data.Projects.SSHHosts, name)
	if err := s.save(); err != nil {
		if existed {
			s.data.Projects.SSHHosts[name] = previous
		}
		return err
	}
	delete(s.lockedPasswords, name)
	return nil
}

// ListSSHHosts returns a snapshot of the saved hosts.
func (s *Store) ListSSHHosts() []SSHHost {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SSHHost, 0, len(s.data.Projects.SSHHosts))
	for _, h := range s.data.Projects.SSHHosts {
		out = append(out, h)
	}
	return out
}

// SetDeployPreset upserts a custom deploy script preset. Empty ID or
// built-in IDs return an error — built-ins are compiled into the binary.
func (s *Store) SetDeployPreset(p DeployPreset) error {
	if p.ID == "" {
		return i18n.Errorf("errors.config.deployPresetIdRequired", nil)
	}
	if strings.HasPrefix(p.ID, "builtin:") {
		return i18n.Errorf("errors.config.builtinPresetImmutable", nil)
	}
	p.BuiltIn = false // computed flag, never persisted
	s.mu.Lock()
	if s.data.Projects.DeployPresets == nil {
		s.data.Projects.DeployPresets = map[string]DeployPreset{}
	}
	s.data.Projects.DeployPresets[p.ID] = p
	err := s.save()
	s.mu.Unlock()
	return err
}

// RemoveDeployPreset deletes a custom preset entry from the config.
func (s *Store) RemoveDeployPreset(id string) error {
	if strings.HasPrefix(id, "builtin:") {
		return i18n.Errorf("errors.config.builtinPresetNotDeletable", nil)
	}
	s.mu.Lock()
	delete(s.data.Projects.DeployPresets, id)
	err := s.save()
	s.mu.Unlock()
	return err
}

// ListDeployPresets returns a snapshot of the saved custom presets.
func (s *Store) ListDeployPresets() []DeployPreset {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DeployPreset, 0, len(s.data.Projects.DeployPresets))
	for _, p := range s.data.Projects.DeployPresets {
		out = append(out, p)
	}
	return out
}

// SetProjectTerminal records the preferred terminal kind for a single project.
// kind="" removes the override (project will use the global DefaultTerminal).
func (s *Store) SetProjectTerminal(name, kind string) error {
	s.mu.Lock()
	if s.data.Projects.Terminals == nil {
		s.data.Projects.Terminals = map[string]string{}
	}
	if kind == "" {
		delete(s.data.Projects.Terminals, name)
	} else {
		s.data.Projects.Terminals[name] = kind
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetDefaultTerminal sets the global fallback terminal kind.
func (s *Store) SetDefaultTerminal(kind string) error {
	s.mu.Lock()
	s.data.Projects.DefaultTerminal = kind
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectEditor records the preferred editor kind for a single project.
// kind="" removes the override (project will use the global DefaultEditor).
func (s *Store) SetProjectEditor(name, kind string) error {
	s.mu.Lock()
	if s.data.Projects.Editors == nil {
		s.data.Projects.Editors = map[string]string{}
	}
	if kind == "" {
		delete(s.data.Projects.Editors, name)
	} else {
		s.data.Projects.Editors[name] = kind
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetDefaultEditor sets the global fallback editor kind.
func (s *Store) SetDefaultEditor(kind string) error {
	s.mu.Lock()
	s.data.Projects.DefaultEditor = kind
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectCustomEditor stores the custom CLI command for a specific project.
// cmd="" removes the override.
func (s *Store) SetProjectCustomEditor(name, cmd string) error {
	s.mu.Lock()
	if s.data.Projects.CustomEditors == nil {
		s.data.Projects.CustomEditors = map[string]string{}
	}
	if cmd == "" {
		delete(s.data.Projects.CustomEditors, name)
	} else {
		s.data.Projects.CustomEditors[name] = cmd
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetDefaultCustomEditor stores the global custom CLI command.
func (s *Store) SetDefaultCustomEditor(cmd string) error {
	s.mu.Lock()
	s.data.Projects.DefaultCustomEditor = cmd
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetNginxTuning persists the nginx-specific tuning knobs. Caller is
// responsible for re-applying them to the live nginx.conf and restarting
// the service if it's running.
func (s *Store) SetNginxTuning(t NginxTuning) error {
	s.mu.Lock()
	s.data.Services.Nginx = t
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetApacheTuning persists the apache-specific tuning knobs. Caller is
// responsible for re-applying to httpd.conf and restarting Apache.
func (s *Store) SetApacheTuning(t ApacheTuning) error {
	s.mu.Lock()
	s.data.Services.Apache = t
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetPhpTuning persists the php.ini upload/runtime limits and worker-pool size.
// Caller is responsible for re-applying them (EnsurePhpTuning per version,
// rebuilding the php-cgi pool + nginx upstreams) and reloading services.
func (s *Store) SetPhpTuning(t PhpTuning) error {
	s.mu.Lock()
	s.data.Services.Php = t
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetCloud replaces the persisted cloud session block.
func (s *Store) SetCloud(c Cloud) error {
	s.mu.Lock()
	previous := s.data.Cloud
	s.data.Cloud = c
	err := s.save()
	if err != nil {
		s.data.Cloud = previous
	}
	s.mu.Unlock()
	return err
}

// GetCommandCenter returns the persisted command-palette config, defaulting
// the palette hotkey to "Ctrl+K" when unset.
func (s *Store) GetCommandCenter() CommandCenter {
	s.mu.Lock()
	defer s.mu.Unlock()
	cc := s.data.CommandCenter
	if cc.PaletteHotkey == "" {
		cc.PaletteHotkey = "Ctrl+K"
	}
	return cc
}

// SetCommandCenter replaces the entire command-palette config.
func (s *Store) SetCommandCenter(cc CommandCenter) error {
	s.mu.Lock()
	s.data.CommandCenter = cc
	err := s.save()
	s.mu.Unlock()
	return err
}

func (s *Store) SetAgentSessionsEnabled(enabled bool) error {
	s.mu.Lock()
	s.data.Experimental.AgentSessions = enabled
	err := s.save()
	s.mu.Unlock()
	return err
}

func (s *Store) SetAgentHooksDisabled(disabled bool) error {
	s.mu.Lock()
	s.data.Experimental.AgentHooksDisabled = disabled
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetCrons replaces the entire persisted cron list. Cron Manager calls this
// after every Save/Remove/Toggle/run-completion so the file reflects the
// current state.
func (s *Store) SetCrons(jobs []map[string]any) error {
	s.mu.Lock()
	s.data.Crons = jobs
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetTerminalWorkspace replaces the persisted terminal-canvas layout. raw is
// the frontend's serialized workspace JSON; nil/empty clears it.
func (s *Store) SetTerminalWorkspace(raw json.RawMessage) error {
	s.mu.Lock()
	s.data.TerminalWorkspace = raw
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetDocumentWorkspace clones the caller's reference-only layout for local storage.
func (s *Store) SetDocumentWorkspace(raw json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.DocumentWorkspace = slices.Clone(raw)
	return s.save()
}

func (s *Store) GetDocumentWorkspace() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data.DocumentWorkspace)
}

// GetTerminalWorkspace returns the persisted terminal-canvas layout JSON (or
// nil when none has been saved yet).
func (s *Store) GetTerminalWorkspace() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.TerminalWorkspace
}

// SetProjectPhp records the chosen PHP version for a single project.
// version="" removes the override (project will use the global default).
func (s *Store) SetProjectPhp(name, version string) error {
	s.mu.Lock()
	if s.data.Projects.PhpVersions == nil {
		s.data.Projects.PhpVersions = map[string]string{}
	}
	if version == "" {
		delete(s.data.Projects.PhpVersions, name)
	} else {
		s.data.Projects.PhpVersions[name] = version
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectNode records the chosen Node version for a single project.
// version="" removes the override (project will use the global default).
func (s *Store) SetProjectNode(name, version string) error {
	s.mu.Lock()
	if s.data.Projects.NodeVersions == nil {
		s.data.Projects.NodeVersions = map[string]string{}
	}
	if version == "" {
		delete(s.data.Projects.NodeVersions, name)
	} else {
		s.data.Projects.NodeVersions[name] = version
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectTags replaces the tag list for a single project. Tags are trimmed,
// empty strings dropped, and duplicates removed case-insensitively (first
// occurrence wins for casing). An empty resulting slice removes the entry.
func (s *Store) SetProjectTags(name string, tags []string) error {
	clean := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		clean = append(clean, t)
	}
	s.mu.Lock()
	if s.data.Projects.ProjectTags == nil {
		s.data.Projects.ProjectTags = map[string][]string{}
	}
	if len(clean) == 0 {
		delete(s.data.Projects.ProjectTags, name)
	} else {
		s.data.Projects.ProjectTags[name] = clean
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectProxyPort records the reverse-proxy target port for a project.
// port=0 removes the proxy (vhost reverts to standard PHP/static template).
func (s *Store) SetProjectProxyPort(name string, port int) error {
	s.mu.Lock()
	if s.data.Projects.ProxyPorts == nil {
		s.data.Projects.ProxyPorts = map[string]int{}
	}
	if port <= 0 {
		delete(s.data.Projects.ProxyPorts, name)
	} else {
		s.data.Projects.ProxyPorts[name] = port
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectQueueArgs records the last set of extra arguments passed to
// `php artisan queue:work` for a project. Empty string removes the entry
// (next start uses no extra args).
func (s *Store) SetProjectQueueArgs(name, args string) error {
	s.mu.Lock()
	if s.data.Projects.QueueArgs == nil {
		s.data.Projects.QueueArgs = map[string]string{}
	}
	args = strings.TrimSpace(args)
	if args == "" {
		delete(s.data.Projects.QueueArgs, name)
	} else {
		s.data.Projects.QueueArgs[name] = args
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// SetProjectDevCommand records a custom dev-server command for a project
// (run instead of the default `<pm> run dev`). Empty/whitespace removes the
// entry, reverting the project to the default behavior.
func (s *Store) SetProjectDevCommand(name, cmd string) error {
	s.mu.Lock()
	if s.data.Projects.DevCommands == nil {
		s.data.Projects.DevCommands = map[string]string{}
	}
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		delete(s.data.Projects.DevCommands, name)
	} else {
		s.data.Projects.DevCommands[name] = cmd
	}
	err := s.save()
	s.mu.Unlock()
	return err
}

// ProjectDevCommand resolves the dev command for a project:
// per-project override → global DefaultDevCommand → "" (caller runs the
// default `<pm> run dev`).
func (s *Store) ProjectDevCommand(name string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := strings.TrimSpace(s.data.Projects.DevCommands[name]); c != "" {
		return c
	}
	return strings.TrimSpace(s.data.Projects.DefaultDevCommand)
}

// SetDefaultPackageManager persists the user's preferred JS package manager
// for create-* template flows. Empty string falls back to npm.
func (s *Store) SetDefaultPackageManager(pm string) error {
	s.mu.Lock()
	s.data.Projects.DefaultPackageManager = pm
	err := s.save()
	s.mu.Unlock()
	return err
}

// Language is the chosen UI language code, or "" if the user never picked one.
func (s *Store) Language() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Language
}

// SetLanguage persists the UI language. Every LaraEnv process watching this
// file picks the change up from disk — see Watch.
func (s *Store) SetLanguage(code string) error {
	s.mu.Lock()
	s.data.Language = code
	err := s.save()
	s.mu.Unlock()
	return err
}

// Watch calls onChange whenever the config file changes underneath us, with
// the freshly parsed contents. It blocks until ctx is done.
//
// This exists because a LaraEnv install is several processes: the main window,
// each detached terminal window (--terminal-window) and the command bar
// (--command-bar) are separate executables, deliberately outside the
// single-instance lock. A Wails event reaches only the process that emitted
// it, so config.json is what they agree through.
//
// save() atomically replaces the file; directory events also observe external
// editor writes and replacements without following an obsolete file handle.
func (s *Store) Watch(ctx context.Context, onChange func(Config)) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer fw.Close()
	// Watch the directory, not the file: an editor that replaces config.json
	// would otherwise leave the watch pointing at a deleted inode.
	if err := fw.Add(filepath.Dir(s.path)); err != nil {
		return
	}

	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fw.Events:
			if !ok {
				return
			}
			if filepath.Clean(event.Name) != filepath.Clean(s.path) {
				continue
			}
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			// A single save can produce several events; coalesce them so we
			// re-read once and never mid-write.
			debounce = time.After(150 * time.Millisecond)
		case <-debounce:
			debounce = nil
			raw, err := os.ReadFile(s.path)
			if err != nil {
				continue
			}
			fresh, _, _, err := decodeDiskConfig(raw, s.passwords)
			if err != nil {
				continue
			}
			s.mu.Lock()
			same := s.data.Language == fresh.Language
			s.data.Language = fresh.Language
			s.mu.Unlock()
			if !same {
				onChange(fresh)
			}
		case _, ok := <-fw.Errors:
			if !ok {
				return
			}
		}
	}
}
