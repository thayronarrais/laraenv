package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSavedSSHPasswordIsProtectedOnDiskAndPreservedForCallers(t *testing.T) {
	s := newTestStore(t)
	const secret = "fixture ssh password with whitespace "
	h := SSHHost{Name: "fixture", Address: "fixture.invalid", User: "fixture", AuthMode: "password", Password: secret}
	if err := s.SetSSHHost(h); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte(`"password":`)) {
		t.Fatal("SSH password persisted as plaintext")
	}
	if !bytes.Contains(raw, []byte(`"passwordProtected":`)) {
		t.Fatal("protected credential is missing")
	}
	reloaded, err := NewStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get().Projects.SSHHosts[h.Name].Password; got != secret {
		t.Fatal("password changed during protected round trip")
	}
	encoded, err := json.Marshal(reloaded.ListSSHHosts()[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(secret)) || bytes.Contains(encoded, []byte("passwordProtected")) {
		t.Fatal("UI/cloud SSHHost JSON must retain the in-memory credential contract")
	}
}

type fixturePasswordProtector struct{ failProtect, failUnprotect bool }

func (p fixturePasswordProtector) Protect(value string) (string, error) {
	if p.failProtect {
		return "", errors.New("fixture protection failure")
	}
	return "protected-fixture:" + value, nil
}
func (p fixturePasswordProtector) Unprotect(value string) (string, error) {
	if p.failUnprotect {
		return "", errors.New("fixture belongs to another account")
	}
	if !bytes.HasPrefix([]byte(value), []byte("protected-fixture:")) {
		return "", errors.New("invalid fixture")
	}
	return value[len("protected-fixture:"):], nil
}

func TestLegacyProtectionFailureKeepsOriginalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"language":"pt-BR","projects":{"sshHosts":{"fixture":{"name":"fixture","password":"legacy-fixture"}}}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newStore(path, fixturePasswordProtector{failProtect: true}); err == nil {
		t.Fatal("legacy migration must surface protection failure")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("failed protection modified original legacy config")
	}
}

func TestUnavailablePasswordSurvivesCloudImportAndRecoversOnReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"language":"pt-BR","projects":{"sshHosts":{"fixture":{"name":"fixture","authMode":"password","passwordProtected":"opaque-foreign-account-blob"}}}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newStore(path, fixturePasswordProtector{failUnprotect: true})
	if err != nil {
		t.Fatal(err)
	}
	// A cloud/UI update lacking a credential must never erase a foreign blob.
	if err := s.SetSSHHost(SSHHost{Name: "fixture", AuthMode: "password", Address: "new-address.invalid"}); err == nil {
		t.Fatal("empty cloud import silently replaced unavailable password")
	}
	if s.Get().Language != "pt-BR" || !s.Get().Projects.SSHHosts["fixture"].PasswordUnavailable {
		t.Fatal("unavailable password prevented preserving other config")
	}
	// Key-only connections remain editable without entering an unused password.
	if err := s.SetSSHHost(SSHHost{Name: "fixture", AuthMode: "key", KeyName: "fixture-key", Address: "key-host.invalid"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte("opaque-foreign-account-blob")) {
		t.Fatal("key-only edit discarded protected password")
	}
	if err := s.SetSSHHost(SSHHost{Name: "fixture", AuthMode: "password", Password: "replacement-fixture"}); err != nil {
		t.Fatal(err)
	}
	if s.Get().Projects.SSHHosts["fixture"].PasswordUnavailable {
		t.Fatal("replacement password did not clear unavailable state")
	}
	reloaded, err := newStore(path, fixturePasswordProtector{})
	if err != nil || reloaded.Get().Projects.SSHHosts["fixture"].Password != "replacement-fixture" {
		t.Fatal("replacement credential was not reprotected for destination account")
	}
}

