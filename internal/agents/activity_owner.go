package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"lara4ever/internal/atomicfile"
)

var ErrActivityOwnerConflict = errors.New("activity owner conflict")
var ErrActivityOwnerUnavailable = errors.New("activity owner unavailable")

type ActivityOwnerIdentity struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"createdAt"`
}

// ActivityOwnerDescriptor is local-only. Never include it in frontend DTOs.
type ActivityOwnerDescriptor struct {
	Identity ActivityOwnerIdentity `json:"identity"`
	Endpoint string                `json:"endpoint"`
	Token    string                `json:"token"`
}

type ActivityBinding struct {
	SessionID         string  `json:"sessionId"`
	RunID             string  `json:"runId"`
	TerminalID        string  `json:"terminalId"`
	ProviderSessionID string  `json:"providerSessionId"`
	OwnerID           string  `json:"ownerId"`
	Provider          AgentID `json:"provider"`
}

type activityOwnerBindings struct {
	Terminals map[string]bool        `json:"terminals"`
	Active    []ActivityBinding      `json:"active"`
	Ended     []activityEndedBinding `json:"ended"`
	// Keys are active LaraEnv session IDs. All writes are fenced by their run
	// and live terminal; aliases never replace the canonical provider root.
	ProviderAliases map[string][]string `json:"providerAliases,omitempty"`
}
type activityEndedBinding struct {
	Binding    ActivityBinding `json:"binding"`
	ReleasedAt time.Time       `json:"releasedAt"`
}
type activityOwnerRecord struct {
	descriptor ActivityOwnerDescriptor
	bindings   activityOwnerBindings
}

type ActivityOwnerRegistry struct {
	root   string
	alive  func(ActivityOwnerIdentity) bool
	mu     sync.Mutex
	local  map[string]bool
	closed bool
}

func NewActivityOwnerRegistry(root string, alive func(ActivityOwnerIdentity) bool) *ActivityOwnerRegistry {
	if alive == nil {
		alive = activityProcessAlive
	}
	return &ActivityOwnerRegistry{root: root, alive: alive, local: map[string]bool{}}
}

func validActivityOwnerID(id string) bool {
	u, err := uuid.Parse(id)
	return err == nil && u.String() == id
}
func (r *ActivityOwnerRegistry) ownerDir(id string) string {
	return filepath.Join(r.root, "owners", id)
}

// locked uses an OS lock, not a lock-file existence lease: process exit always
// releases it and a reused PID can never strand or steal a claim.
func (r *ActivityOwnerRegistry) locked(fn func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrActivityOwnerUnavailable
	}
	if err := os.MkdirAll(r.root, 0700); err != nil {
		return err
	}
	if err := protectActivityDirectory(r.root); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(r.root, "registry.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := lockActivityFile(f); err != nil {
		return err
	}
	defer unlockActivityFile(f)
	return fn()
}

func (r *ActivityOwnerRegistry) readOwner(id string) (activityOwnerRecord, bool, error) {
	var out activityOwnerRecord
	if !validActivityOwnerID(id) {
		return out, false, errors.New("invalid activity owner ID")
	}
	if err := readActivityJSON(filepath.Join(r.ownerDir(id), "descriptor.json"), &out.descriptor); err != nil {
		if os.IsNotExist(err) {
			return out, false, nil
		}
		return out, false, err
	}
	if out.descriptor.Identity.ID != id || !r.alive(out.descriptor.Identity) {
		return out, false, nil
	}
	if err := readActivityJSON(filepath.Join(r.ownerDir(id), "bindings.json"), &out.bindings); err != nil {
		return out, false, err
	}
	return out, true, nil
}

func readActivityJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 2<<20 {
		return errors.New("activity registry file too large")
	}
	return json.NewDecoder(f).Decode(v)
}
func writeActivityJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("activity registry file too large")
	}
	return atomicfile.Write(path, data, 0600)
}
func (r *ActivityOwnerRegistry) writeBindings(id string, b activityOwnerBindings) error {
	return writeActivityJSON(filepath.Join(r.ownerDir(id), "bindings.json"), b)
}

func (r *ActivityOwnerRegistry) records() ([]activityOwnerRecord, error) {
	entries, err := os.ReadDir(filepath.Join(r.root, "owners"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []activityOwnerRecord
	for _, e := range entries {
		if !e.IsDir() || !validActivityOwnerID(e.Name()) {
			continue
		}
		v, ok, err := r.readOwner(e.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, v)
		}
	}
	return result, nil
}

func (r *ActivityOwnerRegistry) Register(owner ActivityOwnerDescriptor) error {
	return r.locked(func() error {
		if !validActivityOwnerID(owner.Identity.ID) || owner.Identity.PID <= 0 || owner.Identity.CreatedAt.IsZero() || owner.Token == "" || !validActivityEndpoint(owner.Endpoint) || !r.alive(owner.Identity) {
			return ErrActivityOwnerUnavailable
		}
		if _, err := os.Stat(filepath.Join(r.ownerDir(owner.Identity.ID), "descriptor.json")); err == nil {
			return ErrActivityOwnerConflict
		} else if !os.IsNotExist(err) {
			return err
		}
		for _, kind := range []string{"activity", "session"} {
			if err := os.MkdirAll(filepath.Join(r.ownerDir(owner.Identity.ID), "events", kind), 0700); err != nil {
				return err
			}
		}
		if err := r.writeBindings(owner.Identity.ID, activityOwnerBindings{Terminals: map[string]bool{}, Active: []ActivityBinding{}, Ended: []activityEndedBinding{}}); err != nil {
			return err
		}
		if err := writeActivityJSON(filepath.Join(r.ownerDir(owner.Identity.ID), "descriptor.json"), owner); err != nil {
			return err
		}
		r.local[owner.Identity.ID] = true
		return nil
	})
}

func (r *ActivityOwnerRegistry) Bind(binding ActivityBinding) error {
	return r.locked(func() error {
		if !r.local[binding.OwnerID] || binding.SessionID == "" || binding.RunID == "" || binding.TerminalID == "" || binding.Provider == "" {
			return ErrActivityOwnerUnavailable
		}
		own, ok, err := r.readOwner(binding.OwnerID)
		if err != nil {
			return err
		}
		if !ok || !own.bindings.Terminals[binding.TerminalID] {
			return ErrActivityOwnerUnavailable
		}
		records, err := r.records()
		if err != nil {
			return err
		}
		for _, record := range records {
			for _, b := range record.bindings.Active {
				if b.SessionID == binding.SessionID || (b.Provider == binding.Provider && activityBindingHasProviderID(record.bindings, b, binding.ProviderSessionID)) {
					if b.SessionID != binding.SessionID || b.OwnerID != binding.OwnerID || b.RunID != binding.RunID || b.TerminalID != binding.TerminalID || b.Provider != binding.Provider {
						return ErrActivityOwnerConflict
					}
					// Adoption can fill an unknown root, but descendants must use
					// SetProviderAliases instead of rewriting the root identity.
					if b.ProviderSessionID != "" && b.ProviderSessionID != binding.ProviderSessionID {
						return ErrActivityOwnerConflict
					}
				}
			}
		}
		next := own.bindings.Active[:0]
		for _, b := range own.bindings.Active {
			if b.SessionID != binding.SessionID {
				next = append(next, b)
			}
		}
		own.bindings.Active = append(next, binding)
		return r.writeBindings(binding.OwnerID, own.bindings)
	})
}

func activityBindingHasProviderID(owner activityOwnerBindings, binding ActivityBinding, providerID string) bool {
	return providerID != "" && (binding.ProviderSessionID == providerID || slices.Contains(owner.ProviderAliases[binding.SessionID], providerID))
}

// SetProviderAliases replaces the full inventory of at most 500 proven
// descendant IDs for a currently owned run. Callers must establish ancestry;
// this registry establishes exclusive routing, not provider ancestry itself.
// The canonical root stays in ActivityBinding and is never replaced/removed.
func (r *ActivityOwnerRegistry) SetProviderAliases(sessionID, runID string, providerIDs []string) error {
	if len(providerIDs) > 500 {
		return errors.New("too many activity provider aliases")
	}
	return r.locked(func() error {
		records, err := r.records()
		if err != nil {
			return err
		}
		var target *activityOwnerRecord
		var binding ActivityBinding
		for i := range records {
			if !r.local[records[i].descriptor.Identity.ID] {
				continue
			}
			for _, b := range records[i].bindings.Active {
				if b.SessionID == sessionID && b.RunID == runID && b.ProviderSessionID != "" && records[i].bindings.Terminals[b.TerminalID] {
					target = &records[i]
					binding = b
				}
			}
		}
		if target == nil {
			return ErrActivityOwnerUnavailable
		}
		aliases := make([]string, 0, len(providerIDs))
		seen := map[string]bool{binding.ProviderSessionID: true}
		for _, id := range providerIDs {
			if id == "" {
				return errors.New("empty activity provider alias")
			}
			if seen[id] {
				continue
			}
			for _, record := range records {
				for _, other := range record.bindings.Active {
					if other == binding || other.Provider != binding.Provider {
						continue
					}
					if activityBindingHasProviderID(record.bindings, other, id) {
						return ErrActivityOwnerConflict
					}
				}
			}
			seen[id] = true
			aliases = append(aliases, id)
		}
		if target.bindings.ProviderAliases == nil {
			target.bindings.ProviderAliases = map[string][]string{}
		}
		if len(aliases) == 0 {
			delete(target.bindings.ProviderAliases, sessionID)
		} else {
			target.bindings.ProviderAliases[sessionID] = aliases
		}
		return r.writeBindings(binding.OwnerID, target.bindings)
	})
}

func (r *ActivityOwnerRegistry) SetTerminal(ownerID, terminalID string, live bool) error {
	return r.locked(func() error {
		if !r.local[ownerID] || terminalID == "" {
			return ErrActivityOwnerUnavailable
		}
		record, ok, err := r.readOwner(ownerID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrActivityOwnerUnavailable
		}
		if live {
			record.bindings.Terminals[terminalID] = true
		} else {
			delete(record.bindings.Terminals, terminalID)
			active := record.bindings.Active[:0]
			for _, b := range record.bindings.Active {
				if b.TerminalID == terminalID {
					delete(record.bindings.ProviderAliases, b.SessionID)
					record.bindings.Ended = append(record.bindings.Ended, activityEndedBinding{b, time.Now().UTC()})
				} else {
					active = append(active, b)
				}
			}
			record.bindings.Active = active
			trimActivityEnded(&record.bindings)
		}
		return r.writeBindings(ownerID, record.bindings)
	})
}
func trimActivityEnded(b *activityOwnerBindings) {
	if len(b.Ended) > 20 {
		b.Ended = b.Ended[len(b.Ended)-20:]
	}
}

func (r *ActivityOwnerRegistry) ResolveOwner(ownerID string) (ActivityOwnerDescriptor, bool, error) {
	var d ActivityOwnerDescriptor
	var found bool
	err := r.locked(func() error { v, ok, err := r.readOwner(ownerID); d = v.descriptor; found = ok; return err })
	return d, found, err
}
func (r *ActivityOwnerRegistry) ResolveTerminal(ownerID, terminalID string) (bool, error) {
	var found bool
	err := r.locked(func() error {
		v, ok, err := r.readOwner(ownerID)
		found = ok && v.bindings.Terminals[terminalID]
		return err
	})
	return found, err
}
func (r *ActivityOwnerRegistry) ResolveProvider(provider AgentID, providerSessionID string) (ActivityBinding, bool, error) {
	var out ActivityBinding
	var found bool
	err := r.locked(func() error {
		records, err := r.records()
		if err != nil {
			return err
		}
		for _, v := range records {
			for _, b := range v.bindings.Active {
				if b.Provider == provider && activityBindingHasProviderID(v.bindings, b, providerSessionID) && v.bindings.Terminals[b.TerminalID] {
					out = b
					found = true
					return nil
				}
			}
		}
		return nil
	})
	return out, found, err
}
func (r *ActivityOwnerRegistry) resolveSession(sessionID string) (ActivityOwnerDescriptor, ActivityBinding, bool, error) {
	records, err := r.records()
	if err != nil {
		return ActivityOwnerDescriptor{}, ActivityBinding{}, false, err
	}
	for _, v := range records {
		for _, b := range v.bindings.Active {
			if b.SessionID == sessionID {
				return v.descriptor, b, true, nil
			}
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].descriptor.Identity.ID < records[j].descriptor.Identity.ID })
	var descriptor ActivityOwnerDescriptor
	var binding ActivityBinding
	var released time.Time
	for _, v := range records {
		for _, b := range v.bindings.Ended {
			if b.Binding.SessionID == sessionID && b.ReleasedAt.After(released) {
				descriptor = v.descriptor
				binding = b.Binding
				released = b.ReleasedAt
			}
		}
	}
	return descriptor, binding, !released.IsZero(), nil
}
func (r *ActivityOwnerRegistry) ResolveSession(sessionID string) (ActivityOwnerDescriptor, bool, error) {
	var out ActivityOwnerDescriptor
	var found bool
	err := r.locked(func() error { var err error; out, _, found, err = r.resolveSession(sessionID); return err })
	return out, found, err
}

// ResolveBinding is for reading snapshots (including ended runs), never hook authorization.
func (r *ActivityOwnerRegistry) ResolveBinding(sessionID string) (ActivityBinding, bool, error) {
	var out ActivityBinding
	var found bool
	err := r.locked(func() error { var err error; _, out, found, err = r.resolveSession(sessionID); return err })
	return out, found, err
}

func (r *ActivityOwnerRegistry) Release(sessionID, runID string) error {
	return r.locked(func() error {
		for id := range r.local {
			v, ok, err := r.readOwner(id)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			active := v.bindings.Active[:0]
			changed := false
			for _, b := range v.bindings.Active {
				if b.SessionID == sessionID && b.RunID == runID {
					delete(v.bindings.ProviderAliases, b.SessionID)
					v.bindings.Ended = append(v.bindings.Ended, activityEndedBinding{b, time.Now().UTC()})
					changed = true
				} else {
					active = append(active, b)
				}
			}
			if changed {
				v.bindings.Active = active
				trimActivityEnded(&v.bindings)
				if err := r.writeBindings(id, v.bindings); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// QueueDir resolves one owner's queue. Consumers must pass their own owner ID;
// helper producers may resolve another validated live owner's queue.
func (r *ActivityOwnerRegistry) QueueDir(ownerID, kind string) (string, error) {
	if kind != "activity" && kind != "session" {
		return "", errors.New("invalid activity queue")
	}
	if _, ok, err := r.ResolveOwner(ownerID); err != nil {
		return "", err
	} else if !ok {
		return "", ErrActivityOwnerUnavailable
	}
	return filepath.Join(r.ownerDir(ownerID), "events", kind), nil
}

func (r *ActivityOwnerRegistry) Close() error {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return nil
	}
	return r.locked(func() error {
		var result error
		for id := range r.local {
			if validActivityOwnerID(id) {
				result = errors.Join(result, os.RemoveAll(r.ownerDir(id)))
			}
		}
		r.closed = true
		return result
	})
}

func (r *ActivityOwnerRegistry) removeOwner(id string) error {
	return r.locked(func() error {
		if !r.local[id] || !validActivityOwnerID(id) {
			return ErrActivityOwnerUnavailable
		}
		if err := os.RemoveAll(r.ownerDir(id)); err != nil {
			return err
		}
		delete(r.local, id)
		return nil
	})
}

// ResolveActiveBinding is for lifecycle helper authentication. Unlike
// snapshot lookup it never returns retained ended runs.
func (registry *ActivityOwnerRegistry) ResolveActiveBinding(sessionID string) (ActivityBinding, bool, error) {
	var out ActivityBinding
	found := false
	if registry == nil {
		return out, false, nil
	}
	err := registry.locked(func() error {
		records, err := registry.records()
		if err != nil {
			return err
		}
		for _, record := range records {
			for _, b := range record.bindings.Active {
				if b.SessionID == sessionID && record.bindings.Terminals[b.TerminalID] {
					out = b
					found = true
					return nil
				}
			}
		}
		return nil
	})
	return out, found, err
}

// HasActiveBindings is a cross-process mutation guard. A caller's session
// cache may predate a foreign root, so examine every proven live owner.
func (r *ActivityOwnerRegistry) HasActiveBindings() (bool, error) {
	active := false
	err := r.locked(func() error {
		records, err := r.records()
		if err != nil {
			return err
		}
		for _, record := range records {
			for _, binding := range record.bindings.Active {
				if record.bindings.Terminals[binding.TerminalID] {
					active = true
					return nil
				}
			}
		}
		return nil
	})
	return active, err
}
