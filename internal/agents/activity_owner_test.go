package agents

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestActivityOwnerProviderAliasesPreserveRootAndReplaceAtomically(t *testing.T) {
	root := t.TempDir()
	owner := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	reader := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	descriptor := ownerFixture(t, owner)
	binding := ownerBinding(descriptor, "canonical")
	if err := owner.Bind(binding); err != nil {
		t.Fatal(err)
	}
	if err := owner.SetProviderAliases(binding.SessionID, binding.RunID, []string{"child", "grandchild", binding.ProviderSessionID, "child"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{binding.ProviderSessionID, "child", "grandchild"} {
		got, ok, err := reader.ResolveProvider(binding.Provider, id)
		if err != nil || !ok || got != binding {
			t.Fatalf("provider %s lost canonical binding: %+v %v %v", id, got, ok, err)
		}
	}
	changed := binding
	changed.ProviderSessionID = "replacement-root"
	if err := owner.Bind(changed); !errors.Is(err, ErrActivityOwnerConflict) {
		t.Fatalf("canonical root replaced: %v", err)
	}
	if err := owner.SetProviderAliases(binding.SessionID, binding.RunID, []string{"new-child"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := reader.ResolveProvider(binding.Provider, "child"); ok {
		t.Fatal("obsolete alias retained")
	}
	if got, ok, _ := reader.ResolveProvider(binding.Provider, binding.ProviderSessionID); !ok || got != binding {
		t.Fatal("canonical root lost on alias replacement")
	}
	if err := owner.Release(binding.SessionID, binding.RunID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{binding.ProviderSessionID, "new-child"} {
		if _, ok, _ := reader.ResolveProvider(binding.Provider, id); ok {
			t.Fatal("released ID still authorizes hook")
		}
	}
}

func TestActivityOwnerProviderAliasConflictsAndFences(t *testing.T) {
	root := t.TempDir()
	a := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	b := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	da, db := ownerFixture(t, a), ownerFixture(t, b)
	ba, bb := ownerBinding(da, "a"), ownerBinding(db, "b")
	if err := a.Bind(ba); err != nil {
		t.Fatal(err)
	}
	if err := b.Bind(bb); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProviderAliases(ba.SessionID, ba.RunID, []string{"child-a"}); err != nil {
		t.Fatal(err)
	}
	if err := b.SetProviderAliases(bb.SessionID, bb.RunID, []string{"child-b"}); err != nil {
		t.Fatal(err)
	}
	for _, conflict := range []string{bb.ProviderSessionID, "child-b"} {
		if err := a.SetProviderAliases(ba.SessionID, ba.RunID, []string{"must-not-be-written", conflict}); !errors.Is(err, ErrActivityOwnerConflict) {
			t.Fatalf("alias conflict accepted: %v", err)
		}
		if _, ok, _ := b.ResolveProvider(ba.Provider, "must-not-be-written"); ok {
			t.Fatal("partial alias inventory written")
		}
		if got, ok, _ := b.ResolveProvider(ba.Provider, "child-a"); !ok || got != ba {
			t.Fatal("previous alias inventory lost")
		}
	}
	conflictingRoot := ownerBinding(db, "new-b")
	conflictingRoot.ProviderSessionID = "child-a"
	if err := b.Bind(conflictingRoot); !errors.Is(err, ErrActivityOwnerConflict) {
		t.Fatalf("root claimed another binding's alias: %v", err)
	}
	if err := a.SetProviderAliases(ba.SessionID, "old-run", []string{"stale"}); err == nil {
		t.Fatal("stale run wrote aliases")
	}
	if err := b.SetProviderAliases(ba.SessionID, ba.RunID, []string{"foreign"}); err == nil {
		t.Fatal("foreign registry wrote aliases")
	}
	excessive := make([]string, 501)
	for i := range excessive {
		excessive[i] = fmt.Sprintf("child-%d", i)
	}
	if err := a.SetProviderAliases(ba.SessionID, ba.RunID, excessive); err == nil {
		t.Fatal("unbounded alias inventory accepted")
	}
	if err := a.SetProviderAliases(ba.SessionID, ba.RunID, []string{""}); err == nil {
		t.Fatal("empty alias accepted")
	}
	if err := a.SetTerminal(ba.OwnerID, ba.TerminalID, false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProviderAliases(ba.SessionID, ba.RunID, []string{"after-close"}); err == nil {
		t.Fatal("closed terminal wrote aliases")
	}
	if _, ok, _ := b.ResolveProvider(ba.Provider, "child-a"); ok {
		t.Fatal("closed terminal alias authorized hook")
	}
	if err := a.SetTerminal(ba.OwnerID, ba.TerminalID, true); err != nil {
		t.Fatal(err)
	}
	resumed := ba
	resumed.RunID = uuid.NewString()
	if err := a.Bind(resumed); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.ResolveProvider(ba.Provider, "child-a"); ok {
		t.Fatal("old aliases survived resumed run")
	}
	if err := a.SetProviderAliases(ba.SessionID, ba.RunID, []string{"stale-after-resume"}); err == nil {
		t.Fatal("old run modified resumed aliases")
	}
	if err := a.SetProviderAliases(resumed.SessionID, resumed.RunID, []string{"resumed-child"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.ResolveProvider(ba.Provider, "resumed-child"); ok {
		t.Fatal("closed owner alias authorized hook")
	}
}

func TestActivityOwnerProviderAliasesLimitAndEmptyInventory(t *testing.T) {
	registry := NewActivityOwnerRegistry(t.TempDir(), func(ActivityOwnerIdentity) bool { return true })
	descriptor := ownerFixture(t, registry)
	binding := ownerBinding(descriptor, "session")
	if err := registry.Bind(binding); err != nil {
		t.Fatal(err)
	}
	aliases := make([]string, 500)
	for i := range aliases {
		aliases[i] = fmt.Sprintf("descendant-%d", i)
	}
	if err := registry.SetProviderAliases(binding.SessionID, binding.RunID, aliases); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := registry.ResolveProvider(binding.Provider, "descendant-499"); err != nil || !ok || got != binding {
		t.Fatalf("last bounded alias missing: %v %v", ok, err)
	}
	if err := registry.SetProviderAliases(binding.SessionID, binding.RunID, []string{strings.Repeat("x", 2<<20)}); err == nil {
		t.Fatal("oversized registry replacement accepted")
	}
	if _, ok, err := registry.ResolveProvider(binding.Provider, "descendant-499"); err != nil || !ok {
		t.Fatalf("failed write damaged prior registry: %v %v", ok, err)
	}
	if err := registry.SetProviderAliases(binding.SessionID, binding.RunID, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := registry.ResolveProvider(binding.Provider, "descendant-499"); err != nil || ok {
		t.Fatalf("empty inventory left alias: %v %v", ok, err)
	}
	if got, ok, err := registry.ResolveProvider(binding.Provider, binding.ProviderSessionID); err != nil || !ok || got != binding {
		t.Fatalf("empty inventory removed canonical root: %v %v", ok, err)
	}
}

func ownerFixture(t *testing.T, r *ActivityOwnerRegistry) ActivityOwnerDescriptor {
	t.Helper()
	d := ActivityOwnerDescriptor{Identity: ActivityOwnerIdentity{ID: uuid.NewString(), PID: os.Getpid(), CreatedAt: time.Now().UTC()}, Endpoint: "http://127.0.0.1:12345", Token: "test-owner-secret"}
	if err := r.Register(d); err != nil {
		t.Fatal(err)
	}
	if err := r.SetTerminal(d.Identity.ID, "terminal", true); err != nil {
		t.Fatal(err)
	}
	return d
}

func ownerBinding(d ActivityOwnerDescriptor, session string) ActivityBinding {
	return ActivityBinding{OwnerID: d.Identity.ID, SessionID: session, RunID: uuid.NewString(), TerminalID: "terminal", Provider: AgentClaude, ProviderSessionID: "provider-" + session}
}

func TestActivityOwnerPIDReuseRejected(t *testing.T) {
	alive := true
	r := NewActivityOwnerRegistry(t.TempDir(), func(ActivityOwnerIdentity) bool { return alive })
	d := ownerFixture(t, r)
	b := ownerBinding(d, "session")
	if err := r.Bind(b); err != nil {
		t.Fatal(err)
	}
	alive = false // Same PID now belongs to another creation time.
	if _, ok, err := r.ResolveProvider(AgentClaude, b.ProviderSessionID); err != nil || ok {
		t.Fatalf("reused PID authorized hook: %v %v", ok, err)
	}
	if ok, err := r.ResolveTerminal(d.Identity.ID, "terminal"); err != nil || ok {
		t.Fatalf("reused PID authorized adoption: %v %v", ok, err)
	}
}

func TestActivityOwnerProcessCreationIdentity(t *testing.T) {
	created, err := activityProcessCreation(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	identity := ActivityOwnerIdentity{ID: uuid.NewString(), PID: os.Getpid(), CreatedAt: created}
	if !activityProcessAlive(identity) {
		t.Fatal("current process rejected")
	}
	identity.CreatedAt = identity.CreatedAt.Add(-time.Second)
	if activityProcessAlive(identity) {
		t.Fatal("PID accepted with different process creation")
	}
}

func TestActivityOwnerRunClaimConflict(t *testing.T) {
	root := t.TempDir()
	a := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	b := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	da, db := ownerFixture(t, a), ownerFixture(t, b)
	ba := ownerBinding(da, "session")
	if err := a.Bind(ba); err != nil {
		t.Fatal(err)
	}
	bb := ba
	bb.OwnerID = db.Identity.ID
	if err := b.Bind(bb); err == nil {
		t.Fatal("competing live owner claimed run")
	}
	if err := a.Release(ba.SessionID, ba.RunID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := b.ResolveProvider(ba.Provider, ba.ProviderSessionID); ok {
		t.Fatal("released run authorizes hooks")
	}
	if got, ok, err := b.ResolveSession("session"); err != nil || !ok || got.Identity.ID != da.Identity.ID {
		t.Fatalf("ended snapshot lost: %+v %v %v", got, ok, err)
	}
	bb.RunID = uuid.NewString()
	if err := b.Bind(bb); err != nil {
		t.Fatal(err)
	}
	if err := a.Release(ba.SessionID, ba.RunID); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := a.ResolveSession("session"); !ok || got.Identity.ID != db.Identity.ID {
		t.Fatal("stale release removed new run")
	}
}

func TestActivityOwnerQueuesAreIsolated(t *testing.T) {
	root := t.TempDir()
	a := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	b := NewActivityOwnerRegistry(root, func(ActivityOwnerIdentity) bool { return true })
	da, db := ownerFixture(t, a), ownerFixture(t, b)
	for _, kind := range []string{"activity", "session"} {
		qa, err := a.QueueDir(da.Identity.ID, kind)
		if err != nil {
			t.Fatal(err)
		}
		qb, err := b.QueueDir(db.Identity.ID, kind)
		if err != nil {
			t.Fatal(err)
		}
		if qa == qb {
			t.Fatal("shared queue")
		}
		if err := os.WriteFile(filepath.Join(qb, "event.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	qb, _ := b.QueueDir(db.Identity.ID, "activity")
	files, err := os.ReadDir(qb)
	if err != nil {
		t.Fatal(err)
	}
	foreignQueueFiles := len(files)
	if foreignQueueFiles != 1 {
		t.Fatal("foreign queue consumed")
	}
	if _, err := b.QueueDir("../escape", "activity"); err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestActivityOwnerEndedRetention(t *testing.T) {
	r := NewActivityOwnerRegistry(t.TempDir(), func(ActivityOwnerIdentity) bool { return true })
	d := ownerFixture(t, r)
	for i := 0; i < 21; i++ {
		b := ownerBinding(d, fmt.Sprint(i))
		if err := r.Bind(b); err != nil {
			t.Fatal(err)
		}
		if err := r.Release(b.SessionID, b.RunID); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, _ := r.ResolveSession("0"); ok {
		t.Fatal("old ended snapshot retained")
	}
	if _, ok, _ := r.ResolveSession("20"); !ok {
		t.Fatal("latest ended snapshot missing")
	}
}

func TestActivityOwnerProviderRootCannotAliasAnotherSession(t *testing.T) {
	registry := NewActivityOwnerRegistry(t.TempDir(), func(ActivityOwnerIdentity) bool { return true })
	descriptor := ownerFixture(t, registry)
	binding := ownerBinding(descriptor, "original")
	if err := registry.Bind(binding); err != nil {
		t.Fatal(err)
	}
	binding.SessionID = "another-session"
	if err := registry.Bind(binding); err == nil {
		t.Fatal("provider root claimed by another session")
	}
}
