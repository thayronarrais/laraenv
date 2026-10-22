package ssh

import (
	"path/filepath"
	"testing"

	"lara4ever/internal/config"
)

// Resolving a jump via the mutable settings after Snapshot would change the active SSH identity.
func TestManagerSnapshotFreezesTransitiveJumpHostsAndReturnsIndependentClones(t *testing.T) {
	root := t.TempDir()
	manager := NewManager(root, filepath.Join(root, "known_hosts"), func(name string) string { return filepath.Join(root, name) })
	hosts := map[string]config.SSHHost{
		"first":  {Name: "first", Address: "192.0.2.11", User: "first-user", Password: "first-secret", Tags: []string{"original"}, ProxyJump: "second"},
		"second": {Name: "second", Address: "192.0.2.12", User: "second-user", Password: "second-secret", Tags: []string{"original"}},
	}
	manager.SetHostLookup(func(name string) (config.SSHHost, bool) { h, ok := hosts[name]; return h, ok })
	target := config.SSHHost{Name: "target", Address: "192.0.2.10", User: "target-user", ProxyJump: " first ", Tags: []string{"original"}}
	frozen, snapshots, e := manager.Snapshot(target)
	if e != nil {
		t.Fatal(e)
	}
	if len(snapshots) != 3 || snapshots[0].Name != "target" || snapshots[1].Name != "first" || snapshots[2].Name != "second" {
		t.Fatalf("target-first chain: %+v", snapshots)
	}
	target.Tags[0] = "changed"
	h := hosts["first"]
	h.Address = "192.0.2.99"
	h.Password = "new-secret"
	h.Tags[0] = "changed"
	hosts["first"] = h
	delete(hosts, "second")
	snapshots[0].Tags[0] = "caller changed"
	snapshots[1].Password = "caller changed"
	snapshots[1].Tags[0] = "caller changed"
	for _, want := range []struct{ name, address, user, password string }{{"target", "192.0.2.10", "target-user", ""}, {"first", "192.0.2.11", "first-user", "first-secret"}, {"second", "192.0.2.12", "second-user", "second-secret"}} {
		actual, ok := frozen.hostLookup(want.name)
		if !ok || actual.Address != want.address || actual.User != want.user || actual.Password != want.password || actual.Tags[0] != "original" {
			t.Fatalf("mutable snapshot %s: %+v %v", want.name, actual, ok)
		}
		actual.Tags[0] = "lookup consumer changed"
		again, _ := frozen.hostLookup(want.name)
		if again.Tags[0] != "original" {
			t.Fatal("lookup returned shared tags")
		}
	}
	hosts["late"] = config.SSHHost{Name: "late"}
	if _, ok := frozen.hostLookup("late"); ok {
		t.Fatal("snapshot fell back to live resolver")
	}
	if frozen.HostDir("target") != filepath.Join(root, "target") {
		t.Fatal("snapshot lost key directory mapping")
	}
}

// ProxyCommand wins over ProxyJump for both the target and intermediate jumps.
func TestManagerSnapshotHonorsProxyCommandPrecedence(t *testing.T) {
	manager := NewManager(t.TempDir(), "known_hosts", func(name string) string { return name })
	target := config.SSHHost{Name: "target", ProxyCommand: "fixture-proxy %h %p", ProxyJump: "missing"}
	frozen, snapshots, e := manager.Snapshot(target)
	if e != nil || len(snapshots) != 1 || snapshots[0].ProxyCommand != "fixture-proxy %h %p" {
		t.Fatalf("target proxy command: %+v %v", snapshots, e)
	}
	if _, ok := frozen.hostLookup("missing"); ok {
		t.Fatal("resolved ignored ProxyJump")
	}
	manager.SetHostLookup(func(name string) (config.SSHHost, bool) {
		return config.SSHHost{Name: "first", ProxyCommand: "fixture-proxy %h %p", ProxyJump: "missing"}, name == "first"
	})
	target.ProxyCommand = ""
	target.ProxyJump = "first"
	_, snapshots, e = manager.Snapshot(target)
	if e != nil || len(snapshots) != 2 {
		t.Fatalf("jump proxy command: %+v %v", snapshots, e)
	}
}

// Missing jumps, cycles, and unavailable target/transit credentials must fail before any durable run or dial.
func TestManagerSnapshotRejectsUnresolvableOrUnsafeChains(t *testing.T) {
	tests := []struct {
		name   string
		target config.SSHHost
		hosts  map[string]config.SSHHost
		lookup bool
	}{
		{name: "no resolver", target: config.SSHHost{Name: "target", ProxyJump: "first"}},
		{name: "missing jump", target: config.SSHHost{Name: "target", ProxyJump: "first"}, lookup: true},
		{name: "target cycle", target: config.SSHHost{Name: "target", ProxyJump: "target"}, lookup: true},
		{name: "transitive cycle", target: config.SSHHost{Name: "target", ProxyJump: "first"}, lookup: true, hosts: map[string]config.SSHHost{"first": {Name: "first", ProxyJump: "second"}, "second": {Name: "second", ProxyJump: "first"}}},
		{name: "target password unavailable", target: config.SSHHost{Name: "target", PasswordUnavailable: true}},
		{name: "jump password unavailable", target: config.SSHHost{Name: "target", ProxyJump: "first"}, lookup: true, hosts: map[string]config.SSHHost{"first": {Name: "first", PasswordUnavailable: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewManager(t.TempDir(), "known_hosts", func(name string) string { return name })
			if tc.lookup {
				manager.SetHostLookup(func(name string) (config.SSHHost, bool) { h, ok := tc.hosts[name]; return h, ok })
			}
			frozen, snapshots, e := manager.Snapshot(tc.target)
			if e == nil || frozen != nil || snapshots != nil {
				t.Fatalf("unsafe snapshot accepted: %+v %+v %v", frozen, snapshots, e)
			}
		})
	}
}

func TestManagerSnapshotPreservesKeyAuthWhenUnusedPasswordIsLocked(t *testing.T) {
	for _, mode := range []string{"key", "both"} {
		t.Run(mode, func(t *testing.T) {
			manager := NewManager(t.TempDir(), "known_hosts", func(name string) string { return name })
			manager.SetHostLookup(func(name string) (config.SSHHost, bool) {
				return config.SSHHost{Name: "jump", AuthMode: mode, KeyName: "fixture-key", PasswordUnavailable: true}, name == "jump"
			})
			target := config.SSHHost{Name: "target", AuthMode: mode, KeyName: "fixture-key", PasswordUnavailable: true, ProxyJump: "jump"}
			frozen, hosts, err := manager.Snapshot(target)
			if err != nil || frozen == nil || len(hosts) != 2 {
				t.Fatalf("unused protected password blocked configured key auth: %#v %v", hosts, err)
			}
		})
	}
}
