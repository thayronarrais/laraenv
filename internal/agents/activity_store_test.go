package agents

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func activityFixture() (*ActivityStore, ActivityRun, *time.Time) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	run := ActivityRun{Session: AgentSession{ID: "session-a", AgentID: AgentClaude, TerminalID: "terminal-a", ProviderSessionID: "provider-root", Title: "Root", ProjectName: "project", ProjectPath: "C:/project", State: SessionWorking}, RunID: "run-a", OwnerID: "owner-a", StartedAt: now}
	store := NewActivityStore(func() time.Time { return now })
	store.Begin(run)
	return store, run, &now
}

func activityFixtureEvent(run ActivityRun, at time.Time, kind ActivityEventKind, node string) ActivityEvent {
	return ActivityEvent{OwnerID: run.OwnerID, TerminalID: run.Session.TerminalID, SessionID: run.Session.ID, RunID: run.RunID, Provider: run.Session.AgentID, ProviderSessionID: run.Session.ProviderSessionID, NodeID: node, Kind: kind, ReceivedAt: at}
}

func TestActivityStoreQueuedStateSurvivesLaterMetadata(t *testing.T) {
	for _, metadataFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(metadataFirst), func(t *testing.T) {
			store, run, now := activityFixture()
			start := activityFixtureEvent(run, now.Add(time.Second), ActivitySubagentStart, "child")
			start.TurnID = "turn"
			metadata := ActivityMetadataResult{ObservedAt: now.Add(3 * time.Second), Nodes: []ActivityMetadataNode{{ProviderNodeID: "child", Label: "Known child"}}}
			if metadataFirst {
				store.MergeMetadata(run.Session.ID, run.RunID, metadata)
			}
			if !store.Apply(start) {
				t.Fatal("queued start rejected by metadata completion")
			}
			if !metadataFirst {
				store.MergeMetadata(run.Session.ID, run.RunID, metadata)
			}
			stop := start
			stop.Kind, stop.ReceivedAt = ActivitySubagentTurnStop, now.Add(2*time.Second)
			if !store.Apply(stop) {
				t.Fatal("queued stop rejected by metadata completion")
			}
			got, _ := store.Snapshot(run.Session.ID)
			child := got.Nodes[1]
			if child.State != ActivityReplyFinished || !child.StartedAt.Equal(start.ReceivedAt) || !child.EndedAt.Equal(stop.ReceivedAt) || !child.LastObservedAt.Equal(metadata.ObservedAt) || !got.ObservedAt.Equal(metadata.ObservedAt) {
				t.Fatalf("incorrect interval or regressed observation: %+v", child)
			}
			if store.Apply(start) {
				t.Fatal("genuinely old state accepted")
			}
		})
	}
}

// Losing reply-finished semantics or creating a new identity on reactivation breaks this test.
func TestActivityStoreTurnStopAndReactivation(t *testing.T) {
	store, run, now := activityFixture()
	event := activityFixtureEvent(run, *now, ActivitySubagentStart, "child")
	event.TurnID = "turn-a"
	if !store.Apply(event) {
		t.Fatal("start rejected")
	}
	*now = now.Add(time.Second)
	event.Kind, event.ReceivedAt = ActivitySubagentTurnStop, *now
	if !store.Apply(event) {
		t.Fatal("stop rejected")
	}
	got, _ := store.Snapshot(run.Session.ID)
	if len(got.Nodes) != 2 || got.Nodes[1].State != ActivityReplyFinished || got.Nodes[1].EndedAt == nil {
		t.Fatalf("missing reply end: %+v", got)
	}
	id, first := got.Nodes[1].ID, got.Nodes[1].FirstObservedAt
	*now = now.Add(time.Second)
	event.Kind, event.ReceivedAt, event.TurnID = ActivitySubagentStart, *now, "turn-b"
	store.Apply(event)
	got, _ = store.Snapshot(run.Session.ID)
	if len(got.Nodes) != 2 || got.Nodes[1].ID != id || got.Nodes[1].EndedAt != nil || got.Nodes[1].State != ActivityWorking || !got.Nodes[1].StartedAt.Equal(*now) || got.Nodes[1].FirstObservedAt != first {
		t.Fatalf("reactivation: %+v", got.Nodes)
	}
	// A delayed stop belonging to the earlier turn must not end the current turn.
	event.Kind, event.TurnID = ActivitySubagentTurnStop, "turn-a"
	if store.Apply(event) {
		t.Fatal("old turn accepted")
	}
}

