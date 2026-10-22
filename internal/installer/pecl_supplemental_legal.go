package installer

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
)

// This registry supplements only the reviewed, byte-identical PECL ZIPs.
// It does not identify other builds by their version string or grant complete
// binary redistribution clearance. The generator checks these same bytes.
//
//go:embed pecl_notices/*
var peclNoticeRegistry embed.FS

type peclNoticeDocument struct {
	Path               string          `json:"path"`
	SHA256             string          `json:"sha256"`
	Bytes              int             `json:"bytes"`
	SourceURL          string          `json:"sourceUrl"`
	LicenseDescription string          `json:"licenseDescription"`
	SourceEvidence     json.RawMessage `json:"sourceEvidence,omitempty"`
}

type peclNoticeBundle struct {
	ID            string               `json:"id"`
	Extension     string               `json:"extension"`
	Kind          Kind                 `json:"kind,omitempty"`
	Version       string               `json:"version"`
	PHPSeries     string               `json:"phpSeries"`
	ArchiveURL    string               `json:"archiveUrl"`
	ArchiveSHA256 string               `json:"archiveSha256"`
	Scope         string               `json:"scope"`
	Notices       []peclNoticeDocument `json:"notices"`
}

type retainedPeclSupplement struct {
	retainedLegalFile
	SourceURL          string          `json:"sourceUrl"`
	LicenseDescription string          `json:"licenseDescription"`
	Bundle             string          `json:"bundle"`
	Scope              string          `json:"scope"`
	SourceEvidence     json.RawMessage `json:"sourceEvidence,omitempty"`
}

func collectPeclSupplementalLegal(registry fs.FS, extension, archiveHash string) (map[string][]byte, []retainedPeclSupplement, error) {
	return collectArchiveSupplementalLegal(registry, "pecl_notices", extension, "", "", archiveHash)
}

func collectArchiveSupplementalLegal(registry fs.FS, directory, extension string, kind Kind, version, archiveHash string) (map[string][]byte, []retainedPeclSupplement, error) {
	var manifest struct {
		SchemaVersion int                `json:"schemaVersion"`
		Bundles       []peclNoticeBundle `json:"bundles"`
	}
	data, err := fs.ReadFile(registry, directory+"/manifest.json")
	if err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, nil, err
	}
	if manifest.SchemaVersion != 1 || len(manifest.Bundles) > 64 {
		return nil, nil, fmt.Errorf("invalid PECL notice registry")
	}
	files := make(map[string][]byte)
	records := []retainedPeclSupplement{}
	seenBundles := make(map[string]bool)
	for _, bundle := range manifest.Bundles {
		if seenBundles[bundle.ArchiveSHA256] {
			return nil, nil, fmt.Errorf("duplicate PECL notice archive")
		}
		seenBundles[bundle.ArchiveSHA256] = true
		if bundle.ArchiveSHA256 != archiveHash {
			continue
		}
		matches := bundle.Extension == extension && bundle.Kind == kind
		if kind != "" {
			matches = matches && bundle.Version == version
		}
		if !matches || !safeLegalPath(bundle.ID) || strings.ContainsAny(bundle.ID, `/\`) || bundle.Scope != "supplemental-notices-not-complete-binary-clearance" || len(bundle.Notices) == 0 || len(bundle.Notices) > 256 {
			return nil, nil, fmt.Errorf("invalid PECL notice scope")
		}
		seenPaths, total := make(map[string]bool), 0
		for _, notice := range bundle.Notices {
			key := strings.ToLower(notice.Path)
			source, sourceErr := url.Parse(notice.SourceURL)
			if !safeLegalPath(notice.Path) || strings.ContainsAny(notice.Path, `/\`) || !strings.HasSuffix(notice.Path, ".txt") || seenPaths[key] || sourceErr != nil || source.Scheme != "https" || source.Host == "" {
				return nil, nil, fmt.Errorf("invalid PECL notice document")
			}
			seenPaths[key] = true
			bytes, err := fs.ReadFile(registry, directory+"/"+notice.Path)
			if err != nil {
				return nil, nil, err
			}
			sum := sha256.Sum256(bytes)
			total += len(bytes)
			if len(bytes) != notice.Bytes || len(bytes) > maxLegalFileBytes || total > maxLegalBundleBytes || hex.EncodeToString(sum[:]) != notice.SHA256 {
				return nil, nil, fmt.Errorf("PECL supplemental notice hash/size mismatch: %s", notice.Path)
			}
			name := "supplemental/" + notice.Path
			files[name] = bytes
			records = append(records, retainedPeclSupplement{retainedLegalFile{name, notice.SHA256, notice.Bytes}, notice.SourceURL, notice.LicenseDescription, bundle.ID, bundle.Scope, notice.SourceEvidence})
		}
	}
	return files, records, nil
}
