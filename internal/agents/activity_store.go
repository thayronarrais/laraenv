package agents

import (
	"encoding/base64"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const activityItemLimit = 500
const activityEndedRunLimit = 20

type activityRecord struct {
	snapshot       ActivitySnapshot
	terminalID     string
	providerID     string
	endedAt        *time.Time
	turns          map[string]string
	observedStarts map[string]bool
	// State evidence is ordered independently from metadata read completion.
	stateObservedAt map[string]time.Time
	// One proven immediate-parent identity per retained child, including when
	// that parent no longer fits the snapshot. This map shares node retention.
	parents             map[string]string
	inventoryObservedAt time.Time
}

// ActivityStore keeps only bounded, ephemeral metadata about observed runs.
// It does not persist AgentSession or any provider conversation content.
type ActivityStore struct {
	mu        sync.RWMutex
	now       func() time.Time
	sessions  map[string]*activityRecord
	revisions map[string]uint64
}

func NewActivityStore(now func() time.Time) *ActivityStore {
	if now == nil {
		now = time.Now
	}
	return &ActivityStore{now: now, sessions: make(map[string]*activityRecord), revisions: make(map[string]uint64)}
}

func (s *ActivityStore) Begin(run ActivityRun) {
	if !activityValidID(run.Session.ID) || !activityValidID(run.RunID) || !activityValidID(run.OwnerID) || !activityProvider(run.Session.AgentID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.sessions[run.Session.ID]; old != nil && old.snapshot.RunID == run.RunID && old.snapshot.SourceID == run.OwnerID {
		return
	}
	at := run.StartedAt
	if at.IsZero() {
		at = s.now()
	}
	rootID := activityNodeID(run.Session.AgentID, run.Session.ID, "root", "")
	root := ActivityNode{ID: rootID, ProviderNodeID: run.Session.ProviderSessionID, Kind: ActivityAgent, Label: activityText(run.Session.Title), State: activityRootState(run.Session.State), FirstObservedAt: at, LastObservedAt: at, ProjectName: activityText(run.Session.ProjectName), ProjectPath: run.Session.ProjectPath, WorktreePath: run.Session.WorktreePath, TerminalID: run.Session.TerminalID}
	if root.Label == "" {
		root.Label = string(run.Session.AgentID)
	}
	status, message := ActivityPartial, ActivityMessageHooksUnobserved
	if run.Session.AgentID == AgentKimi || run.Session.AgentID == AgentOpenCode {
		status, message = ActivityUnavailable, ActivityMessageUnsupportedProvider
	} else if run.Session.ProviderSessionID == "" {
		message = ActivityMessageNoProviderSession
	}
	record := &activityRecord{snapshot: ActivitySnapshot{SessionID: run.Session.ID, RunID: run.RunID, SourceID: run.OwnerID, RootID: rootID, Provider: run.Session.AgentID, ObservedSince: at, ObservedAt: at, Status: status, MessageCode: message, Nodes: []ActivityNode{root}, BackgroundTasks: []ActivityNode{}}, terminalID: run.Session.TerminalID, providerID: run.Session.ProviderSessionID, turns: make(map[string]string), observedStarts: make(map[string]bool), parents: make(map[string]string)}
	record.stateObservedAt = make(map[string]time.Time)
	s.advance(record)
	s.sessions[run.Session.ID] = record
}

func (s *ActivityStore) BindProvider(sessionID, runID, providerID string) {
	if !activityValidID(providerID) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active(sessionID, runID)
	if r == nil || r.providerID == providerID || r.providerID != "" {
		return
	}
	r.providerID = providerID
	r.snapshot.Nodes[0].ProviderNodeID = providerID
	if r.snapshot.MessageCode == ActivityMessageNoProviderSession {
		r.snapshot.MessageCode = ActivityMessageHooksUnobserved
	}
	r.snapshot.ObservedAt = activityLater(r.snapshot.ObservedAt, s.now())
	s.advance(r)
}

func (s *ActivityStore) RootChanged(session AgentSession, runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active(session.ID, runID)
	if r == nil || session.AgentID != r.snapshot.Provider || session.TerminalID != r.terminalID {
		return
	}
	before := cloneActivitySnapshot(r.snapshot)
	root := &r.snapshot.Nodes[0]
	root.Label = activityText(session.Title)
	if root.Label == "" {
		root.Label = string(session.AgentID)
	}
	root.State = activityRootState(session.State)
	root.ProjectName, root.ProjectPath = activityText(session.ProjectName), session.ProjectPath
	root.WorktreePath, root.TerminalID = session.WorktreePath, session.TerminalID
	if reflect.DeepEqual(before, r.snapshot) {
		return
	}
	at := activityLater(r.snapshot.ObservedAt, s.now())
	root.LastObservedAt, r.snapshot.ObservedAt = at, at
	// UpdatedAt is deliberately never used as a turn ending or duration.
	s.advance(r)
}

func (s *ActivityStore) Apply(event ActivityEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active(event.SessionID, event.RunID)
	if r == nil || !activityChildrenProvider(r.snapshot.Provider) || event.OwnerID != r.snapshot.SourceID || event.TerminalID == "" || event.TerminalID != r.terminalID || event.Provider != r.snapshot.Provider || r.providerID == "" || event.ProviderSessionID != r.providerID || event.ReceivedAt.IsZero() || event.ReceivedAt.Before(r.snapshot.ObservedSince) {
		return false
	}
	if event.Kind != ActivitySubagentStart && event.Kind != ActivitySubagentTurnStop && event.Kind != ActivitySubagentNeedsInput && event.Kind != ActivitySubagentError && event.Kind != ActivityBackgroundInventory {
		return false
	}
	if event.TurnID != "" && !activityValidID(event.TurnID) {
		return false
	}
	if !activityValidInventory(event.BackgroundTasks, event.ReceivedAt) {
		return false
	}
	before := cloneActivitySnapshot(r.snapshot)
	if event.Kind != ActivityBackgroundInventory {
		if !activityValidID(event.NodeID) || event.NodeID == r.providerID {
			return false
		}
		parentID, ok := activityParent(r, event.NodeID, event.ParentProviderID)
		if !ok {
			return false
		}
		index := activityFind(r.snapshot.Nodes, event.NodeID)
		if index < 0 && event.Kind != ActivitySubagentStart {
			return false
		}
		if event.ReceivedAt.Before(r.stateObservedAt[event.NodeID]) {
			return false
		}
		// Stop hooks often omit ancestry. Preserve a previously proven parent.
		if index >= 0 && event.ParentProviderID == "" {
			parentID = r.snapshot.Nodes[index].ParentID
		}
		if event.Kind != ActivitySubagentStart && (!r.observedStarts[event.NodeID] || (event.TurnID != "" && r.turns[event.NodeID] != "" && event.TurnID != r.turns[event.NodeID])) {
			return false
		}
		if index < 0 {
			r.snapshot.Nodes = append(r.snapshot.Nodes, ActivityNode{ID: activityNodeID(event.Provider, event.SessionID, "agent", event.NodeID), ProviderNodeID: event.NodeID, Kind: ActivityAgent, FirstObservedAt: event.ReceivedAt})
			index = len(r.snapshot.Nodes) - 1
		}
		node := &r.snapshot.Nodes[index]
		node.ParentID, node.LastObservedAt = parentID, activityLater(node.LastObservedAt, event.ReceivedAt)
		r.stateObservedAt[event.NodeID] = event.ReceivedAt
		if event.ParentProviderID != "" {
			r.parents[event.NodeID] = event.ParentProviderID
		} else if r.parents[event.NodeID] == "" {
			r.parents[event.NodeID] = r.providerID
		}
		if event.AgentType != "" {
			node.AgentType = activityText(event.AgentType)
		}
		if node.Label == "" {
			node.Label = node.AgentType
			if node.Label == "" {
				node.Label = "Subagent"
			}
		}
		if model := activityModel(event.Model, false); model != nil && (node.Model == nil || node.Model.Kind != ActivityModelObserved || model.Kind == ActivityModelObserved) {
			node.Model = model
		}
		switch event.Kind {
		case ActivitySubagentStart:
			if !r.observedStarts[event.NodeID] || activityFinal(node.State) || (event.TurnID != "" && r.turns[event.NodeID] != event.TurnID) {
				node.StartedAt = activityTime(event.ReceivedAt)
				node.EndedAt = nil
			}
			node.State = ActivityWorking
			r.observedStarts[event.NodeID] = true
			if event.TurnID != "" {
				r.turns[event.NodeID] = event.TurnID
			}
		case ActivitySubagentTurnStop:
			node.State, node.EndedAt = ActivityReplyFinished, activityTime(event.ReceivedAt)
		case ActivitySubagentNeedsInput:
			node.State = ActivityNeedsInput
		case ActivitySubagentError:
			node.State, node.EndedAt = ActivityError, activityTime(event.ReceivedAt)
		}
		r.snapshot.Capabilities.Children, r.snapshot.Capabilities.LiveState, r.snapshot.Capabilities.Duration = true, true, true
		if node.Model != nil {
			r.snapshot.Capabilities.Model = true
		}
	}
	if event.BackgroundTasks != nil {
		activityMergeInventory(r, *event.BackgroundTasks, event.ReceivedAt)
	}
	r.snapshot.Truncated = r.snapshot.Truncated || event.Truncated
	r.snapshot.Status, r.snapshot.MessageCode, r.snapshot.Stale = ActivityAvailable, ActivityMessageNone, false
	r.snapshot.ObservedAt = activityLater(r.snapshot.ObservedAt, event.ReceivedAt)
	activityRetain(r)
	if reflect.DeepEqual(before, r.snapshot) {
		return false
	}
	s.advance(r)
	return true
}

func (s *ActivityStore) MergeMetadata(sessionID, runID string, result ActivityMetadataResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active(sessionID, runID)
	if r == nil {
		return false
	}
	if !activityChildrenProvider(r.snapshot.Provider) && (len(result.Nodes) > 0 || result.BackgroundTasks != nil) {
		return false
	}
	at := result.ObservedAt
	if at.IsZero() {
		at = s.now()
	}
	if at.Before(r.snapshot.ObservedSince) || !activityValidInventory(result.BackgroundTasks, at) {
		return false
	}
	// Validate the whole proposed ancestry before applying any part of a page.
	parents := make(map[string]string, len(r.snapshot.Nodes)+len(result.Nodes))
	for child, parent := range r.parents {
		parents[child] = parent
	}
	seen := make(map[string]bool, len(result.Nodes))
	for _, node := range result.Nodes {
		if !activityValidID(node.ProviderNodeID) || node.ProviderNodeID == r.providerID || seen[node.ProviderNodeID] || (node.ParentProviderID != "" && !activityValidID(node.ParentProviderID)) {
			return false
		}
		seen[node.ProviderNodeID] = true
		parent := node.ParentProviderID
		if parent == "" {
			parent = r.parents[node.ProviderNodeID]
			if parent == "" {
				parent = r.providerID
			}
		}
		parents[node.ProviderNodeID] = parent
	}
	for id := range seen {
		visited := map[string]bool{id: true}
		previous := id
		for parent := parents[id]; parent != r.providerID; {
			if parent == "" || visited[parent] {
				return false
			}
			visited[parent] = true
			next, exists := parents[parent]
			if !exists {
				// Only a previously proven edge can cross a retention gap.
				if r.parents[previous] != parent {
					return false
				}
				break
			}
			previous, parent = parent, next
		}
	}
	before := cloneActivitySnapshot(r.snapshot)
	for _, metadata := range result.Nodes {
		index := activityFind(r.snapshot.Nodes, metadata.ProviderNodeID)
		if index < 0 {
			r.snapshot.Nodes = append(r.snapshot.Nodes, ActivityNode{ID: activityNodeID(r.snapshot.Provider, sessionID, "agent", metadata.ProviderNodeID), ProviderNodeID: metadata.ProviderNodeID, Kind: ActivityAgent, State: ActivityUnknown, FirstObservedAt: at, LastObservedAt: at})
			index = len(r.snapshot.Nodes) - 1
		}
		node := &r.snapshot.Nodes[index]
		if at.Before(node.LastObservedAt) {
			continue
		}
		parent := parents[metadata.ProviderNodeID]
		r.parents[metadata.ProviderNodeID] = parent
		node.ParentID = r.snapshot.RootID
		if parent != r.providerID {
			node.ParentID = activityNodeID(r.snapshot.Provider, sessionID, "agent", parent)
		}
		if label := activityText(metadata.Label); label != "" {
			node.Label = label
		}
		if agentType := activityText(metadata.AgentType); agentType != "" {
			node.AgentType = agentType
		}
		if node.Label == "" {
			node.Label = node.AgentType
			if node.Label == "" {
				node.Label = "Subagent"
			}
		}
		node.LastObservedAt = at
		if node.Model == nil || node.Model.Kind != ActivityModelObserved {
			if model := activityModel(metadata.Model, true); model != nil {
				node.Model = model
			}
		}
		if node.Model != nil {
			r.snapshot.Capabilities.Model = true
		}
	}
	if result.BackgroundTasks != nil {
		activityMergeInventory(r, *result.BackgroundTasks, at)
	}
	r.snapshot.Capabilities.Children = r.snapshot.Capabilities.Children || result.Capabilities.Children || len(result.Nodes) > 0
	// Metadata alone cannot enable live state, duration, or per-agent tokens.
	r.snapshot.Capabilities.LiveState = before.Capabilities.LiveState && !result.liveObservationDisabled
	if result.liveObservationDisabled {
		activityDisableLiveObservation(r)
	}
	if result.Status != "" {
		r.snapshot.Status = result.Status
	}
	if result.MessageCode != "" {
		r.snapshot.MessageCode = result.MessageCode
	}
	r.snapshot.Stale = result.Stale
	r.snapshot.Truncated = r.snapshot.Truncated || result.Truncated
	r.snapshot.ObservedAt = activityLater(r.snapshot.ObservedAt, at)
	activityRetain(r)
	if reflect.DeepEqual(before, r.snapshot) {
		return false
	}
	s.advance(r)
	return true
}

func (s *ActivityStore) End(sessionID, runID string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active(sessionID, runID)
	if r == nil {
		return
	}
	if at.IsZero() {
		at = s.now()
	}
	at = activityLater(at, r.snapshot.ObservedAt)
	r.endedAt = activityTime(at)
	r.stateObservedAt = nil
	r.snapshot.Nodes[0].State = ActivityStopped
	r.snapshot.Nodes[0].LastObservedAt = at
	for i := 1; i < len(r.snapshot.Nodes); i++ {
		if !activityFinal(r.snapshot.Nodes[i].State) {
			r.snapshot.Nodes[i].State = ActivityUnknown
		}
	}
	for i := range r.snapshot.BackgroundTasks {
		if !activityFinal(r.snapshot.BackgroundTasks[i].State) {
			r.snapshot.BackgroundTasks[i].State = ActivityUnknown
		}
	}
	r.snapshot.Stale, r.snapshot.Status, r.snapshot.MessageCode, r.snapshot.ObservedAt = true, ActivityPartial, ActivityMessageObservationEnded, at
	s.advance(r)
	type endedRun struct {
		id string
		at time.Time
	}
	ended := make([]endedRun, 0)
	for id, run := range s.sessions {
		if run.endedAt != nil {
			ended = append(ended, endedRun{id, *run.endedAt})
		}
	}
	sort.Slice(ended, func(i, j int) bool {
		if ended[i].at.Equal(ended[j].at) {
			return ended[i].id < ended[j].id
		}
		return ended[i].at.Before(ended[j].at)
	})
	for len(ended) > activityEndedRunLimit {
		delete(s.sessions, ended[0].id)
		ended = ended[1:]
	}
}

func (s *ActivityStore) disableLiveObservation(sessionID, runID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.active(sessionID, runID)
	if r == nil || !activityDisableLiveObservation(r) {
		return false
	}
	s.advance(r)
	return true
}

func activityDisableLiveObservation(r *activityRecord) bool {
	changed := r.snapshot.Capabilities.LiveState
	r.snapshot.Capabilities.LiveState = false
	// A later event from another child must not revive this child's old state.
	// Preserve real interval timestamps, including completed intervals.
	for i := 1; i < len(r.snapshot.Nodes); i++ {
		if r.snapshot.Nodes[i].State != ActivityUnknown && !activityFinal(r.snapshot.Nodes[i].State) {
			r.snapshot.Nodes[i].State = ActivityUnknown
			changed = true
		}
	}
	for i := range r.snapshot.BackgroundTasks {
		if r.snapshot.BackgroundTasks[i].State != ActivityUnknown && !activityFinal(r.snapshot.BackgroundTasks[i].State) {
			r.snapshot.BackgroundTasks[i].State = ActivityUnknown
			changed = true
		}
	}
	return changed
}

func (s *ActivityStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions, s.revisions = make(map[string]*activityRecord), make(map[string]uint64)
}

func (s *ActivityStore) Snapshot(sessionID string) (ActivitySnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if r := s.sessions[sessionID]; r != nil {
		return cloneActivitySnapshot(r.snapshot), true
	}
	return ActivitySnapshot{}, false
}

func (s *ActivityStore) active(sessionID, runID string) *activityRecord {
	r := s.sessions[sessionID]
	if r == nil || r.snapshot.RunID != runID || r.endedAt != nil {
		return nil
	}
	return r
}

func (s *ActivityStore) advance(r *activityRecord) {
	key := r.snapshot.SourceID + "\x00" + r.snapshot.SessionID
	s.revisions[key]++
	r.snapshot.Revision = s.revisions[key]
}

func activityNodeID(provider AgentID, sessionID, kind, providerID string) string {
	return string(provider) + ":" + base64.RawURLEncoding.EncodeToString([]byte(sessionID)) + ":" + kind + ":" + base64.RawURLEncoding.EncodeToString([]byte(providerID))
}

func activityValidID(value string) bool {
	if strings.TrimSpace(value) == "" || len(value) > 512 {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func activityProvider(id AgentID) bool {
	return id == AgentClaude || id == AgentCodex || id == AgentKimi || id == AgentOpenCode
}

func activityChildrenProvider(id AgentID) bool { return id == AgentClaude || id == AgentCodex }

func activityText(value string) string {
	runes := make([]rune, 0, 240)
	for _, r := range value {
		if unicode.IsControl(r) {
			continue
		}
		if len(runes) == 240 {
			break
		}
		runes = append(runes, r)
	}
	return strings.TrimSpace(string(runes))
}

func activityRootState(state SessionState) ActivityState {
	switch state {
	case SessionStarting:
		return ActivityStarting
	case SessionWorking:
		return ActivityWorking
	case SessionNeedsInput:
		return ActivityNeedsInput
	case SessionDone:
		return ActivityReplyFinished
	case SessionIdle:
		return ActivityIdle
	case SessionStopped:
		return ActivityStopped
	case SessionError:
		return ActivityError
	default:
		return ActivityUnknown
	}
}

func activityFinal(state ActivityState) bool {
	return state == ActivityReplyFinished || state == ActivityStopped || state == ActivityInterrupted || state == ActivityError
}

func activityFind(nodes []ActivityNode, providerID string) int {
	for i := range nodes {
		if nodes[i].ProviderNodeID == providerID {
			return i
		}
	}
	return -1
}

func activityParent(r *activityRecord, childID, parentProviderID string) (string, bool) {
	if parentProviderID == "" || parentProviderID == r.providerID {
		return r.snapshot.RootID, true
	}
	if !activityValidID(parentProviderID) || parentProviderID == childID {
		return "", false
	}
	index := activityFind(r.snapshot.Nodes, parentProviderID)
	if index < 1 {
		return "", r.parents[childID] == parentProviderID
	}
	parent := r.snapshot.Nodes[index]
	visited := make(map[string]bool)
	for cursor := parentProviderID; cursor != r.providerID; {
		if cursor == childID || visited[cursor] {
			return "", false
		}
		visited[cursor] = true
		next, exists := r.parents[cursor]
		if !exists {
			break
		} // Membership was proven before this ancestor was evicted.
		cursor = next
	}
	return parent.ID, true
}

func activityModel(model *ActivityModel, metadata bool) *ActivityModel {
	if model == nil {
		return nil
	}
	name := activityText(model.Name)
	if name == "" {
		return nil
	}
	kind := model.Kind
	if metadata || kind != ActivityModelObserved {
		kind = ActivityModelConfigured
	}
	return &ActivityModel{Name: name, Kind: kind}
}

func activityValidInventory(items *[]ActivityBackgroundTask, at time.Time) bool {
	if items == nil {
		return true
	}
	seen := make(map[string]bool)
	for _, item := range *items {
		if !activityValidID(item.ProviderNodeID) || seen[item.ProviderNodeID] {
			return false
		}
		seen[item.ProviderNodeID] = true
		switch item.State {
		case ActivityStarting, ActivityWorking, ActivityNeedsInput, ActivityReplyFinished, ActivityIdle, ActivityStopped, ActivityInterrupted, ActivityError, ActivityUnknown, "":
		default:
			return false
		}
		if item.StartedAt != nil && item.StartedAt.After(at) {
			return false
		}
		if item.EndedAt != nil && (item.EndedAt.After(at) || item.StartedAt != nil && item.EndedAt.Before(*item.StartedAt)) {
			return false
		}
	}
	return true
}

func activityMergeInventory(r *activityRecord, items []ActivityBackgroundTask, at time.Time) {
	if at.Before(r.inventoryObservedAt) {
		return
	}
	r.inventoryObservedAt = at
	snapshot := &r.snapshot
	present := make(map[string]bool, len(items))
	for _, item := range items {
		present[item.ProviderNodeID] = true
		index := activityFind(snapshot.BackgroundTasks, item.ProviderNodeID)
		if index < 0 {
			snapshot.BackgroundTasks = append(snapshot.BackgroundTasks, ActivityNode{ID: activityNodeID(snapshot.Provider, snapshot.SessionID, "background-task", item.ProviderNodeID), ProviderNodeID: item.ProviderNodeID, Kind: ActivityBackgroundTaskNode, FirstObservedAt: at, State: ActivityUnknown})
			index = len(snapshot.BackgroundTasks) - 1
		}
		node := &snapshot.BackgroundTasks[index]
		if at.Before(node.LastObservedAt) {
			continue
		}
		node.Label, node.AgentType, node.LastObservedAt = activityText(item.Label), activityText(item.AgentType), at
		if node.Label == "" {
			node.Label = "Background task"
		}
		previousState := node.State
		node.State = item.State
		if node.State == "" {
			node.State = ActivityUnknown
		}
		// An explicit different start, or reactivation after a finished interval,
		// starts a new evidence interval; absent fields cannot reuse its predecessor.
		newInterval := item.StartedAt != nil && (node.StartedAt == nil || !item.StartedAt.Equal(*node.StartedAt))
		if newInterval || activityFinal(previousState) && !activityFinal(node.State) {
			node.StartedAt, node.EndedAt = nil, nil
		}
		if item.StartedAt != nil {
			node.StartedAt = activityTime(*item.StartedAt)
		}
		if item.EndedAt != nil {
			node.EndedAt = activityTime(*item.EndedAt)
		} else if !activityFinal(node.State) {
			node.EndedAt = nil
		}
		if node.StartedAt != nil && node.EndedAt != nil && node.EndedAt.Before(*node.StartedAt) {
			node.StartedAt = nil
		}
		if node.StartedAt != nil {
			snapshot.Capabilities.Duration = true
		}
	}
	for i := range snapshot.BackgroundTasks {
		node := &snapshot.BackgroundTasks[i]
		if !present[node.ProviderNodeID] && !at.Before(node.LastObservedAt) && !activityFinal(node.State) {
			node.State = ActivityUnknown
			node.LastObservedAt = at
		}
	}
	snapshot.Capabilities.BackgroundTasks = true
	if len(items) > 0 {
		snapshot.Capabilities.LiveState = true
	}
}

func activityRetain(r *activityRecord) {
	snapshot := &r.snapshot
	if len(snapshot.Nodes)+len(snapshot.BackgroundTasks) > activityItemLimit {
		items := make([]ActivityNode, 0, len(snapshot.Nodes)-1+len(snapshot.BackgroundTasks))
		items = append(items, snapshot.Nodes[1:]...)
		items = append(items, snapshot.BackgroundTasks...)
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if activityFinal(a.State) != activityFinal(b.State) {
				return !activityFinal(a.State)
			}
			at, bt := a.LastObservedAt, b.LastObservedAt
			if activityFinal(a.State) && a.EndedAt != nil {
				at = *a.EndedAt
			}
			if activityFinal(b.State) && b.EndedAt != nil {
				bt = *b.EndedAt
			}
			if !at.Equal(bt) {
				return at.After(bt)
			}
			return a.ID < b.ID
		})
		snapshot.Nodes, snapshot.BackgroundTasks = snapshot.Nodes[:1], []ActivityNode{}
		for _, item := range items[:activityItemLimit-1] {
			if item.Kind == ActivityBackgroundTaskNode {
				snapshot.BackgroundTasks = append(snapshot.BackgroundTasks, item)
			} else {
				snapshot.Nodes = append(snapshot.Nodes, item)
			}
		}
		snapshot.Truncated = true
		kept := make(map[string]bool)
		for _, node := range snapshot.Nodes {
			kept[node.ProviderNodeID] = true
		}
		for id := range r.observedStarts {
			if !kept[id] {
				delete(r.turns, id)
				delete(r.observedStarts, id)
				delete(r.stateObservedAt, id)
			}
		}
		for id := range r.parents {
			if !kept[id] {
				delete(r.parents, id)
			}
		}
	}
	// Project only retained immediate-parent relationships into the DTO. A gap
	// uses the UI's root-membership grouping, never a fabricated direct root edge.
	retained := make(map[string]string, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		retained[node.ProviderNodeID] = node.ID
	}
	for i := 1; i < len(snapshot.Nodes); i++ {
		node := &snapshot.Nodes[i]
		node.ParentID = retained[r.parents[node.ProviderNodeID]]
	}
	less := func(a, b ActivityNode) bool {
		if !a.FirstObservedAt.Equal(b.FirstObservedAt) {
			return a.FirstObservedAt.Before(b.FirstObservedAt)
		}
		return a.ID < b.ID
	}
	sort.Slice(snapshot.Nodes[1:], func(i, j int) bool { return less(snapshot.Nodes[i+1], snapshot.Nodes[j+1]) })
	sort.Slice(snapshot.BackgroundTasks, func(i, j int) bool { return less(snapshot.BackgroundTasks[i], snapshot.BackgroundTasks[j]) })
	if snapshot.Truncated && snapshot.Status == ActivityAvailable {
		snapshot.Status, snapshot.MessageCode = ActivityPartial, ActivityMessageTruncated
	}
}

func activityTime(at time.Time) *time.Time { result := at; return &result }
func activityLater(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func cloneActivitySnapshot(snapshot ActivitySnapshot) ActivitySnapshot {
	snapshot.Nodes = cloneActivityNodes(snapshot.Nodes)
	snapshot.BackgroundTasks = cloneActivityNodes(snapshot.BackgroundTasks)
	return snapshot
}

func cloneActivityNodes(nodes []ActivityNode) []ActivityNode {
	result := make([]ActivityNode, len(nodes))
	for i, node := range nodes {
		if node.StartedAt != nil {
			node.StartedAt = activityTime(*node.StartedAt)
		}
		if node.EndedAt != nil {
			node.EndedAt = activityTime(*node.EndedAt)
		}
		if node.Model != nil {
			model := *node.Model
			node.Model = &model
		}
		if node.Tokens != nil {
			tokens := *node.Tokens
			if tokens.Input != nil {
				value := *tokens.Input
				tokens.Input = &value
			}
			if tokens.Output != nil {
				value := *tokens.Output
				tokens.Output = &value
			}
			if tokens.Total != nil {
				value := *tokens.Total
				tokens.Total = &value
			}
			node.Tokens = &tokens
		}
		result[i] = node
	}
	return result
}
