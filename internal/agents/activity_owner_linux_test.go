//go:build linux

package agents

import (
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/google/uuid"
	"github.com/shirou/gopsutil/v3/process"
	"golang.org/x/sys/unix"
)

// WNOWAIT deliberately leaves an exited child unreaped. A creation-time-only
// owner check wrongly treats that zombie as alive until cmd.Wait reaps it.
func TestActivityOwnerExitedUnreapedLinux(t *testing.T) {
	if os.Getenv("LARA_ACTIVITY_ZOMBIE_CHILD") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestActivityOwnerExitedUnreapedLinux$")
	child.Env = append(os.Environ(), "LARA_ACTIVITY_ZOMBIE_CHILD=1")
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); _ = child.Process.Kill(); _ = child.Wait() })
	created, err := activityProcessCreation(child.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewActivityOwnerRegistry(t.TempDir(), nil)
	descriptor := ActivityOwnerDescriptor{Identity: ActivityOwnerIdentity{ID: uuid.NewString(), PID: child.Process.Pid, CreatedAt: created}, Endpoint: "http://127.0.0.1:12345", Token: "synthetic-child-token"}
	if err := registry.Register(descriptor); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetTerminal(descriptor.Identity.ID, "terminal", true); err != nil {
		t.Fatal(err)
	}
	binding := ownerBinding(descriptor, "session")
	if err := registry.Bind(binding); err != nil {
		t.Fatal(err)
	}
	if err := registry.SetProviderAliases(binding.SessionID, binding.RunID, []string{"known-child"}); err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Waitid(unix.P_PID, child.Process.Pid, &unix.Siginfo{}, unix.WEXITED|unix.WNOWAIT, nil); err != nil {
		t.Fatal(err)
	}
	zombie, err := process.NewProcess(int32(child.Process.Pid))
	if err != nil {
		t.Fatal("fixture was reaped before assertion", err)
	}
	status, err := zombie.Status()
	if err != nil || len(status) != 1 || status[0] != process.Zombie {
		t.Fatalf("fixture not zombie: %v %v", status, err)
	}
	if activityProcessAlive(descriptor.Identity) {
		t.Fatal("unreaped exited owner considered alive")
	}
	if _, ok, err := registry.ResolveOwner(descriptor.Identity.ID); err != nil || ok {
		t.Fatalf("zombie owner resolved: %v %v", ok, err)
	}
	if ok, err := registry.ResolveTerminal(descriptor.Identity.ID, "terminal"); err != nil || ok {
		t.Fatalf("zombie terminal authorized: %v %v", ok, err)
	}
	for _, id := range []string{binding.ProviderSessionID, "known-child"} {
		if _, ok, err := registry.ResolveProvider(binding.Provider, id); err != nil || ok {
			t.Fatalf("zombie provider authorized: %v %v", ok, err)
		}
	}
	replacement := NewActivityOwnerRegistry(registry.root, nil)
	current, err := activityProcessCreation(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	descriptor.Identity = ActivityOwnerIdentity{ID: uuid.NewString(), PID: os.Getpid(), CreatedAt: current}
	if err := replacement.Register(descriptor); err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := replacement.SetTerminal(descriptor.Identity.ID, "terminal", true); err != nil {
		t.Fatal(err)
	}
	binding.OwnerID = descriptor.Identity.ID
	binding.RunID = uuid.NewString()
	if err := replacement.Bind(binding); err != nil {
		t.Fatalf("zombie blocked replacement owner: %v", err)
	}
}
