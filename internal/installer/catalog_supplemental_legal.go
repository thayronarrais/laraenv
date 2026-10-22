package installer

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

// Additional catalog notices are bound to exact archive bytes. Existing
// version-bound core notices and their manifests remain untouched.
//
//go:embed runtime_notices/*
var catalogNoticeRegistry embed.FS

func collectCatalogSupplementalLegal(registry fs.FS, kind Kind, version, archiveHash string) (map[string][]byte, []retainedPeclSupplement, error) {
	return collectArchiveSupplementalLegal(registry, "runtime_notices", "", kind, version, archiveHash)
}

func preserveCatalogLegal(source Source, archive io.Reader, registry fs.FS) error {
	// Redis is the only catalog archive in this registry. Other large runtime
	// downloads do not need an additional hash pass for an unrelated notice set.
	if source.Kind != KindRedis {
		return nil
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, archive); err != nil {
		return err
	}
	archiveHash := hex.EncodeToString(digest.Sum(nil))
	files, records, err := collectCatalogSupplementalLegal(registry, source.Kind, source.Version, archiveHash)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	if !safeLegalPath(source.Version) || strings.ContainsAny(source.Version, `/\`) {
		return fmt.Errorf("invalid runtime notice version")
	}
	separate := make(map[string][]byte)
	for name, bytes := range files {
		separate[strings.TrimPrefix(name, "supplemental/")] = bytes
	}
	for index := range records {
		records[index].Path = strings.TrimPrefix(records[index].Path, "supplemental/")
	}
	manifest := struct {
		SchemaVersion int                      `json:"schemaVersion"`
		Kind          Kind                     `json:"kind"`
		Version       string                   `json:"version"`
		ArchiveSHA256 string                   `json:"archiveSha256"`
		Status        string                   `json:"status"`
		Files         []retainedPeclSupplement `json:"files"`
	}{1, source.Kind, source.Version, archiveHash, "supplemental-source-notices-not-binary-clearance", records}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	separate["manifest.json"] = append(data, '\n')
	return writeLegalBundle(source.InstallTo, path.Join("licenses/laraenv", string(source.Kind), source.Version, "supplemental", records[0].Bundle), separate)
}