func TestActivityStoreRootExitKeepsChildUnknown(t *testing.T) {
	store, run, now := activityFixture()
	store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
	*now = now.Add(time.Second)
	store.End(run.Session.ID, run.RunID, *now)
	got, _ := store.Snapshot(run.Session.ID)
	if got.Nodes[1].State != ActivityUnknown || got.Nodes[1].EndedAt != nil || !got.Stale || got.MessageCode != ActivityMessageObservationEnded {
		t.Fatalf("invented child ending: %+v", got)
	}
	if store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "late")) {
		t.Fatal("ended run accepted event")
	}
}

func TestActivityStoreRetention(t *testing.T) {
	store, run, now := activityFixture()
	store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "finished"))
	store.Apply(activityFixtureEvent(run, *now, ActivitySubagentTurnStop, "finished"))
	for i := 0; i < 499; i++ {
		*now = now.Add(time.Millisecond)
		store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, fmt.Sprintf("working-%03d", i)))
	}
	got, _ := store.Snapshot(run.Session.ID)
	if len(got.Nodes)+len(got.BackgroundTasks) != 500 || !got.Truncated {
		t.Fatalf("retention: %d %+v", len(got.Nodes), got.Truncated)
	}
	if got.Nodes[0].ID != got.RootID {
		t.Fatal("root evicted")
	}
	if states := store.sessions[run.Session.ID].stateObservedAt; len(states) != 499 || !states["finished"].IsZero() {
		t.Fatal("state watermark retained evicted nodes")
	}
	for _, node := range got.Nodes {
		if node.ProviderNodeID == "finished" {
			t.Fatal("finished node preferred to active one")
		}
	}
	store.End(run.Session.ID, run.RunID, *now)
	if len(store.sessions[run.Session.ID].stateObservedAt) != 0 {
		t.Fatal("ended run kept state watermarks")
	}
	run.RunID = "replacement"
	store.Begin(run)
	if len(store.sessions[run.Session.ID].stateObservedAt) != 0 {
		t.Fatal("new run inherited state watermarks")
	}
}

func TestActivityStoreEndedRunRetention(t *testing.T) {
	store, run, now := activityFixture()
	for i := 0; i < 21; i++ {
		run.Session.ID, run.RunID = fmt.Sprintf("session-%02d", i), fmt.Sprintf("run-%02d", i)
		store.Begin(run)
		*now = now.Add(time.Second)
		store.End(run.Session.ID, run.RunID, *now)
	}
	if _, ok := store.Snapshot("session-00"); ok {
		t.Fatal("oldest ended run retained")
	}
	if _, ok := store.Snapshot("session-01"); !ok {
		t.Fatal("recent ended run evicted")
	}
	if _, ok := store.Snapshot("session-a"); !ok {
		t.Fatal("active run evicted")
	}
}

