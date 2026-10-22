package agents

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"lara4ever/internal/atomicfile"
	"lara4ever/internal/i18n"
)

type SignalKind string

const (
	SignalDone       SignalKind = "done"
	SignalNeedsInput SignalKind = "needs-input"
	SignalError      SignalKind = "error"
	// Adopted-session lifecycle (provider hooks in LaraEnv-owned terminals).
	SignalStarted SignalKind = "started"
	SignalWorking SignalKind = "working"
	SignalEnded   SignalKind = "ended"
)

// SessionSignal is the only thing provider hooks hand to LaraEnv. Launched
// sessions identify themselves by SessionID; adopted sessions by TerminalID
// plus provider metadata. Nothing here ever contains conversation content.
type SessionSignal struct {
	OwnerID           string     `json:"ownerId,omitempty"`
	RunID             string     `json:"runId,omitempty"`
	SessionID         string     `json:"sessionId,omitempty"`
	Kind              SignalKind `json:"kind"`
	CreatedAt         time.Time  `json:"createdAt"`
	TerminalID        string     `json:"terminalId,omitempty"`
	Provider          AgentID    `json:"provider,omitempty"`
	ProviderSessionID string     `json:"providerSessionId,omitempty"`
	Cwd               string     `json:"cwd,omitempty"`
	Source            string     `json:"source,omitempty"`
}

var (
	safeSignalSessionID  = regexp.MustCompile(`^[A-Za-z0-9-]{1,100}$`)
	safeSignalTerminalID = regexp.MustCompile(`^term-[0-9]{1,12}$`)
	safeProviderSession  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)
	safeSignalSource     = regexp.MustCompile(`^[a-z_-]{0,32}$`)
)

func WriteSessionSignal(directory, sessionID string, kind SignalKind) error {
	return WriteSignal(directory, SessionSignal{SessionID: strings.TrimSpace(sessionID), Kind: kind})
}

// WriteSignal validates and persists one envelope. CreatedAt is always
// assigned here so hook processes cannot backdate or postdate events.
func WriteSignal(directory string, signal SessionSignal) error {
	signal.CreatedAt = time.Now().UTC()
	if !validSignal(signal) {
		return i18n.Errorf("errors.agents.invalidSignalEnvelope", nil)
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(directory, uuid.NewString()+".json"), append(data, '\n'), 0644)
}

func containsControlRune(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validSignal(signal SessionSignal) bool {
	if signal.OwnerID != "" {
		if !validActivityOwnerID(signal.OwnerID) || !safeSignalTerminalID.MatchString(signal.TerminalID) {
			return false
		}
		if !knownProvider(signal.Provider) || (signal.ProviderSessionID != "" && !safeProviderSession.MatchString(signal.ProviderSessionID)) || len(signal.Cwd) > 4096 || containsControlRune(signal.Cwd) || !safeSignalSource.MatchString(signal.Source) {
			return false
		}
		if signal.SessionID != "" && !safeSignalSessionID.MatchString(signal.RunID) {
			return false
		}
		if signal.SessionID == "" && signal.RunID != "" {
			return false
		}
	} else if signal.RunID != "" {
		return false
	}
	if !validSignalKind(signal.Kind) {
		return false
	}
	if signal.SessionID != "" {
		return safeSignalSessionID.MatchString(signal.SessionID)
	}
	if !safeSignalTerminalID.MatchString(signal.TerminalID) || !knownProvider(signal.Provider) {
		return false
	}
	if signal.ProviderSessionID != "" && !safeProviderSession.MatchString(signal.ProviderSessionID) {
		return false
	}
	if len(signal.Cwd) > 4096 || containsControlRune(signal.Cwd) {
		return false
	}
	return safeSignalSource.MatchString(signal.Source)
}

func knownProvider(id AgentID) bool {
	switch id {
	case AgentClaude, AgentCodex, AgentKimi, AgentOpenCode:
		return true
	default:
		return false
	}
}

// DrainSessionSignals consumes only LaraEnv's small, schema-validated event
// envelopes. Provider hook payloads, prompts, messages and transcripts are
// never written to this directory.
func DrainSessionSignals(directory string, handle func(SessionSignal)) error {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type pendingSignal struct {
		path   string
		signal SessionSignal
	}
	var pending []pendingSignal
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, readErr := readActivitySignalFile(path)
		if readErr == nil && len(data) <= 16*1024 {
			var signal SessionSignal
			if json.Unmarshal(data, &signal) == nil && validSignal(signal) {
				pending = append(pending, pendingSignal{path: path, signal: signal})
				continue
			}
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
	}
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].signal.CreatedAt.Before(pending[j].signal.CreatedAt) })
	for _, item := range pending {
		handle(item.signal)
		if removeErr := os.Remove(item.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
	}
	return nil
}

func validSignalKind(kind SignalKind) bool {
	switch kind {
	case SignalDone, SignalNeedsInput, SignalError, SignalStarted, SignalWorking, SignalEnded:
		return true
	default:
		return false
	}
}
