package deployments

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"lara4ever/internal/atomicfile"
)

type envelope struct {
	ID      string `json:"id"`
	Payload string `json:"payload"`
}
type diskIndex struct {
	Version int        `json:"version"`
	Records []envelope `json:"records"`
}
type Store struct {
	mu        sync.Mutex
	root      string
	protector Protector
	records   map[string]*Deployment
	payloads  map[string]string
}

// NewStore uses root itself (normally <StateRoot>/deployments). It never opens a
// general preferences file, cloud session, SSH connection, or remote service.
// Unavailable records remain opaque and are exposed as UUID-only locked stubs.
func NewStore(root string, protector Protector) (*Store, error) {
	if strings.TrimSpace(root) == "" || protector == nil {
		return nil, fmt.Errorf("%w: root and protector required", ErrInvalidData)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	s := &Store{root: root, protector: protector, records: map[string]*Deployment{}, payloads: map[string]string{}}
	path := filepath.Join(root, "index.json")
	raw, err := readBounded(path, maxIndexBytes)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var disk diskIndex
	if err := json.Unmarshal(raw, &disk); err != nil || disk.Version != SchemaVersion || disk.Records == nil {
		return nil, fmt.Errorf("%w: unsupported or malformed index", ErrInvalidData)
	}
	changed := false
	for _, entry := range disk.Records {
		if !validUUID(entry.ID) || entry.Payload == "" {
			return nil, fmt.Errorf("%w: malformed record envelope", ErrInvalidData)
		}
		if _, exists := s.payloads[entry.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate ID", ErrInvalidData)
		}
		s.payloads[entry.ID] = entry.Payload
		plain, err := protector.Unprotect(entry.Payload)
		if err != nil {
			s.records[entry.ID] = &Deployment{ID: entry.ID, SecretsUnavailable: true}
			continue
		}
		var d Deployment
		if len(plain) > maxRecordBytes || json.Unmarshal([]byte(plain), &d) != nil || d.ID != entry.ID || d.SecretsUnavailable {
			return nil, fmt.Errorf("%w: malformed protected record", ErrInvalidData)
		}
		if err := validateDeployment(&d); err != nil {
			return nil, err
		}
		for i := range d.Runs {
			r := &d.Runs[i]
			if r.RecoveryPending && r.Status == "running" && r.FinishedAt == "" && knownRunKind(r.Kind) {
				r.Status = "interrupted"
				r.Interrupted = true
				r.RecoveryPending = false
				r.FinishedAt = now()
				if i == len(d.Runs)-1 {
					d.LastStatus = "interrupted"
				}
				changed = true
			}
		}
		s.records[d.ID] = &d
	}
	if changed {
		if err := s.commit(s.records); err != nil {
			return nil, fmt.Errorf("persist interrupted runs: %w", err)
		}
	}
	return s, nil
}

func (s *Store) List() ([]Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Deployment, 0, len(s.records))
	var resultErr error
	for _, d := range s.records {
		out = append(out, *clone(d))
		if d.SecretsUnavailable {
			resultErr = ErrSecretsUnavailable
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == out[j].CreatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out, resultErr
}
func (s *Store) Get(id string) (*Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.get(id)
	if err != nil {
		if d != nil {
			return clone(d), err
		}
		return nil, err
	}
	return clone(d), nil
}
func (s *Store) get(id string) (*Deployment, error) {
	if !validUUID(id) {
		return nil, fmt.Errorf("%w: deployment ID must be UUID", ErrInvalidData)
	}
	d, ok := s.records[id]
	if !ok {
		return nil, ErrNotFound
	}
	if d.SecretsUnavailable {
		return d, ErrSecretsUnavailable
	}
	return d, nil
}
func (s *Store) Create(name, host string) (*Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := &Deployment{ID: uuid.NewString(), Name: name, SSHHostExternalID: host, CreatedAt: now(), UpdatedAt: now()}
	if err := validateDeployment(d); err != nil {
		return nil, err
	}
	next := copyRecords(s.records)
	next[d.ID] = d
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return clone(d), nil
}
func (s *Store) Patch(id string, patch map[string]any) (*Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(id)
	if err != nil {
		return nil, err
	}
	d := clone(current)
	for key, value := range patch {
		if key == "config" {
			if value == nil {
				d.Config = nil
				continue
			}
			raw, err := json.Marshal(value)
			if err != nil || len(raw) > maxRecordBytes {
				return nil, fmt.Errorf("%w: invalid config", ErrInvalidData)
			}
			var cfg map[string]any
			if json.Unmarshal(raw, &cfg) != nil || cfg == nil {
				return nil, fmt.Errorf("%w: config must be an object", ErrInvalidData)
			}
			d.Config = cfg
			continue
		}
		v, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: %s must be a string", ErrInvalidData, key)
		}
		switch key {
		case "name":
			d.Name = v
		case "ssh_host_external_id":
			d.SSHHostExternalID = v
		case "os_family":
			d.OSFamily = v
		case "os_version":
			d.OSVersion = v
		case "project_kind":
			d.ProjectKind = v
		default:
			return nil, fmt.Errorf("%w: unsupported patch field", ErrInvalidData)
		}
	}
	d.UpdatedAt = now()
	if err := validateDeployment(d); err != nil {
		return nil, err
	}
	next := copyRecords(s.records)
	next[id] = d
	if err := s.commit(next); err != nil {
		return nil, err
	}
	return clone(d), nil
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.get(id); err != nil {
		return err
	}
	next := copyRecords(s.records)
	delete(next, id)
	if err := s.commit(next); err != nil {
		return err
	}
	// Unreferenced protected logs are safe to retain if cleanup is interrupted.
	// Remove only files named by the validated record, never a recursive path.
	entries, _ := os.ReadDir(filepath.Join(s.root, "logs", id))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") && validUUID(strings.TrimSuffix(entry.Name(), ".json")) {
			_ = os.Remove(filepath.Join(s.root, "logs", id, entry.Name()))
		}
	}
	_ = os.Remove(filepath.Join(s.root, "logs", id))
	return nil
}

