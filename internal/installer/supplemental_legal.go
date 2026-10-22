package installer

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"path"
)

// These unmodified upstream source notices supplement reviewed archive gaps.
// They do not certify all companion binaries, authenticate a downloaded ZIP or
// apply to other runtime versions. Exact sources/hashes are in runtime-policy.
//
//go:embed licenses/*.txt
var supplementalNotices embed.FS

func preserveSupplementalLegal(source Source) error {
	var names []string
	switch {
	case source.Kind == KindPgvector && (source.Version == "0.8.6-pg15" || source.Version == "0.8.6-pg16" || source.Version == "0.8.6-pg17" || source.Version == "0.8.6-pg18"):
		names = []string{"pgvector-LICENSE.txt"}
	case source.Kind == KindRedis && source.Version == "5.0.14.1":
		names = []string{"redis-license.txt", "redis-COPYING.txt", "redis-jemalloc-COPYING.txt", "redis-lua-COPYRIGHT.txt", "redis-hiredis-COPYING.txt"}
	default:
		return nil
	}
	files := make(map[string][]byte)
	records := make([]retainedLegalFile, 0, len(names))
	for _, name := range names {
		data, err := supplementalNotices.ReadFile("licenses/" + name)
		if err != nil {
			return err
		}
		files[name] = data
		hash := sha256.Sum256(data)
		records = append(records, retainedLegalFile{Path: name, SHA256: hex.EncodeToString(hash[:]), Bytes: len(data)})
	}
	manifest := struct {
		SchemaVersion int                 `json:"schemaVersion"`
		Kind          Kind                `json:"kind"`
		Version       string              `json:"version"`
		Status        string              `json:"status"`
		Files         []retainedLegalFile `json:"files"`
	}{1, source.Kind, source.Version, "supplemental-source-notices-not-binary-clearance", records}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	files["manifest.json"] = append(data, '\n')
	return writeLegalBundle(source.InstallTo, path.Join("licenses/laraenv", string(source.Kind), source.Version), files)
}
