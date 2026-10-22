package mcpclient

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestRegisterPreservesExistingClientConfiguration(t *testing.T) {
	for _, id := range []string{"claude", "codex", "cursor"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config")
			original := []byte(`{"theme":"dark","projects":{"demo":{"allowed":true}},"mcpServers":{"other":{"command":"other.exe"}}}`)
			if id == "codex" {
				original = []byte("# original comment\nmodel = \"test\"\n[mcp_servers.other]\ncommand = \"other.exe\"\n")
			}
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			r := Registration{Executable: filepath.Join(dir, "LaraEnv.exe"), Connection: filepath.Join(dir, "connection.json")}
			if err := Register(path, id, r); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if id == "codex" {
				err = toml.Unmarshal(got, &doc)
			} else {
				err = json.Unmarshal(got, &doc)
			}
			if err != nil {
				t.Fatal(err)
			}
			key := "mcpServers"
			if id == "codex" {
				key = "mcp_servers"
				if doc["model"] != "test" {
					t.Fatal("lost model")
				}
			} else if doc["theme"] != "dark" {
				t.Fatal("lost preferences")
			}
			servers := doc[key].(map[string]any)
			if _, ok := servers["other"]; !ok {
				t.Fatal("lost other MCP server")
			}
			status := Inspect(path, id, r)
			if !status.Registered || status.Error != "" {
				t.Fatalf("status: %+v", status)
			}
			backups, _ := filepath.Glob(path + ".laraenv-*.bak")
			if len(backups) != 1 {
				t.Fatalf("backups=%v", backups)
			}
			backup, _ := os.ReadFile(backups[0])
			if string(backup) != string(original) {
				t.Fatal("backup changed bytes")
			}
			if err := Register(path, id, r); err != nil {
				t.Fatal(err)
			}
			again, _ := os.ReadFile(path)
			if string(got) != string(again) {
				t.Fatal("registration not idempotent")
			}
			backups, _ = filepath.Glob(path + ".laraenv-*.bak")
			if len(backups) != 1 {
				t.Fatal("idempotent registration made another backup")
			}
		})
	}
}

func TestRegistrationPreservesNestedValuesAndUsesLiteralPaths(t *testing.T) {
	for _, id := range []string{"claude", "codex", "cursor"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "configuration")
			original := []byte(`{"counter":9007199254740993,"projects":{"D:\\Checkout Dir":{"trusted":true,"tags":["a",null,3]}},"mcpServers":{"other":{"command":"other.exe","env":{"PREFERENCE":"keep"},"args":["a & b"]}}}`)
			if id == "codex" {
				original = []byte("# keep this exact comment in the backup\nmodel = 'chosen'\ncreated = 2026-10-06\n[projects.'D:\\Checkout Dir']\ntrust_level = 'trusted'\ntags = ['a', 'b']\n[mcp_servers.other]\ncommand = 'other.exe'\nargs = ['a & b']\n[mcp_servers.other.env]\nPREFERENCE = 'keep'\n")
			}
			decode := func(raw []byte) map[string]any {
				var doc map[string]any
				var err error
				if id == "codex" {
					err = toml.Unmarshal(raw, &doc)
				} else {
					decoder := json.NewDecoder(bytes.NewReader(raw))
					decoder.UseNumber()
					err = decoder.Decode(&doc)
				}
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}
			before := decode(original)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			r := Registration{Executable: filepath.Join(dir, "Lara Env", "LaraEnv.exe"), Connection: filepath.Join(dir, "Local Cache", "connection.json")}
			if err := Register(path, id, r); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after := decode(saved)
			k, _ := key(id)
			servers := after[k].(map[string]any)
			managed := servers["laraenv"].(map[string]any)
			if managed["command"] != r.Executable {
				t.Fatalf("quoted or changed executable: %#v", managed)
			}
			args := managed["args"].([]any)
			if len(args) != 3 || args[0] != "mcp" || args[1] != "--connection" || args[2] != r.Connection {
				t.Fatalf("paths are not literal argv: %#v", args)
			}
			delete(servers, "laraenv")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("unrelated semantic values changed: before=%#v after=%#v", before, after)
			}
			backups, _ := filepath.Glob(path + ".laraenv-*.bak")
			if len(backups) != 1 {
				t.Fatalf("backups: %v", backups)
			}
			backup, err := os.ReadFile(backups[0])
			if err != nil || !bytes.Equal(backup, original) {
				t.Fatal("commented configuration was not backed up exactly", err)
			}
		})
	}
}

func TestConcurrentRegistrationAndInspectionNeverCorruptsClientConfiguration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"preferences":{"keep":true},"mcpServers":{"other":{"command":"other.exe"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := Registration{Executable: filepath.Join(dir, "LaraEnv.exe"), Connection: filepath.Join(dir, "connection.json")}
	var workers sync.WaitGroup
	errors := make(chan error, 12)
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			_ = Inspect(path, "cursor", r)
			errors <- Register(path, "cursor", r)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	successful := 0
	for err := range errors {
		if err == nil {
			successful++
		} else if !strings.Contains(err.Error(), "changed; retry registration") {
			t.Error(err)
		}
	}
	if successful == 0 {
		t.Fatal("no registration succeeded")
	}
	if s := Inspect(path, "cursor", r); !s.Registered || s.Error != "" {
		t.Fatalf("final registration: %+v", s)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["preferences"].(map[string]any)["keep"] != true || doc["mcpServers"].(map[string]any)["other"].(map[string]any)["command"] != "other.exe" {
		t.Fatal("concurrent registration lost unrelated values")
	}
}

func TestInvalidConfigurationRemainsUntouched(t *testing.T) {
	for _, tc := range []struct{ id, data string }{{"claude", "{broken"}, {"cursor", `{"mcpServers":[]}`}, {"codex", "[bad\n"}, {"codex", "mcp_servers = 'bad'"}} {
		t.Run(tc.id+tc.data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			os.WriteFile(path, []byte(tc.data), 0600)
			if err := Register(path, tc.id, Registration{Executable: filepath.Join(t.TempDir(), "app.exe"), Connection: filepath.Join(t.TempDir(), "connection.json")}); err == nil {
				t.Fatal("accepted invalid configuration")
			}
			got, _ := os.ReadFile(path)
			if string(got) != tc.data {
				t.Fatal("invalid file modified")
			}
			if Inspect(path, tc.id, Registration{}).Error == "" {
				t.Fatal("status hides malformed configuration")
			}
		})
	}
}

func TestInspectDoesNotCreateFilesAndPathsRespectCodexHome(t *testing.T) {
	home := t.TempDir()
	paths := DefaultPaths(home, func(key string) string {
		if key == "CODEX_HOME" {
			return filepath.Join(home, "custom-codex")
		}
		return ""
	})
	if paths["codex"] != filepath.Join(home, "custom-codex", "config.toml") {
		t.Fatal(paths)
	}
	for id, path := range paths {
		s := Inspect(path, id, Registration{})
		if s.Registered || s.Error != "" {
			t.Fatal(s)
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) && id != "claude" {
			t.Fatal("inspection created a directory")
		}
	}
}