// commit encrypts and stages the entire next index before its sole atomic
// publication. Memory changes only after publication succeeds; locked payloads
// are copied byte-for-byte rather than decoded or replaced with empty values.
func (s *Store) commit(next map[string]*Deployment) error {
	disk := diskIndex{Version: SchemaVersion, Records: make([]envelope, 0, len(next))}
	payloads := make(map[string]string, len(next))
	ids := make([]string, 0, len(next))
	for id := range next {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		d := next[id]
		payload := s.payloads[id]
		if !d.SecretsUnavailable {
			raw, err := json.Marshal(d)
			if err != nil || len(raw) > maxRecordBytes {
				return fmt.Errorf("%w: record too large or malformed", ErrInvalidData)
			}
			payload, err = s.protector.Protect(string(raw))
			if err != nil {
				return fmt.Errorf("protect deployment: %w", err)
			}
			if payload == "" || payload == string(raw) {
				return fmt.Errorf("%w: protector returned an unprotected record", ErrSecretsUnavailable)
			}
		}
		if payload == "" {
			return ErrSecretsUnavailable
		}
		disk.Records = append(disk.Records, envelope{ID: id, Payload: payload})
		payloads[id] = payload
	}
	raw, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	if len(raw) > maxIndexBytes {
		return fmt.Errorf("%w: index too large", ErrInvalidData)
	}
	if err := atomicfile.WritePreservingPermissions(filepath.Join(s.root, "index.json"), raw, 0600); err != nil {
		return fmt.Errorf("save deployment index: %w", err)
	}
	s.records = next
	s.payloads = payloads
	return nil
}
func clone(d *Deployment) *Deployment {
	raw, _ := json.Marshal(d)
	var out Deployment
	_ = json.Unmarshal(raw, &out)
	return &out
}
func copyRecords(in map[string]*Deployment) map[string]*Deployment {
	out := make(map[string]*Deployment, len(in))
	for id, d := range in {
		out[id] = d
	}
	return out
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func validUUID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.String() == id && parsed != uuid.Nil
}
func validHost(host string) bool {
	return len(host) > 0 && len(host) <= 256 && host == strings.TrimSpace(host) && !strings.ContainsAny(host, "/\\\x00\r\n") && host != "." && host != ".."
}
func validateDeployment(d *Deployment) error {
	if !validUUID(d.ID) || strings.TrimSpace(d.Name) == "" || len(d.Name) > 256 || !validHost(d.SSHHostExternalID) || len(d.Runs) > MaxRuns {
		return fmt.Errorf("%w: invalid deployment fields", ErrInvalidData)
	}
	seen := map[string]bool{}
	for _, r := range d.Runs {
		if !validUUID(r.ID) || seen[r.ID] || (r.DeploymentID != "" && r.DeploymentID != d.ID) || len(r.Kind) > 64 || len(r.Status) > 64 {
			return fmt.Errorf("%w: invalid run fields", ErrInvalidData)
		}
		seen[r.ID] = true
	}
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > maxRecordBytes {
		return fmt.Errorf("%w: record too large or malformed", ErrInvalidData)
	}
	return nil
}
func readBounded(path string, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > limit {
		return nil, fmt.Errorf("%w: file too large", ErrInvalidData)
	}
	return raw, nil
}
func knownRunKind(kind string) bool {
	switch kind {
	case "detect", "apply", "deploy", "destroy":
		return true
	}
	return false
}

type logEnvelope struct {
	Version int    `json:"version"`
	Payload string `json:"payload"`
}
type protectedLog struct {
	DeploymentID string `json:"deployment_id"`
	RunID        string `json:"run_id"`
	Log          string `json:"log"`
}

