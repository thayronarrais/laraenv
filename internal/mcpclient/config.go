// Package mcpclient registers the stdio bridge without exposing its bearer token.
package mcpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
	"lara4ever/internal/atomicfile"
)

type Registration struct{ Executable, Connection string }

var configMu sync.Mutex

type Status struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Path            string `json:"path"`
	Registered      bool   `json:"registered"`
	Error           string `json:"error,omitempty"`
	RestartRequired bool   `json:"restartRequired"`
}

func DefaultPaths(home string, getenv func(string) string) map[string]string {
	codexHome := getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	return map[string]string{"claude": filepath.Join(home, ".claude.json"), "codex": filepath.Join(codexHome, "config.toml"), "cursor": filepath.Join(home, ".cursor", "mcp.json")}
}

func label(id string) string {
	switch id {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "cursor":
		return "Cursor"
	}
	return id
}
func key(id string) (string, error) {
	switch id {
	case "claude", "cursor":
		return "mcpServers", nil
	case "codex":
		return "mcp_servers", nil
	}
	return "", errors.New("unknown MCP client")
}
func entry(id string, r Registration) map[string]any {
	out := map[string]any{"command": r.Executable, "args": []string{"mcp", "--connection", r.Connection}}
	if id != "codex" {
		out["type"] = "stdio"
	}
	return out
}

func read(path, id string) ([]byte, map[string]any, error) {
	if _, err := key(id); err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, map[string]any{}, nil
	}
	if err != nil {
		return nil, nil, errors.New("cannot read client configuration")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err != nil || len(raw) > 4*1024*1024 {
		return nil, nil, errors.New("client configuration exceeds the 4 MB limit")
	}
	doc := map[string]any{}
	if id == "codex" {
		err = toml.Unmarshal(raw, &doc)
	} else {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		err = dec.Decode(&doc)
		if err == nil {
			var extra any
			if dec.Decode(&extra) != io.EOF {
				err = errors.New("extra JSON content")
			}
		}
	}
	if err != nil || doc == nil {
		return nil, nil, errors.New("invalid client configuration; existing file was preserved")
	}
	k, _ := key(id)
	if s, ok := doc[k]; ok {
		if _, ok := s.(map[string]any); !ok {
			return nil, nil, errors.New("invalid MCP server configuration; existing file was preserved")
		}
	}
	return raw, doc, nil
}

// Inspect is read-only, including when the client or its configuration is absent.
func Inspect(path, id string, r Registration) Status {
	configMu.Lock()
	defer configMu.Unlock()
	s := Status{ID: id, Label: label(id), Path: path}
	_, doc, err := read(path, id)
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.Registered = isRegistered(doc, id, r)
	return s
}

func isRegistered(doc map[string]any, id string, r Registration) bool {
	k, _ := key(id)
	servers, _ := doc[k].(map[string]any)
	existing, _ := servers["laraenv"].(map[string]any)
	if existing == nil {
		return false
	}
	expected := entry(id, r)
	// Normalize []interface{} from decoders and []string from our registration.
	a, _ := json.Marshal(existing)
	b, _ := json.Marshal(expected)
	var x, y any
	json.Unmarshal(a, &x)
	json.Unmarshal(b, &y)
	return reflect.DeepEqual(x, y)
}

// Register replaces only the named server, preserving other configuration values
// and keeping an exact backup before an atomic write. Invalid files are untouched.
func Register(path, id string, r Registration) error {
	configMu.Lock()
	defer configMu.Unlock()
	if !filepath.IsAbs(r.Executable) || !filepath.IsAbs(r.Connection) {
		return errors.New("MCP registration requires absolute installation paths")
	}
	raw, doc, err := read(path, id)
	if err != nil {
		return err
	}
	if isRegistered(doc, id, r) {
		return nil
	}
	k, _ := key(id)
	servers, _ := doc[k].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
		doc[k] = servers
	}
	servers["laraenv"] = entry(id, r)
	var data []byte
	if id == "codex" {
		data, err = toml.Marshal(doc)
	} else {
		data, err = json.MarshalIndent(doc, "", "  ")
		data = append(data, '\n')
	}
	if err != nil {
		return errors.New("cannot encode client configuration")
	}
	if raw != nil {
		backup := fmt.Sprintf("%s.laraenv-%s.bak", path, time.Now().UTC().Format("20060102T150405.000000000"))
		f, e := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return errors.New("cannot back up client configuration")
		}
		_, e = f.Write(raw)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return errors.New("cannot back up client configuration")
		}
	}
	// Detect an external client write between our read/backup and replacement.
	latest, e := os.ReadFile(path)
	if e != nil && !os.IsNotExist(e) {
		return errors.New("cannot verify client configuration")
	}
	if !bytes.Equal(latest, raw) {
		return errors.New("client configuration changed; retry registration")
	}
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return errors.New("cannot save client configuration")
	}
	return nil
}
