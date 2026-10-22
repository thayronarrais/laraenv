package pty

import (
	"io"
	"sync"
	"testing"
	"time"
)

// Typing `exit` in a terminal left the pane on screen, inert. On Windows a PTY
// is a pseudoconsole: conhost.exe owns the far end of the output pipe and keeps
// it open after the shell it hosted is gone, so the read loop that feeds
// term:data / term:exit simply never returns. The process handle is the only
// thing that knows, which is what Waiter exposes.

type fakeWaitSession struct {
	exit   chan struct{}
	closed chan struct{}
	once   sync.Once
}

func newFakeWaitSession() *fakeWaitSession {
	return &fakeWaitSession{exit: make(chan struct{}), closed: make(chan struct{})}
}

func (f *fakeWaitSession) Read(p []byte) (int, error)  { <-f.closed; return 0, io.EOF }
func (f *fakeWaitSession) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeWaitSession) Resize(cols, rows int) error { return nil }
func (f *fakeWaitSession) Wait() error                 { <-f.exit; return nil }
func (f *fakeWaitSession) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

type fakePlainSession struct{ fakeWaitSession }

// Shadows the embedded Wait so this type does not satisfy Waiter.
func (f *fakePlainSession) Wait(keepMeOffTheInterface int) error { return nil }

func TestWatchExitClosesTheSessionWhenTheProcessGoes(t *testing.T) {
	r := NewRegistry()
	s := newFakeWaitSession()
	id := r.Register(s)
	r.watchExit(id, s)

	select {
	case <-s.closed:
		t.Fatal("session closed while the process was still running")
	case <-time.After(30 * time.Millisecond):
	}

	close(s.exit)

	select {
	case <-s.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("process exited but the session was never closed, so reads never end")
	}

	// Closing must also unregister it: a later StopPty for the same id has
	// nothing left to close, and CloseAll at shutdown cannot double-close.
	if _, ok := r.Get(id); ok {
		t.Fatal("closed session is still registered")
	}
}

func TestWatchExitIgnoresSessionsThatCannotReportIt(t *testing.T) {
	// Unix PTYs and SSH sessions end their own reads; they never implement
	// Waiter, and watching them must not close anything.
	r := NewRegistry()
	s := &fakePlainSession{fakeWaitSession: *newFakeWaitSession()}
	id := r.Register(s)
	r.watchExit(id, s)

	time.Sleep(30 * time.Millisecond)
	if _, ok := r.Get(id); !ok {
		t.Fatal("a session with no exit signal was closed anyway")
	}
}