// StartRun must succeed before a caller sends any mutating SSH command.
func (s *Store) StartRun(id, kind string) (*RunSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(id)
	if err != nil {
		return nil, err
	}
	if !knownRunKind(kind) {
		return nil, fmt.Errorf("%w: unsupported run kind", ErrInvalidData)
	}
	d := clone(current)
	r := RunSummary{ID: uuid.NewString(), DeploymentID: id, Kind: kind, Status: "running", StartedAt: now(), RecoveryPending: true}
	d.Runs = append(d.Runs, r)
	var evicted []RunSummary
	if len(d.Runs) > MaxRuns {
		evicted = append(evicted, d.Runs[:len(d.Runs)-MaxRuns]...)
		d.Runs = append([]RunSummary(nil), d.Runs[len(d.Runs)-MaxRuns:]...)
	}
	d.LastStatus = "running"
	d.LastRunAt = r.StartedAt
	d.UpdatedAt = now()
	next := copyRecords(s.records)
	next[id] = d
	if err := s.commit(next); err != nil {
		return nil, err
	}
	for _, old := range evicted {
		_ = os.Remove(s.logPath(id, old.ID))
	}
	return &r, nil
}

// FinishRun stages an immutable encrypted log before atomically publishing the
// summary. A failure leaves the durable running state, allowing the caller to
// report remote success separately without rerunning any remote operation.
func (s *Store) FinishRun(id, runID, status, log, errorMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.get(id)
	if err != nil {
		return err
	}
	if !validUUID(runID) || strings.TrimSpace(status) == "" || len(status) > 64 || status == "running" {
		return fmt.Errorf("%w: invalid final run status or ID", ErrInvalidData)
	}
	d := clone(current)
	index := -1
	for i := range d.Runs {
		if d.Runs[i].ID == runID {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrNotFound
	}
	r := &d.Runs[index]
	if r.Status != "running" || r.FinishedAt != "" {
		return fmt.Errorf("%w: run is already finalized", ErrConflict)
	}
	log = strings.ToValidUTF8(log, "\uFFFD")
	if len(log) > MaxLogBytes {
		log = log[:MaxLogBytes]
		for !utf8.ValidString(log) {
			log = log[:len(log)-1]
		}
		r.Truncated = true
	}
	r.Status = status
	r.RecoveryPending = false
	r.ErrorMessage = errorMessage
	r.FinishedAt = now()
	r.HasLog = log != ""
	d.LastStatus = status
	d.LastRunAt = r.StartedAt
	d.UpdatedAt = now()
	if err := validateDeployment(d); err != nil {
		return err
	}
	wroteLog := false
	if r.HasLog {
		path := s.logPath(id, runID)
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("%w: existing opaque log will not be overwritten", ErrSecretsUnavailable)
		} else if !os.IsNotExist(err) {
			return err
		}
		plain, err := json.Marshal(protectedLog{DeploymentID: id, RunID: runID, Log: log})
		if err != nil {
			return err
		}
		payload, err := s.protector.Protect(string(plain))
		if err != nil {
			return fmt.Errorf("protect run log: %w", err)
		}
		if payload == "" || payload == string(plain) {
			return ErrSecretsUnavailable
		}
		raw, err := json.Marshal(logEnvelope{Version: SchemaVersion, Payload: payload})
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		if err := atomicfile.WritePreservingPermissions(path, raw, 0600); err != nil {
			return fmt.Errorf("save run log: %w", err)
		}
		wroteLog = true
	}
	next := copyRecords(s.records)
	next[id] = d
	if err := s.commit(next); err != nil {
		if wroteLog {
			_ = os.Remove(s.logPath(id, runID))
		}
		return err
	}
	return nil
}
func (s *Store) ReadRunLog(id, runID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.get(id)
	if err != nil {
		return "", err
	}
	if !validUUID(runID) {
		return "", fmt.Errorf("%w: run ID must be UUID", ErrInvalidData)
	}
	var run *RunSummary
	for i := range d.Runs {
		if d.Runs[i].ID == runID {
			run = &d.Runs[i]
			break
		}
	}
	if run == nil {
		return "", ErrNotFound
	}
	if !run.HasLog {
		return "", nil
	}
	raw, err := readBounded(s.logPath(id, runID), maxIndexBytes)
	if err != nil {
		return "", err
	}
	var disk logEnvelope
	if json.Unmarshal(raw, &disk) != nil || disk.Version != SchemaVersion || disk.Payload == "" {
		return "", fmt.Errorf("%w: malformed log envelope", ErrInvalidData)
	}
	plain, err := s.protector.Unprotect(disk.Payload)
	if err != nil {
		return "", ErrSecretsUnavailable
	}
	var body protectedLog
	if len(plain) > MaxLogBytes*6+1024 || json.Unmarshal([]byte(plain), &body) != nil || body.DeploymentID != id || body.RunID != runID || len(body.Log) > MaxLogBytes {
		return "", fmt.Errorf("%w: malformed protected log", ErrInvalidData)
	}
	return body.Log, nil
}
func (s *Store) logPath(id, runID string) string {
	return filepath.Join(s.root, "logs", id, runID+".json")
}
