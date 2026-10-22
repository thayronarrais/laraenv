package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"lara4ever/internal/atomicfile"
	"lara4ever/internal/mcpbridge"
	"lara4ever/internal/mcpclient"
	"lara4ever/internal/paths"
	"lara4ever/internal/version"
)

type MCPActivity struct {
	ID         string `json:"id"`
	Time       string `json:"time"`
	Group      string `json:"group"`
	Action     string `json:"action"`
	Target     string `json:"target"`
	Success    bool   `json:"success"`
	DurationMS int64  `json:"durationMs"`
}
type MCPStatus struct {
	Enabled  bool               `json:"enabled"`
	Running  bool               `json:"running"`
	Message  string             `json:"message,omitempty"`
	Clients  []mcpclient.Status `json:"clients"`
	Activity []MCPActivity      `json:"activity"`
}

func mcpConnectionPath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir, _ = os.UserHomeDir()
	}
	root := filepath.Clean(paths.Root())
	sum := sha256.Sum256([]byte(root))
	return filepath.Join(dir, "LaraEnv", "mcp", hex.EncodeToString(sum[:8]), "connection.json")
}

func mcpCLIConnection(args []string) (string, error) {
	if len(args) == 2 {
		return mcpConnectionPath(), nil
	}
	if len(args) == 4 && args[2] == "--connection" && filepath.IsAbs(args[3]) {
		return filepath.Clean(args[3]), nil
	}
	return "", errors.New("usage: LaraEnv.exe mcp [--connection <absolute descriptor path>]")
}

func mcpRegistration() (mcpclient.Registration, error) {
	exe, err := os.Executable()
	if err != nil {
		return mcpclient.Registration{}, err
	}
	return mcpclient.Registration{Executable: exe, Connection: mcpConnectionPath()}, nil
}
func mcpClientPaths() map[string]string {
	home, _ := os.UserHomeDir()
	return mcpclient.DefaultPaths(home, os.Getenv)
}

func (a *App) GetMCPActivity() []MCPActivity {
	a.mcpActivityMu.Lock()
	defer a.mcpActivityMu.Unlock()
	return append([]MCPActivity{}, a.mcpActivity...)
}
func (a *App) GetMCPStatus() MCPStatus {
	a.mcpMu.Lock()
	s := MCPStatus{Enabled: a.cfg.Get().Experimental.MCPEnabled, Message: a.mcpMessage, Clients: []mcpclient.Status{}}
	if a.mcpServer != nil {
		s.Running = a.mcpServer.Status().Running
	}
	a.mcpMu.Unlock()
	r, err := mcpRegistration()
	p := mcpClientPaths()
	for _, id := range []string{"claude", "codex", "cursor"} {
		c := mcpclient.Inspect(p[id], id, r)
		if err != nil {
			c.Error = "cannot resolve LaraEnv executable"
		}
		s.Clients = append(s.Clients, c)
	}
	s.Activity = a.GetMCPActivity()
	return s
}

func (a *App) RegisterMCPClient(id string) (mcpclient.Status, error) {
	// Registration is only triggered by the explicit client button in Settings.
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	p, ok := mcpClientPaths()[id]
	if !ok {
		return mcpclient.Status{}, errors.New("unknown MCP client")
	}
	r, err := mcpRegistration()
	if err != nil {
		return mcpclient.Status{}, err
	}
	if err = mcpclient.Register(p, id, r); err != nil {
		return mcpclient.Status{}, err
	}
	s := mcpclient.Inspect(p, id, r)
	s.RestartRequired = true
	return s, nil
}

func (a *App) SetMCPEnabled(enabled bool) (MCPStatus, error) {
	a.mcpMu.Lock()
	if a.detached || a.commandBar || !a.mcpReady {
		a.mcpMu.Unlock()
		return a.GetMCPStatus(), errors.New("open the main LaraEnv window before enabling MCP")
	}
	if enabled {
		if err := a.ensureMCPLocked(); err != nil {
			a.mcpMessage = "could not start local MCP access"
			a.mcpMu.Unlock()
			return a.GetMCPStatus(), err
		}
		if err := a.cfg.SetMCPEnabled(true); err != nil {
			a.mcpServer.Close()
			a.mcpServer = nil
			a.mcpMu.Unlock()
			return a.GetMCPStatus(), err
		}
	} else {
		if err := a.cfg.SetMCPEnabled(false); err != nil {
			a.mcpMu.Unlock()
			return a.GetMCPStatus(), err
		}
		if a.mcpServer != nil {
			a.mcpServer.Close()
			a.mcpServer = nil
		}
	}
	a.mcpMessage = ""
	a.mcpMu.Unlock()
	s := a.GetMCPStatus()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "mcp:status-changed", s)
	}
	return s, nil
}

func (a *App) ensureMCPLocked() error {
	if a.mcpServer != nil && a.mcpServer.Status().Running {
		return nil
	}
	server, err := mcpbridge.NewServer(mcpbridge.Options{Backend: appMCPBackend{a}, DescriptorPath: mcpConnectionPath(), Version: version.Version, Audit: a.recordMCPActivity})
	if err != nil {
		return err
	}
	if err = server.Start(a.ctx); err != nil {
		return err
	}
	a.mcpServer = server
	return nil
}
func (a *App) startMCP() {
	a.mcpActivityMu.Lock()
	if raw, err := os.ReadFile(filepath.Join(paths.StateRoot(), "mcp-activity.json")); err == nil && len(raw) < 128*1024 {
		json.Unmarshal(raw, &a.mcpActivity)
		if len(a.mcpActivity) > 100 {
			a.mcpActivity = a.mcpActivity[len(a.mcpActivity)-100:]
		}
	}
	a.mcpActivityMu.Unlock()
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	a.mcpReady = true
	if a.cfg.Get().Experimental.MCPEnabled {
		if err := a.ensureMCPLocked(); err != nil {
			a.mcpMessage = "could not start local MCP access"
		}
	}
}
func (a *App) stopMCP() {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	a.mcpReady = false
	if a.mcpServer != nil {
		a.mcpServer.Close()
		a.mcpServer = nil
	}
}
func (a *App) recordMCPActivity(e mcpbridge.AuditEvent) {
	target := e.Project
	if target == "" {
		target = e.Service
	}
	if target == "" {
		target = e.Kind
	}
	if e.Database != "" {
		target = e.Engine + " / " + e.Database
	}
	if e.JobID != "" {
		target = e.JobID
	}
	row := MCPActivity{ID: e.ID, Time: e.StartedAt.UTC().Format(time.RFC3339), Group: e.Group, Action: e.Action, Target: target, Success: e.Success, DurationMS: e.DurationMS}
	a.mcpActivityMu.Lock()
	a.mcpActivity = append(a.mcpActivity, row)
	if len(a.mcpActivity) > 100 {
		a.mcpActivity = a.mcpActivity[len(a.mcpActivity)-100:]
	}
	raw, err := json.Marshal(a.mcpActivity)
	if err == nil {
		_ = atomicfile.Write(filepath.Join(paths.StateRoot(), "mcp-activity.json"), raw, 0600)
	}
	a.mcpActivityMu.Unlock()
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "mcp:activity", row)
	}
}