func TestActivityStoreInventoryAbsentVersusEmpty(t *testing.T) {
	store, run, now := activityFixture()
	items := []ActivityBackgroundTask{{ProviderNodeID: "task", Label: "Background", State: ActivityWorking}}
	event := activityFixtureEvent(run, *now, ActivityBackgroundInventory, "")
	event.BackgroundTasks = &items
	if !store.Apply(event) {
		t.Fatal("inventory rejected")
	}
	*now = now.Add(time.Second)
	event = activityFixtureEvent(run, *now, ActivitySubagentStart, "child")
	store.Apply(event)
	got, _ := store.Snapshot(run.Session.ID)
	if len(got.BackgroundTasks) != 1 || got.BackgroundTasks[0].State != ActivityWorking {
		t.Fatal("absent inventory erased prior state")
	}
	empty := []ActivityBackgroundTask{}
	event.Kind, event.NodeID, event.BackgroundTasks = ActivityBackgroundInventory, "", &empty
	store.Apply(event)
	got, _ = store.Snapshot(run.Session.ID)
	if len(got.BackgroundTasks) != 1 || got.BackgroundTasks[0].State != ActivityUnknown || got.BackgroundTasks[0].EndedAt != nil || !got.Capabilities.BackgroundTasks {
		t.Fatalf("missing item was concluded: %+v", got)
	}
}

func TestActivityStoreRejectsCycleAndForeignRun(t *testing.T) {
	store, run, now := activityFixture()
	a := activityFixtureEvent(run, *now, ActivitySubagentStart, "a")
	store.Apply(a)
	b := activityFixtureEvent(run, *now, ActivitySubagentStart, "b")
	b.ParentProviderID = "a"
	store.Apply(b)
	before, _ := store.Snapshot(run.Session.ID)
	a.ParentProviderID = "b"
	if store.Apply(a) {
		t.Fatal("cycle accepted")
	}
	for _, mutate := range []func(*ActivityEvent){
		func(e *ActivityEvent) { e.RunID = "old-run" }, func(e *ActivityEvent) { e.OwnerID = "foreign-owner" }, func(e *ActivityEvent) { e.TerminalID = "old-terminal" }, func(e *ActivityEvent) { e.ProviderSessionID = "foreign-root" }, func(e *ActivityEvent) { e.Provider = AgentCodex }, func(e *ActivityEvent) { e.NodeID = "" }, func(e *ActivityEvent) { e.ParentProviderID = "unproven-parent" }, func(e *ActivityEvent) { e.ReceivedAt = e.ReceivedAt.Add(-time.Hour) },
	} {
		e := b
		mutate(&e)
		if store.Apply(e) {
			t.Fatalf("invalid event accepted: %+v", e)
		}
	}
	after, _ := store.Snapshot(run.Session.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid event mutated snapshot")
	}
}

