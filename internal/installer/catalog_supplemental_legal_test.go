package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func catalogNoticeFixture(t *testing.T, archiveHash string) fstest.MapFS {
	t.Helper()
	pecl := noticeFixture(t, archiveHash)
	var manifest map[string]any
	if err := json.Unmarshal(pecl["pecl_notices/manifest.json"].Data, &manifest); err != nil {
		t.Fatal(err)
	}
	bundle := manifest["bundles"].([]any)[0].(map[string]any)
	delete(bundle, "extension")
	delete(bundle, "phpSeries")
	bundle["id"], bundle["kind"], bundle["version"] = "redis-fixture", "redis", "5.0.14.1"
	bundle["archiveUrl"] = "https://github.com/tporadowski/redis/releases/download/v5.0.14.1/Redis-x64-5.0.14.1.zip"
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"runtime_notices/manifest.json": &fstest.MapFile{Data: data}, "runtime_notices/companion-NOTICE.txt": pecl["pecl_notices/companion-NOTICE.txt"]}
}

func TestCatalogSupplementalLegalRetainsLargeReviewedNoticeSet(t *testing.T) {
	const hash = "018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf"
	registry := catalogNoticeFixture(t, hash)
	var manifest map[string]any
	if err := json.Unmarshal(registry["runtime_notices/manifest.json"].Data, &manifest); err != nil {
		t.Fatal(err)
	}
	bundle := manifest["bundles"].([]any)[0].(map[string]any)
	template := bundle["notices"].([]any)[0].(map[string]any)
	notices := []any{}
	for index := 0; index < 100; index++ {
		notice := make(map[string]any)
		for key, value := range template {
			notice[key] = value
		}
		name := fmt.Sprintf("notice-%03d.txt", index)
		notice["path"] = name
		notice["sourceEvidence"] = map[string]any{"sourceSha256": hash, "byteRanges": []any{map[string]int{"start": 0, "endExclusive": 42}}}
		registry["runtime_notices/"+name] = registry["runtime_notices/companion-NOTICE.txt"]
		notices = append(notices, notice)
	}
	bundle["notices"] = notices
	registry["runtime_notices/manifest.json"].Data, _ = json.Marshal(manifest)
	files, records, err := collectCatalogSupplementalLegal(registry, KindRedis, "5.0.14.1", hash)
	if err != nil || len(files) != 100 || len(records) != 100 {
		t.Fatalf("lost reviewed notices: %d %d %v", len(files), len(records), err)
	}
	if !bytes.Contains(records[0].SourceEvidence, []byte(hash)) {
		t.Fatal("lost source evidence while retaining notice")
	}
}

func TestCatalogSupplementalLegalIsBoundToKindVersionAndArchive(t *testing.T) {
	const hash = "018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf"
	registry := catalogNoticeFixture(t, hash)
	files, records, err := collectCatalogSupplementalLegal(registry, KindRedis, "5.0.14.1", hash)
	if err != nil || len(files) != 1 || len(records) != 1 {
		t.Fatalf("missing exact Redis notices: %v", err)
	}
	for _, candidate := range []struct {
		kind          Kind
		version, hash string
	}{{KindPHP, "5.0.14.1", hash}, {KindRedis, "5.0.14.2", hash}} {
		if _, _, err := collectCatalogSupplementalLegal(registry, candidate.kind, candidate.version, candidate.hash); err == nil {
			t.Fatal("accepted an incompatible known fingerprint scope")
		}
	}
	files, records, err = collectCatalogSupplementalLegal(registry, KindRedis, "5.0.14.1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(files) != 0 || len(records) != 0 {
		t.Fatal("unknown archive got supplemental notices")
	}
	if _, _, err := collectPeclSupplementalLegal(registry, "redis", hash); err == nil {
		t.Fatal("mixed catalog and PECL registry roots")
	}
}

func TestCatalogSupplementalLegalPreservesExistingManifestsAndData(t *testing.T) {
	root := t.TempDir()
	source := Source{Kind: KindRedis, Version: "5.0.14.1", InstallTo: root}
	if err := preserveSupplementalLegal(source); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "licenses/laraenv/redis/5.0.14.1/manifest.json")
	old, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte("existing private settings"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := legalZip(t, legalZipEntry{name: "redis-server.exe", body: "fixture binary, never executed"})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	registry := catalogNoticeFixture(t, hash)
	manager := NewManager(t.TempDir(), func(Progress) {})
	for attempt := 0; attempt < 2; attempt++ {
		if err := manager.extractWithCatalogNotices(source, archive, registry); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if current, _ := os.ReadFile(oldPath); !bytes.Equal(current, old) {
		t.Fatal("modified existing legacy manifest")
	}
	if current, _ := os.ReadFile(filepath.Join(root, "config.json")); string(current) != "existing private settings" {
		t.Fatal("modified existing user data")
	}
	legalRoot := filepath.Join(root, "licenses/laraenv/redis/5.0.14.1/supplemental/redis-fixture")
	if current, err := os.ReadFile(filepath.Join(legalRoot, "companion-NOTICE.txt")); err != nil || string(current) != "Exact companion notice\r\nCopyright Example\r\n" {
		t.Fatalf("notice bytes changed: %v", err)
	}
	var manifest struct {
		Kind          string                   `json:"kind"`
		Version       string                   `json:"version"`
		ArchiveSHA256 string                   `json:"archiveSha256"`
		Files         []retainedPeclSupplement `json:"files"`
	}
	encoded, err := os.ReadFile(filepath.Join(legalRoot, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != "redis" || manifest.Version != "5.0.14.1" || manifest.ArchiveSHA256 != hash || len(manifest.Files) != 1 || manifest.Files[0].SourceURL == "" {
		t.Fatalf("missing evidence scope: %s", encoded)
	}
}

func TestCatalogSupplementalLegalFailurePrecedesPayloadWrites(t *testing.T) {
	root := t.TempDir()
	source := Source{Kind: KindRedis, Version: "5.0.14.1", InstallTo: root}
	archive := legalZip(t, legalZipEntry{name: "redis-server.exe", body: "replacement fixture, never executed"})
	data, _ := os.ReadFile(archive)
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	registry := catalogNoticeFixture(t, hash)
	registry["runtime_notices/companion-NOTICE.txt"].Data = []byte("tampered notice")
	if err := os.WriteFile(filepath.Join(root, "redis-server.exe"), []byte("existing installation"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := NewManager(t.TempDir(), func(Progress) {}).extractWithCatalogNotices(source, archive, registry); err == nil {
		t.Fatal("accepted corrupted legal evidence")
	}
	if current, _ := os.ReadFile(filepath.Join(root, "redis-server.exe")); string(current) != "existing installation" {
		t.Fatal("modified payload before legal preflight")
	}
}

func TestEmbeddedCatalogSupplementalNoticesMatchReviewedHashes(t *testing.T) {
	files, records, err := collectCatalogSupplementalLegal(catalogNoticeRegistry, KindRedis, "5.0.14.1", "018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf")
	if err != nil || len(files) != 100 || len(records) != 100 {
		t.Fatalf("missing reviewed Redis notices: %d %d %v", len(files), len(records), err)
	}
	for _, record := range records {
		if len(record.SourceEvidence) == 0 {
			t.Fatalf("missing source provenance: %s", record.Path)
		}
	}
}