func TestFailedReplacementProtectionKeepsUnavailableBlob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"projects":{"sshHosts":{"fixture":{"name":"fixture","authMode":"password","passwordProtected":"opaque-foreign-account-blob"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newStore(path, fixturePasswordProtector{failUnprotect: true})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.passwords = fixturePasswordProtector{failProtect: true}
	if err := s.SetSSHHost(SSHHost{Name: "fixture", AuthMode: "password", Password: "replacement-fixture"}); err == nil {
		t.Fatal("protection failure must not fall back to plaintext")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("protection failure overwrote saved blob")
	}
	if !s.Get().Projects.SSHHosts["fixture"].PasswordUnavailable {
		t.Fatal("protection failure changed in-memory host state")
	}
}

func TestLegacySSHPasswordMigratesWithoutChangingOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"language":"pt-BR","projects":{"tld":"local","sshHosts":{"fixture":{"name":"fixture","user":"fixture","address":"fixture.invalid","authMode":"password","password":"legacy-fixture-only","tag":"PROD"}}}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Get().Language != "pt-BR" || s.Get().Projects.TLD != "local" || s.Get().Projects.SSHHosts["fixture"].Password != "legacy-fixture-only" {
		t.Fatal("legacy credential migration lost configuration")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("legacy-fixture-only")) || !bytes.Contains(raw, []byte("passwordProtected")) {
		t.Fatal("legacy password was not migrated on load")
	}
}

func TestUnreadableProtectedCredentialPreservesHostAndBlob(t *testing.T) {
	for _, suffix := range []string{"invalid", "", "AA=="} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			original := []byte(`{"projects":{"sshHosts":{"fixture":{"name":"fixture","passwordProtected":"dpapi:v1:` + suffix + `"}}}}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			s, err := NewStore(path)
			if err != nil {
				t.Fatal("an unavailable password must not prevent opening the app", err)
			}
			host := s.Get().Projects.SSHHosts["fixture"]
			if host.Password != "" {
				t.Fatal("unreadable protected data exposed as password")
			}
			encoded, err := json.Marshal(host)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded, []byte(`"passwordUnavailable":true`)) || bytes.Contains(encoded, []byte("dpapi:v1:")) {
				t.Fatal("unavailable credential needs safe actionable host state")
			}
			if err := s.SetProjectDevCommand("fixture", "fixture command"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var before, after struct {
				Projects struct {
					SSHHosts map[string]struct {
						PasswordProtected string `json:"passwordProtected"`
					} `json:"sshHosts"`
				} `json:"projects"`
			}
			if json.Unmarshal(original, &before) != nil || json.Unmarshal(got, &after) != nil || before.Projects.SSHHosts["fixture"].PasswordProtected != after.Projects.SSHHosts["fixture"].PasswordProtected {
				t.Fatal("unrelated save replaced unavailable credential blob")
			}
		})
	}
}

func TestFailedSSHSavePreservesPriorMemoryAndDisk(t *testing.T) {
	s := newTestStore(t)
	h := SSHHost{Name: "fixture", Password: "original-fixture-only"}
	if err := s.SetSSHHost(h); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(s.path)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := s.path
	s.path = filepath.Join(originalPath, "impossible-child")
	h.Password = "replacement-fixture-only"
	if err := s.SetSSHHost(h); err == nil {
		t.Fatal("save unexpectedly succeeded through a file parent")
	}
	if got := s.Get().Projects.SSHHosts[h.Name].Password; got != "original-fixture-only" {
		t.Fatal("failed save replaced the in-memory credential")
	}
	got, err := os.ReadFile(originalPath)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("failed save changed original config")
	}
}

func TestMalformedConfigMustNotBeReplacedByDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"projects":{"sshHosts":`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(path); err == nil {
		t.Fatal("malformed configuration must fail rather than discard credentials")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("malformed configuration was replaced")
	}
}

func TestReplacingProjectsKeepsUnavailableStateAndForgetsRemovedBlobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"projects":{"sshHosts":{"fixture":{"name":"fixture","authMode":"password","passwordProtected":"opaque-foreign-account-blob"}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newStore(path, fixturePasswordProtector{failUnprotect: true})
	if err != nil {
		t.Fatal(err)
	}
	prefs := s.Get().Projects
	host := prefs.SSHHosts["fixture"]
	host.PasswordUnavailable = false
	prefs.SSHHosts[host.Name] = host
	if err := s.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	if !s.Get().Projects.SSHHosts[host.Name].PasswordUnavailable {
		t.Fatal("replacing project preferences hid the unavailable password state")
	}
	prefs = s.Get().Projects
	delete(prefs.SSHHosts, host.Name)
	if err := s.SetProjects(prefs); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSSHHost(SSHHost{Name: host.Name, AuthMode: "password"}); err != nil {
		t.Fatal("deleted host's credential affected a new host with the same name", err)
	}
	if s.Get().Projects.SSHHosts[host.Name].PasswordUnavailable {
		t.Fatal("deleted protected blob was resurrected")
	}
}

func TestClearingAvailablePasswordRemovesProtectedDiskValue(t *testing.T) {
	s := newTestStore(t)
	h := SSHHost{Name: "fixture", AuthMode: "password", Password: "fixture-only"}
	if err := s.SetSSHHost(h); err != nil {
		t.Fatal(err)
	}
	h.Password = ""
	if err := s.SetSSHHost(h); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.path)
	if err != nil || bytes.Contains(raw, []byte("passwordProtected")) || bytes.Contains(raw, []byte(`"password":`)) {
		t.Fatal("clearing the saved password left a credential on disk")
	}
}