func TestActivityStoreDuplicateCopyAndOptionalJSON(t *testing.T) {
	store, run, now := activityFixture()
	event := activityFixtureEvent(run, *now, ActivitySubagentStart, "child")
	event.Model = &ActivityModel{Name: "observed-model", Kind: ActivityModelObserved}
	store.Apply(event)
	before, _ := store.Snapshot(run.Session.ID)
	if store.Apply(event) {
		t.Fatal("identical event advanced revision")
	}
	before.Nodes[1].Model.Name = "mutated"
	*before.Nodes[1].StartedAt = now.Add(time.Hour)
	before.Nodes[0].Label = "mutated"
	event.Model.Name = "mutated input"
	after, _ := store.Snapshot(run.Session.ID)
	if after.Nodes[1].Model.Name != "observed-model" || !after.Nodes[1].StartedAt.Equal(*now) || after.Nodes[0].Label != "Root" {
		t.Fatal("snapshot or input aliases store")
	}
	data, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"backgroundTasks":[]`) || strings.Contains(string(data), `"tokens":{`) || strings.Contains(string(data), `"endedAt"`) {
		t.Fatal(string(data))
	}
}

func TestActivityStoreGenerationRootUpdatesAndReset(t *testing.T) {
	store, run, now := activityFixture()
	store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
	before, _ := store.Snapshot(run.Session.ID)
	run.RunID, run.Session.TerminalID = "new-run", "new-terminal"
	run.StartedAt = now.Add(time.Hour)
	store.Begin(run)
	after, _ := store.Snapshot(run.Session.ID)
	if after.Revision <= before.Revision || after.RootID != before.RootID || len(after.Nodes) != 1 || after.SourceID != run.OwnerID {
		t.Fatalf("new generation: %+v", after)
	}
	run.Session.Title, run.Session.State = "Renamed", SessionNeedsInput
	store.RootChanged(run.Session, "old-run")
	old, _ := store.Snapshot(run.Session.ID)
	if old.Nodes[0].Label != "Root" {
		t.Fatal("old generation changed root")
	}
	store.RootChanged(run.Session, run.RunID)
	after, _ = store.Snapshot(run.Session.ID)
	if after.Nodes[0].Label != "Renamed" || after.Nodes[0].State != ActivityNeedsInput || after.Nodes[0].EndedAt != nil {
		t.Fatal(after.Nodes[0])
	}
	store.Reset()
	if _, ok := store.Snapshot(run.Session.ID); ok {
		t.Fatal("reset left snapshot")
	}
}

func TestActivityStoreMetadataNeverInventsLiveState(t *testing.T) {
	store, run, now := activityFixture()
	result := ActivityMetadataResult{Nodes: []ActivityMetadataNode{{ProviderNodeID: "child", ParentProviderID: "provider-root", Label: "Safe\x00 label", Model: &ActivityModel{Name: "configured", Kind: ActivityModelConfigured}}}, Status: ActivityAvailable, MessageCode: ActivityMessageNone, Capabilities: ActivityCapabilities{Children: true, LiveState: true, Duration: true, Tokens: true}, ObservedAt: *now}
	if !store.MergeMetadata(run.Session.ID, run.RunID, result) {
		t.Fatal("metadata rejected")
	}
	got, _ := store.Snapshot(run.Session.ID)
	if got.Nodes[1].State != ActivityUnknown || got.Nodes[1].StartedAt != nil || got.Nodes[1].EndedAt != nil || got.Capabilities.LiveState || got.Capabilities.Duration || got.Capabilities.Tokens || got.Nodes[1].Label != "Safe label" {
		t.Fatalf("metadata inferred live data: %+v", got)
	}
	event := activityFixtureEvent(run, *now, ActivitySubagentStart, "child")
	event.Model = &ActivityModel{Name: "actual", Kind: ActivityModelObserved}
	store.Apply(event)
	*now = now.Add(time.Second)
	result.ObservedAt = *now
	store.MergeMetadata(run.Session.ID, run.RunID, result)
	got, _ = store.Snapshot(run.Session.ID)
	if got.Nodes[1].State != ActivityWorking || got.Nodes[1].Model.Name != "actual" || got.Nodes[1].Model.Kind != ActivityModelObserved {
		t.Fatal("metadata overwrote observed state/model")
	}
	if store.MergeMetadata(run.Session.ID, "old-run", result) {
		t.Fatal("old metadata generation accepted")
	}
	before, _ := store.Snapshot(run.Session.ID)
	result.Nodes = []ActivityMetadataNode{{ProviderNodeID: "x", ParentProviderID: "y"}, {ProviderNodeID: "y", ParentProviderID: "x"}}
	if store.MergeMetadata(run.Session.ID, run.RunID, result) {
		t.Fatal("metadata cycle accepted")
	}
	after, _ := store.Snapshot(run.Session.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid metadata mutated store")
	}
}

func TestActivityStoreSanitizesAndRequiresObservedStart(t *testing.T) {
	store, run, now := activityFixture()
	event := activityFixtureEvent(run, *now, ActivitySubagentTurnStop, "unknown-child")
	if store.Apply(event) {
		t.Fatal("stop without observed start accepted")
	}
	event.Kind, event.AgentType = ActivitySubagentStart, strings.Repeat("界", 245)+"\n"
	store.Apply(event)
	got, _ := store.Snapshot(run.Session.ID)
	if len([]rune(got.Nodes[1].Label)) != 240 || strings.ContainsAny(got.Nodes[1].Label, "\n\x00") {
		t.Fatal("unsafe label")
	}
}

func TestActivityStoreUnsupportedProviderStaysRootOnly(t *testing.T) {
	store, run, now := activityFixture()
	run.Session.AgentID, run.Session.ID, run.RunID = AgentKimi, "kimi", "kimi-run"
	store.Begin(run)
	before, _ := store.Snapshot("kimi")
	if store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "child")) {
		t.Fatal("unsupported provider accepted child event")
	}
	if store.MergeMetadata("kimi", run.RunID, ActivityMetadataResult{Nodes: []ActivityMetadataNode{{ProviderNodeID: "child"}}, Status: ActivityAvailable}) {
		t.Fatal("unsupported provider accepted descendants")
	}
	after, _ := store.Snapshot("kimi")
	if !reflect.DeepEqual(before, after) || after.MessageCode != ActivityMessageUnsupportedProvider {
		t.Fatal("unsupported provider lost coverage warning")
	}
}

func TestActivityStoreBindProviderAndRefreshFailure(t *testing.T) {
	store, run, now := activityFixture()
	run.Session.ID, run.RunID, run.Session.ProviderSessionID = "unbound", "unbound-run", ""
	store.Begin(run)
	store.BindProvider(run.Session.ID, "old-run", "wrong")
	before, _ := store.Snapshot(run.Session.ID)
	if before.Nodes[0].ProviderNodeID != "" || before.MessageCode != ActivityMessageNoProviderSession {
		t.Fatal(before)
	}
	store.BindProvider(run.Session.ID, run.RunID, "bound")
	run.Session.ProviderSessionID = "bound"
	store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "child"))
	*now = now.Add(time.Second)
	store.MergeMetadata(run.Session.ID, run.RunID, ActivityMetadataResult{Status: ActivityStatusError, MessageCode: ActivityMessageRefreshFailed, Stale: true, ObservedAt: *now})
	got, _ := store.Snapshot(run.Session.ID)
	if len(got.Nodes) != 2 || got.Nodes[1].State != ActivityWorking || got.Status != ActivityStatusError || !got.Stale || !got.Capabilities.LiveState {
		t.Fatal("failed refresh discarded last successful data")
	}
	store.BindProvider(run.Session.ID, run.RunID, "different-root")
	got, _ = store.Snapshot(run.Session.ID)
	if got.Nodes[0].ProviderNodeID != "bound" {
		t.Fatal("rebound generation to different provider root")
	}
}

func TestActivityStoreRetentionForgetsEvictedObservedStart(t *testing.T) {
	store, run, now := activityFixture()
	for i := 0; i < 501; i++ {
		*now = now.Add(time.Millisecond)
		store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, fmt.Sprintf("child-%03d", i)))
	}
	*now = now.Add(time.Second)
	store.MergeMetadata(run.Session.ID, run.RunID, ActivityMetadataResult{Nodes: []ActivityMetadataNode{{ProviderNodeID: "child-000", ParentProviderID: "provider-root"}}, ObservedAt: *now})
	if store.Apply(activityFixtureEvent(run, *now, ActivitySubagentTurnStop, "child-000")) {
		t.Fatal("evicted observed start was retained and reused for metadata-only identity")
	}
}

func TestActivityStoreDelayedInventoryCannotEraseNewerTaskState(t *testing.T) {
	store, run, now := activityFixture()
	*now = now.Add(time.Second)
	items := []ActivityBackgroundTask{{ProviderNodeID: "task", State: ActivityWorking}}
	event := activityFixtureEvent(run, *now, ActivityBackgroundInventory, "")
	event.BackgroundTasks = &items
	store.Apply(event)
	empty := []ActivityBackgroundTask{}
	event.BackgroundTasks, event.ReceivedAt = &empty, now.Add(-time.Second)
	store.Apply(event)
	got, _ := store.Snapshot(run.Session.ID)
	if got.BackgroundTasks[0].State != ActivityWorking {
		t.Fatal("old inventory erased current task")
	}
}

func TestActivityStoreStopPreservesProvenParentAndInterval(t *testing.T) {
	store, run, now := activityFixture()
	store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "parent"))
	event := activityFixtureEvent(run, *now, ActivitySubagentStart, "child")
	event.ParentProviderID = "parent"
	store.Apply(event)
	before, _ := store.Snapshot(run.Session.ID)
	var parentID string
	for _, node := range before.Nodes {
		if node.ProviderNodeID == "child" {
			parentID = node.ParentID
		}
	}
	*now = now.Add(time.Second)
	event.Kind, event.ParentProviderID, event.ReceivedAt = ActivitySubagentTurnStop, "", *now
	store.Apply(event)
	store.End(run.Session.ID, run.RunID, now.Add(time.Hour))
	got, _ := store.Snapshot(run.Session.ID)
	for _, node := range got.Nodes {
		if node.ProviderNodeID == "child" && (node.ParentID != parentID || node.State != ActivityReplyFinished || node.EndedAt == nil || !node.EndedAt.Equal(*now)) {
			t.Fatal("ending observation lost proven parent/interval", node)
		}
	}
}

func TestActivityStoreRetentionGroupsEvictedParentsAndAllowsKnownChildUpdates(t *testing.T) {
	for _, finished := range []bool{false, true} {
		t.Run(fmt.Sprintf("finished-parent-%v", finished), func(t *testing.T) {
			store, run, now := activityFixture()
			store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, "parent"))
			*now = now.Add(time.Millisecond)
			child := activityFixtureEvent(run, *now, ActivitySubagentStart, "child")
			child.ParentProviderID = "parent"
			store.Apply(child)
			if finished {
				store.Apply(activityFixtureEvent(run, *now, ActivitySubagentTurnStop, "parent"))
			}
			for i := 0; i < 498; i++ {
				*now = now.Add(time.Millisecond)
				store.Apply(activityFixtureEvent(run, *now, ActivitySubagentStart, fmt.Sprintf("other-%03d", i)))
			}
			got, _ := store.Snapshot(run.Session.ID)
			ids := make(map[string]bool)
			for _, node := range got.Nodes {
				ids[node.ID] = true
			}
			for _, node := range got.Nodes {
				if node.ParentID != "" && !ids[node.ParentID] {
					t.Fatalf("dangling parent: %+v", node)
				}
				if node.ProviderNodeID == "child" && node.ParentID != "" {
					t.Fatalf("missing immediate parent claimed as retained: %+v", node)
				}
				if node.ProviderNodeID == "parent" {
					t.Fatal("fixture did not evict parent")
				}
			}
			*now = now.Add(time.Second)
			child.Kind, child.ReceivedAt = ActivitySubagentNeedsInput, *now
			if !store.Apply(child) {
				t.Fatal("known child update rejected after parent eviction")
			}
			// A different absent parent has no proof and must still be rejected.
			child.ParentProviderID = "foreign-parent"
			if store.Apply(child) {
				t.Fatal("unproven absent parent accepted")
			}
			got, _ = store.Snapshot(run.Session.ID)
			for _, node := range got.Nodes {
				if node.ProviderNodeID == "child" && (node.ParentID != "" || node.State != ActivityNeedsInput) {
					t.Fatal(node)
				}
			}
			// Discovery can restore the immediate parent without forging a root edge.
			*now = now.Add(time.Second)
			if !store.MergeMetadata(run.Session.ID, run.RunID, ActivityMetadataResult{Nodes: []ActivityMetadataNode{{ProviderNodeID: "parent", ParentProviderID: "provider-root"}, {ProviderNodeID: "child", ParentProviderID: "parent"}}, ObservedAt: *now}) {
				t.Fatal("proven metadata could not restore ancestor")
			}
			got, _ = store.Snapshot(run.Session.ID)
			ids = make(map[string]bool)
			for _, node := range got.Nodes {
				ids[node.ID] = true
			}
			for _, node := range got.Nodes {
				if node.ParentID != "" && !ids[node.ParentID] {
					t.Fatal("metadata left dangling parent")
				}
			}
		})
	}
}

func TestActivityStoreInventoryWatermarkRejectsDelayedPresentPhotographs(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(fmt.Sprintf("metadata-%v", metadata), func(t *testing.T) {
			store, run, now := activityFixture()
			apply := func(at time.Time, items []ActivityBackgroundTask) {
				if metadata {
					store.MergeMetadata(run.Session.ID, run.RunID, ActivityMetadataResult{BackgroundTasks: &items, ObservedAt: at})
					return
				}
				event := activityFixtureEvent(run, at, ActivityBackgroundInventory, "")
				event.BackgroundTasks = &items
				store.Apply(event)
			}
			apply(*now, []ActivityBackgroundTask{{ProviderNodeID: "known", State: ActivityWorking}})
			latest := now.Add(2 * time.Second)
			apply(latest, []ActivityBackgroundTask{})
			got, _ := store.Snapshot(run.Session.ID)
			if got.BackgroundTasks[0].State != ActivityUnknown || !got.BackgroundTasks[0].LastObservedAt.Equal(latest) {
				t.Fatal("absence did not advance task observation", got.BackgroundTasks[0])
			}
			apply(now.Add(time.Second), []ActivityBackgroundTask{{ProviderNodeID: "known", State: ActivityWorking}, {ProviderNodeID: "unseen", State: ActivityWorking}})
			got, _ = store.Snapshot(run.Session.ID)
			if len(got.BackgroundTasks) != 1 || got.BackgroundTasks[0].State != ActivityUnknown || !got.BackgroundTasks[0].LastObservedAt.Equal(latest) {
				t.Fatal("delayed inventory revived task after empty photograph", got.BackgroundTasks)
			}
			// The watermark also exists when the store has never retained a task.
			other, otherRun, otherNow := activityFixture()
			empty := []ActivityBackgroundTask{}
			other.MergeMetadata(otherRun.Session.ID, otherRun.RunID, ActivityMetadataResult{BackgroundTasks: &empty, ObservedAt: otherNow.Add(2 * time.Second)})
			lateItems := []ActivityBackgroundTask{{ProviderNodeID: "unseen", State: ActivityWorking}}
			event := activityFixtureEvent(otherRun, otherNow.Add(time.Second), ActivityBackgroundInventory, "")
			event.BackgroundTasks = &lateItems
			other.Apply(event)
			got, _ = other.Snapshot(otherRun.Session.ID)
			if len(got.BackgroundTasks) != 0 {
				t.Fatal("delayed inventory inserted previously unseen task")
			}
		})
	}
}

func TestActivityStoreInventoryIntervalsRemainAssociatedWithObservedInterval(t *testing.T) {
	for _, state := range []ActivityState{ActivityReplyFinished, ActivityWorking} {
		t.Run(string(state), func(t *testing.T) {
			store, run, now := activityFixture()
			start, end := *now, now.Add(time.Second)
			items := []ActivityBackgroundTask{{ProviderNodeID: "task", State: ActivityReplyFinished, StartedAt: &start, EndedAt: &end}}
			event := activityFixtureEvent(run, end, ActivityBackgroundInventory, "")
			event.BackgroundTasks = &items
			store.Apply(event)
			newStart := now.Add(2 * time.Second)
			items[0] = ActivityBackgroundTask{ProviderNodeID: "task", State: state}
			if state == ActivityReplyFinished {
				items[0].StartedAt = &newStart
			}
			event.ReceivedAt = now.Add(3 * time.Second)
			store.Apply(event)
			got, _ := store.Snapshot(run.Session.ID)
			task := got.BackgroundTasks[0]
			if task.EndedAt != nil {
				t.Fatal("new interval reused old end", task)
			}
			if state == ActivityWorking && task.StartedAt != nil {
				t.Fatal("fresh working state invented start from old finished interval", task)
			}
			if state == ActivityReplyFinished && (task.StartedAt == nil || !task.StartedAt.Equal(newStart)) {
				t.Fatal("new explicit start lost", task)
			}
		})
	}
}
