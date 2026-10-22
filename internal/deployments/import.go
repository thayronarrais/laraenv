package deployments

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Import validates and prepares the complete bundle in memory, then publishes a
// single protected index. It has no executor or network dependency. Existing
// origins are skipped only when their current content matches the import;
// divergent local/source content is preserved as an explicit conflict.
func (s *Store) Import(bundle ImportBundle) (ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ImportResult{Inserted: []string{}, Skipped: []string{}, Conflicts: []ImportConflict{}}
	raw, err := json.Marshal(bundle)
	if err != nil || len(raw) > MaxImportBytes || bundle.Version != SchemaVersion {
		return result, fmt.Errorf("%w: unsupported or oversized import", ErrInvalidData)
	}
	source, err := normalizeSource(bundle.Source)
	if err != nil {
		return result, err
	}
	origins := map[string]*Deployment{}
	for _, d := range s.records {
		if d.SecretsUnavailable {
			return result, ErrSecretsUnavailable
		}
		if d.Origin != nil {
			key := originKey(d.Origin.Source, d.Origin.ID)
			if _, exists := origins[key]; exists {
				return result, fmt.Errorf("%w: duplicate stored origin", ErrInvalidData)
			}
			origins[key] = d
		}
	}
	next := copyRecords(s.records)
	seen := map[string]bool{}
	for _, input := range bundle.Deployments {
		if !validSourceID(input.ID) || seen[input.ID] || input.SecretsUnavailable {
			return ImportResult{}, fmt.Errorf("%w: malformed or duplicate source ID", ErrInvalidData)
		}
		seen[input.ID] = true
		host, ok := bundle.HostMappings[input.SSHHostExternalID]
		if !ok || !validHost(input.SSHHostExternalID) || !validHost(host) {
			return ImportResult{}, fmt.Errorf("%w: explicit valid host mapping required", ErrInvalidData)
		}
		incoming := clone(&input)
		incoming.ID = uuid.NewString()
		incoming.SSHHostExternalID = host
		incoming.Origin = nil
		incoming.SecretsUnavailable = false
		if len(incoming.Runs) > MaxRuns {
			incoming.Runs = append([]RunSummary(nil), incoming.Runs[len(incoming.Runs)-MaxRuns:]...)
		}
		runIDs := map[string]bool{}
		// Validate all supplied runs, including history outside the retained window.
		for _, r := range input.Runs {
			if !validSourceID(r.ID) || runIDs[r.ID] || (r.DeploymentID != "" && r.DeploymentID != input.ID) {
				return ImportResult{}, fmt.Errorf("%w: malformed source run ID", ErrInvalidData)
			}
			runIDs[r.ID] = true
		}
		for i := range incoming.Runs {
			r := &incoming.Runs[i]
			r.ID = uuid.NewString()
			r.DeploymentID = incoming.ID
			r.HasLog = false
			r.RecoveryPending = false
		}
		incoming.CreatedAt = now()
		incoming.UpdatedAt = now()
		if err := validateDeployment(incoming); err != nil {
			return ImportResult{}, err
		}
		fingerprint := contentFingerprint(incoming)
		key := originKey(source, input.ID)
		if existing, ok := origins[key]; ok {
			if contentFingerprint(existing) == fingerprint {
				result.Skipped = append(result.Skipped, existing.ID)
			} else {
				result.Conflicts = append(result.Conflicts, ImportConflict{ID: input.ID, ExistingID: existing.ID, Reason: "local or imported content differs"})
			}
			continue
		}
		incoming.Origin = &Origin{Source: source, ID: input.ID, Fingerprint: fingerprint}
		next[incoming.ID] = incoming
		result.Inserted = append(result.Inserted, incoming.ID)
	}
	if len(result.Inserted) == 0 {
		return result, nil
	}
	if err := s.commit(next); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func normalizeSource(source ImportSource) (ImportSource, error) {
	source.Namespace = strings.TrimSpace(source.Namespace)
	source.AccountID = strings.TrimSpace(source.AccountID)
	if source.Namespace != "" {
		if len(source.Namespace) > 512 || strings.ContainsAny(source.Namespace, "\x00\r\n") || source.ServiceURL != "" || source.AccountID != "" {
			return ImportSource{}, fmt.Errorf("%w: invalid namespace scope", ErrInvalidData)
		}
		return source, nil
	}
	parsed, err := url.Parse(source.ServiceURL)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || source.AccountID == "" || len(source.AccountID) > 256 || strings.ContainsAny(source.AccountID, "\x00\r\n") {
		return ImportSource{}, fmt.Errorf("%w: service and account scope required", ErrInvalidData)
	}
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	source.ServiceURL = parsed.String()
	return source, nil
}
func originKey(source ImportSource, id string) string {
	raw, _ := json.Marshal(source)
	return string(raw) + "\x00" + id
}
func validSourceID(id string) bool {
	if validUUID(id) {
		return true
	}
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == id
}
func contentFingerprint(d *Deployment) string {
	copy := clone(d)
	copy.ID = ""
	copy.CreatedAt = ""
	copy.UpdatedAt = ""
	copy.Origin = nil
	copy.SecretsUnavailable = false
	for i := range copy.Runs {
		copy.Runs[i].ID = ""
		copy.Runs[i].DeploymentID = ""
		copy.Runs[i].RecoveryPending = false
	}
	raw, _ := json.Marshal(copy)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
