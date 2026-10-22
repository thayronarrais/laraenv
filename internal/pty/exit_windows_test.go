//go:build windows

package pty

import (
	"testing"
	"time"
)

// Compile-time guard. watchExit reaches the process handle through a type
// assertion, so a Wait signature that drifts would not fail to build — it would
// silently stop matching Waiter and every terminal would go back to outliving
// its shell.
var _ Waiter = (*winSession)(nil)

// The end-to-end shape of the bug: spawn a real shell that exits immediately
// and require the read loop — the one that feeds term:data and emits
// term:exit:<id> — to actually end. Before watchExit this blocked forever,
// because a pseudoconsole's output pipe stays open in conhost.exe once the
// shell it hosted is gone.
func TestRegistryEndsReadsWhenTheShellExits(t *testing.T) {
	r := NewRegistry()
	id, s, err := r.New("cmd.exe /c exit", 80, 24, nil, "")
	if err != nil {
		t.Fatalf("spawning a shell: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(id) })

	readEnded := make(chan error, 1)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := s.Read(buf); err != nil {
				readEnded <- err
				return
			}
		}
	}()

	select {
	case <-readEnded:
	case <-time.After(15 * time.Second):
		t.Fatal("the shell exited but reads never ended, so no term:exit is emitted and the pane stays open")
	}
}
