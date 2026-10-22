package installer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type legalZipEntry struct {
	name, body string
	mode       os.FileMode
}

func legalZip(t *testing.T, entries ...legalZipEntry) string {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "pecl.zip"))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		mode := entry.mode
		if mode == 0 {
			mode = 0644
		}
		header.SetMode(mode)
		writer, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func peclLegalRoot(t *testing.T, installRoot, extension, archive string) string {
	t.Helper()
	bytes, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bytes)
	return filepath.Join(installRoot, "licenses", "pecl", extension, hex.EncodeToString(sum[:]))
}

func TestPeclLegalDocuments(t *testing.T) {
	root := t.TempDir()
	ext := filepath.Join(root, "ext")
	archive := legalZip(t, legalZipEntry{name: "php_imagick.dll", body: "primary"}, legalZipEntry{name: "CORE_RL.dll", body: "companion"},
		legalZipEntry{name: "LICENSE", body: "upstream\r\nnotice\r\n"}, legalZipEntry{name: "README.md", body: "source offer"},
		legalZipEntry{name: "licenses/ImageMagick/COPYING.txt", body: "nested terms"}, legalZipEntry{name: "tests/OFL.txt", body: "font terms"}, legalZipEntry{name: "nested/ignored.dll", body: "not installed"})
	if err := os.WriteFile(filepath.Join(root, "LICENSE"), []byte("existing PHP terms"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := extractPeclZip(archive, "imagick", ext, root); err != nil {
		t.Fatal(err)
	}
	bundle := peclLegalRoot(t, root, "imagick", archive)
	for file, expected := range map[string]string{"LICENSE": "upstream\r\nnotice\r\n", "README.md": "source offer", "licenses/ImageMagick/COPYING.txt": "nested terms", "tests/OFL.txt": "font terms"} {
		bytes, err := os.ReadFile(filepath.Join(bundle, "files", file))
		if err != nil || string(bytes) != expected {
			t.Fatalf("legal file %s lost or changed: %q, %v", file, bytes, err)
		}
	}
	for file, expected := range map[string]string{"LICENSE": "existing PHP terms", "CORE_RL.dll": "companion", "ext/php_imagick.dll": "primary"} {
		bytes, err := os.ReadFile(filepath.Join(root, file))
		if err != nil || string(bytes) != expected {
			t.Fatalf("payload %s: %q, %v", file, bytes, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "nested/ignored.dll")); !os.IsNotExist(err) {
		t.Fatal("nested DLL unexpectedly installed")
	}
	var manifest struct {
		ArchiveSHA256 string `json:"archiveSha256"`
		Status        string `json:"status"`
		Files         []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	bytes, err := os.ReadFile(filepath.Join(bundle, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "preserved" || len(manifest.Files) != 4 || manifest.ArchiveSHA256 != filepath.Base(bundle) {
		t.Fatalf("invalid provenance manifest: %s", bytes)
	}
	for _, file := range manifest.Files {
		data, err := os.ReadFile(filepath.Join(bundle, "files", file.Path))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if file.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatal("wrong notice digest")
		}
	}
	if err := extractPeclZip(archive, "imagick", ext, root); err != nil {
		t.Fatalf("identical reinstall must be idempotent: %v", err)
	}
	retained := filepath.Join(bundle, "files/LICENSE")
	if err := os.WriteFile(retained, []byte("operator note"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := extractPeclZip(archive, "imagick", ext, root); err == nil {
		t.Fatal("must preserve a different existing document")
	}
	if bytes, _ := os.ReadFile(retained); string(bytes) != "operator note" {
		t.Fatal("existing legal document overwritten")
	}
}

func TestPeclLegalPreflight(t *testing.T) {
	for _, entry := range []legalZipEntry{
		{name: "../LICENSE", body: "escape"}, {name: "dir\\..\\COPYING", body: "escape"}, {name: "C:/LICENSE", body: "drive"},
		{name: "LICENSE:stream", body: "ADS"}, {name: "legal/LICENSE.", body: "alias"}, {name: "NOTICE", body: "symlink", mode: os.ModeSymlink | 0777},
		{name: "LICENSE", body: strings.Repeat("x", 8*1024*1024+1)},
	} {
		t.Run(entry.name, func(t *testing.T) {
			root := t.TempDir()
			ext := filepath.Join(root, "ext")
			if err := os.Mkdir(ext, 0755); err != nil {
				t.Fatal(err)
			}
			primary := filepath.Join(ext, "php_redis.dll")
			if err := os.WriteFile(primary, []byte("existing"), 0644); err != nil {
				t.Fatal(err)
			}
			archive := legalZip(t, legalZipEntry{name: "php_redis.dll", body: "replacement"}, entry)
			if err := extractPeclZip(archive, "redis", ext, root); err == nil {
				t.Fatal("unsafe legal material accepted")
			}
			if bytes, _ := os.ReadFile(primary); string(bytes) != "existing" {
				t.Fatal("DLL changed before archive preflight completed")
			}
		})
	}
}

func TestPeclMissingLegalDocuments(t *testing.T) {
	root := t.TempDir()
	archive := legalZip(t, legalZipEntry{name: "php_redis.dll", body: "primary"})
	if err := extractPeclZip(archive, "redis", filepath.Join(root, "ext"), root); err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(filepath.Join(peclLegalRoot(t, root, "redis", archive), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bytes), `"status": "not-provided-by-archive"`) {
		t.Fatalf("missing notices must not be called cleared: %s", bytes)
	}
}

func TestPeclLegalDirectoryLink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "licenses")); err != nil {
		t.Skipf("symlink fixture not permitted: %v", err)
	}
	archive := legalZip(t, legalZipEntry{name: "php_redis.dll", body: "primary"}, legalZipEntry{name: "LICENSE", body: "terms"})
	if err := extractPeclZip(archive, "redis", filepath.Join(root, "ext"), root); err == nil {
		t.Fatal("legal output directory link accepted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("wrote outside runtime tree")
	}
}
