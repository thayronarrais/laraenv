package installer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func noticeFixture(t *testing.T, archiveHash string) fstest.MapFS {
	t.Helper()
	data := []byte("Exact companion notice\r\nCopyright Example\r\n")
	sum := sha256.Sum256(data)
	manifest := map[string]any{"schemaVersion": 1, "bundles": []any{map[string]any{
		"id": "mongodb-fixture", "extension": "mongodb", "version": "1.20.1", "phpSeries": "8.4",
		"archiveUrl": "https://windows.php.net/fixture.zip", "archiveSha256": archiveHash,
		"scope":   "supplemental-notices-not-complete-binary-clearance",
		"notices": []any{map[string]any{"path": "companion-NOTICE.txt", "sha256": hex.EncodeToString(sum[:]), "bytes": len(data), "sourceUrl": "https://example.org/version/NOTICE", "licenseDescription": "Fixture notice; no blanket license claim"}},
	}}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"pecl_notices/manifest.json": &fstest.MapFile{Data: encoded}, "pecl_notices/companion-NOTICE.txt": &fstest.MapFile{Data: data}}
}

func TestPeclSupplementalLegalUsesExactArchiveAndExtension(t *testing.T) {
	const hash = "763efd7317b1153729b39031433fab008ad1c42fd46890dfa0db9765494bc588"
	registry := noticeFixture(t, hash)
	files, records, err := collectPeclSupplementalLegal(registry, "mongodb", hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(records) != 1 || string(files["supplemental/companion-NOTICE.txt"]) != "Exact companion notice\r\nCopyright Example\r\n" {
		t.Fatalf("missing exact supplemental data: %v %v", files, records)
	}
	if records[0].SourceURL != "https://example.org/version/NOTICE" || records[0].Scope != "supplemental-notices-not-complete-binary-clearance" {
		t.Fatalf("missing attribution/scope: %+v", records[0])
	}
	files, records, err = collectPeclSupplementalLegal(registry, "mongodb", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || len(files) != 0 || len(records) != 0 {
		t.Fatalf("unknown archive got notices: %v %v", files, err)
	}
	if _, _, err = collectPeclSupplementalLegal(registry, "redis", hash); err == nil {
		t.Fatal("accepted different extension for exact known archive")
	}
}

func TestPeclSupplementalLegalRejectsTamperingAndUnsafeMetadata(t *testing.T) {
	const hash = "763efd7317b1153729b39031433fab008ad1c42fd46890dfa0db9765494bc588"
	for _, change := range []func(fstest.MapFS){
		func(reg fstest.MapFS) { reg["pecl_notices/companion-NOTICE.txt"].Data = []byte("changed") },
		func(reg fstest.MapFS) { delete(reg, "pecl_notices/companion-NOTICE.txt") },
		func(reg fstest.MapFS) {
			var doc map[string]any
			_ = json.Unmarshal(reg["pecl_notices/manifest.json"].Data, &doc)
			notices := doc["bundles"].([]any)[0].(map[string]any)["notices"].([]any)
			notices[0].(map[string]any)["path"] = "../NOTICE.txt"
			reg["pecl_notices/manifest.json"].Data, _ = json.Marshal(doc)
		},
	} {
		reg := noticeFixture(t, hash)
		change(reg)
		if _, _, err := collectPeclSupplementalLegal(reg, "mongodb", hash); err == nil {
			t.Fatal("accepted invalid supplemental evidence")
		}
	}
}

func TestPeclSupplementalLegalIsWrittenWithOriginalArchiveManifest(t *testing.T) {
	archive := legalZip(t, legalZipEntry{name: "php_mongodb.dll", body: "fixture DLL, never executed"}, legalZipEntry{name: "LICENSE", body: "Original upstream license\r\n"})
	bytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	hash := hex.EncodeToString(sum[:])
	registry := noticeFixture(t, hash)
	root := t.TempDir()
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	input, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	// Recreate a notice bundle retained before supplemental support existed.
	baselineRegistry := fstest.MapFS{"pecl_notices/manifest.json": &fstest.MapFile{Data: []byte(`{"schemaVersion":1,"bundles":[]}`)}}
	if err := preservePeclLegalWithNotices(reader.File, input, "mongodb", root, baselineRegistry); err != nil {
		t.Fatal(err)
	}
	oldManifest, err := os.ReadFile(filepath.Join(root, "licenses", "pecl", "mongodb", hash, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := preservePeclLegalWithNotices(reader.File, input, "mongodb", root, registry); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "licenses", "pecl", "mongodb", hash)
	for path, expected := range map[string]string{"files/LICENSE": "Original upstream license\r\n", "supplemental/companion-NOTICE.txt": "Exact companion notice\r\nCopyright Example\r\n"} {
		data, err := os.ReadFile(filepath.Join(bundle, filepath.FromSlash(path)))
		if err != nil || string(data) != expected {
			t.Fatalf("%s was not retained exactly: %q %v", path, data, err)
		}
	}
	var manifest struct {
		Status       string `json:"status"`
		Supplemental []struct {
			SHA256    string `json:"sha256"`
			SourceURL string `json:"sourceUrl"`
			Scope     string `json:"scope"`
		} `json:"files"`
	}
	currentManifest, _ := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if string(currentManifest) != string(oldManifest) {
		t.Fatal("changed an existing archive manifest")
	}
	data, _ := os.ReadFile(filepath.Join(bundle, "supplemental", "manifest.json"))
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "supplemental-source-notices-not-binary-clearance" || len(manifest.Supplemental) != 1 || manifest.Supplemental[0].SourceURL == "" || manifest.Supplemental[0].Scope == "" {
		t.Fatalf("missing scoped supplemental manifest: %s", data)
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := preservePeclLegalWithNotices(reader.File, input, "mongodb", root, registry); err != nil {
		t.Fatalf("supplemental retry was not idempotent: %v", err)
	}
}

func TestEmbeddedPeclSupplementalNoticesMatchReviewedHashes(t *testing.T) {
	for _, row := range []struct {
		extension, hash string
		count           int
	}{
		{"mongodb", "763efd7317b1153729b39031433fab008ad1c42fd46890dfa0db9765494bc588", 0},
		{"memcached", "787f5311a6855002945c7dacc6295fda0ca6f77182fe7f16b4ef95780d16764b", 1},
		{"imagick", "4204071decbb06dce9f212a63ace0aa0c1b48fd344176b8274cd7d32ab674c71", 69},
	} {
		files, records, err := collectPeclSupplementalLegal(peclNoticeRegistry, row.extension, row.hash)
		if err != nil || len(files) == 0 || len(records) != len(files) {
			t.Fatalf("missing reviewed notices for %s: %v", row.extension, err)
		}
		if row.count > 0 && len(files) != row.count {
			t.Fatalf("unexpected notice coverage for %s: %d", row.extension, len(files))
		}
	}
	if _, err := fs.Stat(peclNoticeRegistry, "pecl_notices/manifest.json"); err != nil {
		t.Fatal(err)
	}
}
