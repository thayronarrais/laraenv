package installer

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

const maxLegalFileBytes = 8 * 1024 * 1024
const maxLegalBundleBytes = 32 * 1024 * 1024

type retainedLegalFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

// Preserve notices as data, never as executable files. Hashes identify the
// inspected archive; they do not authenticate its publisher or clear licenses.
func preservePeclLegal(files []*zip.File, archive io.Reader, extension, installRoot string) error {
	return preservePeclLegalWithNotices(files, archive, extension, installRoot, peclNoticeRegistry)
}

func preservePeclLegalWithNotices(files []*zip.File, archive io.Reader, extension, installRoot string, registry fs.FS) error {
	if extension == "" || strings.Trim(extension, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
		return fmt.Errorf("invalid extension name")
	}
	if len(files) > 4096 {
		return fmt.Errorf("too many PECL archive entries")
	}
	seen := make(map[string]bool)
	legal := make(map[string][]byte)
	records := []retainedLegalFile{}
	total, primary := 0, false
	for _, file := range files {
		name := strings.ReplaceAll(file.Name, `\`, "/")
		if file.FileInfo().IsDir() {
			name = strings.TrimSuffix(name, "/")
		}
		if !safeLegalPath(name) || file.Mode()&os.ModeType != 0 && !file.FileInfo().IsDir() {
			return fmt.Errorf("unsafe PECL archive entry: %q", file.Name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fmt.Errorf("duplicate PECL archive entry: %q", file.Name)
		}
		seen[key] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if key == "php_"+extension+".dll" {
			primary = true
		}
		if !isLegalMaterial(name) {
			continue
		}
		if file.UncompressedSize64 > maxLegalFileBytes {
			return fmt.Errorf("PECL legal file exceeds size limit: %q", file.Name)
		}
		reader, err := file.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxLegalFileBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		total += len(data)
		if len(data) > maxLegalFileBytes || total > maxLegalBundleBytes {
			return fmt.Errorf("PECL legal material exceeds size limit")
		}
		legal["files/"+name] = data
		sum := sha256.Sum256(data)
		records = append(records, retainedLegalFile{Path: name, SHA256: hex.EncodeToString(sum[:]), Bytes: len(data)})
	}
	if !primary {
		return fmt.Errorf("PECL archive is missing php_%s.dll", extension)
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, archive); err != nil {
		return err
	}
	archiveHash := hex.EncodeToString(digest.Sum(nil))
	supplementalFiles, supplemental, err := collectPeclSupplementalLegal(registry, extension, archiveHash)
	if err != nil {
		return err
	}
	for _, bytes := range supplementalFiles {
		total += len(bytes)
		if total > maxLegalBundleBytes {
			return fmt.Errorf("PECL combined legal material exceeds size limit")
		}
	}
	status := "preserved"
	if len(records) == 0 {
		status = "not-provided-by-archive"
	}
	manifest := struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Extension     string              `json:"extension"`
		ArchiveSHA256 string              `json:"archiveSha256"`
		Status        string              `json:"status"`
		Files         []retainedLegalFile `json:"files"`
	}{1, extension, archiveHash, status, records}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	legal["manifest.json"] = append(data, '\n')
	// Keep the original archive manifest byte-identical for existing installs.
	// Additional reviewed evidence has a separate, independently idempotent
	// bundle and manifest, with no overwrite or migration of original files.
	if len(supplemental) > 0 {
		separate := make(map[string][]byte)
		for name, bytes := range supplementalFiles {
			separate[strings.TrimPrefix(name, "supplemental/")] = bytes
		}
		for index := range supplemental {
			supplemental[index].Path = strings.TrimPrefix(supplemental[index].Path, "supplemental/")
		}
		manifest := struct {
			SchemaVersion int                      `json:"schemaVersion"`
			Extension     string                   `json:"extension"`
			ArchiveSHA256 string                   `json:"archiveSha256"`
			Status        string                   `json:"status"`
			Files         []retainedPeclSupplement `json:"files"`
		}{1, extension, archiveHash, "supplemental-source-notices-not-binary-clearance", supplemental}
		bytes, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		separate["manifest.json"] = append(bytes, '\n')
		if err := writeLegalBundle(installRoot, path.Join("licenses/pecl", extension, archiveHash, "supplemental"), separate); err != nil {
			return err
		}
	}
	return writeLegalBundle(installRoot, path.Join("licenses/pecl", extension, archiveHash), legal)
}

func safeLegalPath(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, `:<>"|?*`) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.TrimRight(part, ". ") != part {
			return false
		}
		for _, char := range part {
			if char < 32 {
				return false
			}
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}

func reservedLegalPath(name string) bool {
	parts := strings.Split(strings.ReplaceAll(name, `\`, "/"), "/")
	for index, part := range parts {
		if part != "." && part != ".." {
			parts[index] = strings.TrimRight(part, ". ")
		}
	}
	clean := strings.ToLower(path.Clean(strings.Join(parts, "/")))
	for _, prefix := range []string{"licenses/laraenv", "licenses/pecl"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

func isLegalMaterial(name string) bool {
	parts := strings.Split(strings.ToLower(name), "/")
	for index, part := range parts {
		base := strings.FieldsFunc(part, func(c rune) bool { return c == '.' || c == '_' || c == '-' })
		if len(base) == 0 {
			continue
		}
		switch base[0] {
		case "license", "licence", "licenses", "licences", "copying", "notice", "notices", "legal", "copyright", "authors", "credits", "ofl", "unlicense":
			return true
		case "readme":
			if index == len(parts)-1 {
				return true
			}
		}
		if strings.HasPrefix(part, "thirdparty") || strings.HasPrefix(part, "third-party") || strings.HasPrefix(part, "third_party") {
			return true
		}
	}
	return false
}

// Pin writes beneath the install tree and exclusively create new evidence.
// Existing identical files make retries idempotent; different files stay intact.
func writeLegalBundle(installRoot, relative string, files map[string][]byte) error {
	if err := os.MkdirAll(installRoot, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(installRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := legalMkdirAll(root, relative); err != nil {
		return err
	}
	bundle, err := root.OpenRoot(relative)
	if err != nil {
		return err
	}
	defer bundle.Close()
	// manifest.json is written last, so it never advertises a partial bundle.
	for name, data := range files {
		if name != "manifest.json" {
			if err := writeLegalFile(bundle, name, data); err != nil {
				return err
			}
		}
	}
	return writeLegalFile(bundle, "manifest.json", files["manifest.json"])
}

func legalMkdirAll(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	if !safeLegalPath(name) {
		return fmt.Errorf("invalid legal output path")
	}
	current := ""
	for _, part := range strings.Split(name, "/") {
		current = path.Join(current, part)
		info, err := root.Lstat(current)
		if os.IsNotExist(err) {
			if err := root.Mkdir(current, 0755); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = root.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legal output directory is not a regular directory: %s", current)
		}
	}
	return nil
}

func writeLegalFile(root *os.Root, name string, data []byte) error {
	if !safeLegalPath(name) {
		return fmt.Errorf("invalid legal output path")
	}
	if err := legalMkdirAll(root, path.Dir(name)); err != nil {
		return err
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if os.IsExist(err) {
		info, statErr := root.Lstat(name)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legal output is not a regular file: %s", name)
		}
		existing, readErr := root.ReadFile(name)
		if readErr != nil {
			return readErr
		}
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("existing legal document differs; preserved: %s", name)
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
