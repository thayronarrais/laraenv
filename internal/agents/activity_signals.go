package agents

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"lara4ever/internal/atomicfile"
)

func validActivitySignal(e ActivityEvent) bool {
	if !validActivityOwnerID(e.OwnerID) || !safeSignalTerminalID.MatchString(e.TerminalID) || (e.Provider != AgentClaude && e.Provider != AgentCodex) || !safeProviderSession.MatchString(e.ProviderSessionID) {
		return false
	}
	if (e.SessionID == "") != (e.RunID == "") || (e.SessionID != "" && (!safeSignalSessionID.MatchString(e.SessionID) || !safeSignalSessionID.MatchString(e.RunID))) {
		return false
	}
	age := time.Since(e.ReceivedAt)
	maxAge := 30 * time.Second
	if e.SessionID == "" {
		maxAge = 5 * time.Second
	}
	if e.ReceivedAt.IsZero() || age > maxAge || age < -time.Second {
		return false
	}
	switch e.Kind {
	case ActivitySubagentStart, ActivitySubagentTurnStop, ActivitySubagentNeedsInput, ActivitySubagentError:
		if !safeProviderSession.MatchString(e.NodeID) || e.NodeID == e.ProviderSessionID {
			return false
		}
	case ActivityBackgroundInventory:
		if e.BackgroundTasks == nil {
			return false
		}
	default:
		return false
	}
	if e.ParentProviderID != "" && !safeProviderSession.MatchString(e.ParentProviderID) {
		return false
	}
	if e.TurnID != "" && !safeProviderSession.MatchString(e.TurnID) {
		return false
	}
	if activityHookText(e.AgentType) != e.AgentType {
		return false
	}
	if e.Model != nil && (activityHookText(e.Model.Name) != e.Model.Name || e.Model.Kind != ActivityModelObserved) {
		return false
	}
	if e.BackgroundTasks != nil {
		if !activityValidInventory(e.BackgroundTasks, e.ReceivedAt) {
			return false
		}
		if len(*e.BackgroundTasks) > 500 {
			return false
		}
		for _, t := range *e.BackgroundTasks {
			if !safeProviderSession.MatchString(t.ProviderNodeID) || activityHookText(t.Label) != t.Label || activityHookText(t.AgentType) != t.AgentType {
				return false
			}
		}
	}
	return true
}

func WriteActivitySignal(directory string, event ActivityEvent) error {
	if !validActivitySignal(event) {
		return errors.New("invalid activity envelope")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if len(data)+1 > ActivitySignalLimit {
		return errors.New("activity envelope too large")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(directory, uuid.NewString()+".json"), append(data, '\n'), 0600)
}

// Callers drain only QueueDir(theirOwnOwnerID,"activity") and revalidate the
// active owner/run binding before applying. Pending adoption is limited to 5s.
func DrainActivitySignals(directory string, handle func(ActivityEvent)) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	type pending struct {
		path  string
		event ActivityEvent
	}
	var items []pending
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		data, err := readActivitySignalFile(path)
		var e ActivityEvent
		if err == nil && json.Unmarshal(data, &e) == nil && validActivitySignal(e) {
			items = append(items, pending{path, e})
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].event.ReceivedAt.Before(items[j].event.ReceivedAt) })
	for _, item := range items {
		handle(item.event)
		if err := os.Remove(item.path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func readActivitySignalFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ActivitySignalLimit+1))
	if len(data) > ActivitySignalLimit {
		return nil, errors.New("activity envelope too large")
	}
	return data, err
}
