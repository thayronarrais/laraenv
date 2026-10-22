package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestSupplementalRuntimeLegal(t *testing.T) {
	for _, fixture := range []struct {
		kind                    Kind
		version, notice, digest string
	}{
		{KindPgvector, "0.8.6-pg18", "pgvector-LICENSE.txt", "6bba9ebeb73e27477463b05e5ef1bf303bccbddb3db9bbc95905d351604d6a87"},
		{KindRedis, "5.0.14.1", "redis-license.txt", "83c2b0e936dbb2da90b25fd2cb41d2f53d8d13ef05772d636f526767c3f61afb"},
	} {
		t.Run(string(fixture.kind), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("existing host terms"), 0644); err != nil {
				t.Fatal(err)
			}
			archive := legalZip(t, legalZipEntry{name: "payload.dll", body: "fixture; never executed"})
			manager := NewManager(t.TempDir(), func(Progress) {})
			source := Source{Kind: fixture.kind, Version: fixture.version, InstallTo: root}
			if err := manager.InstallFromFile(source, archive); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(root, "licenses/laraenv", string(fixture.kind), fixture.version)
			data, err := os.ReadFile(filepath.Join(directory, fixture.notice))
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(data)
			if hex.EncodeToString(hash[:]) != fixture.digest {
				t.Fatal("supplemental upstream notice changed")
			}
			if _, err := os.ReadFile(filepath.Join(directory, "manifest.json")); err != nil {
				t.Fatal(err)
			}
			if bytes, _ := os.ReadFile(filepath.Join(root, "LICENSE")); string(bytes) != "existing host terms" {
				t.Fatal("host license overwritten")
			}
			if err := manager.InstallFromFile(source, archive); err != nil {
				t.Fatalf("reinstall not idempotent: %v", err)
			}
		})
	}
}

func TestSupplementalLegalDoesNotClaimOtherVersions(t *testing.T) {
	root := t.TempDir()
	archive := legalZip(t, legalZipEntry{name: "payload.dll", body: "fixture"})
	manager := NewManager(t.TempDir(), func(Progress) {})
	if err := manager.InstallFromFile(Source{Kind: KindRedis, Version: "99.0.0", InstallTo: root}, archive); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "licenses/laraenv")); !os.IsNotExist(err) {
		t.Fatal("notice applied to unreviewed version")
	}
}

func TestRuntimeArchiveCannotReplaceRetainedLegal(t *testing.T) {
	for _, name := range []string{"licenses/laraenv/redis/5.0.14.1/redis-license.txt", "licenses/laraenv/redis/5.0.14.1/manifest.json", `LICENSES.\LARAENV\redis\5.0.14.1\redis-license.txt`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			payload := filepath.Join(root, "payload.dll")
			if err := os.WriteFile(payload, []byte("existing"), 0644); err != nil {
				t.Fatal(err)
			}
			archive := legalZip(t, legalZipEntry{name: "payload.dll", body: "replacement"}, legalZipEntry{name: name, body: "replace retained evidence"})
			manager := NewManager(t.TempDir(), func(Progress) {})
			if err := manager.InstallFromFile(Source{Kind: KindRedis, Version: "5.0.14.1", InstallTo: root}, archive); err == nil {
				t.Fatal("reserved legal namespace accepted")
			}
			if data, _ := os.ReadFile(payload); string(data) != "existing" {
				t.Fatal("payload changed before archive preflight")
			}
		})
	}
}
