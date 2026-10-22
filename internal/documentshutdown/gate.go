// Package documentshutdown serializes the asynchronous document ACK required
// before the main process can quit or start an update installer.
package documentshutdown

import (
	"errors"
	"fmt"
	"sync"
)

var ErrPending = errors.New("document shutdown pending")

// continuationFailure is terminal: the action failed and its request was released.
type continuationFailure struct{ error }

func (continuationFailure) Released() bool { return true }

type State struct {
	Ready        bool   `json:"ready"`
	DirtyCount   int    `json:"dirtyCount"`
	StateVersion uint64 `json:"stateVersion"`
}

type Reply struct {
	RequestID    string `json:"requestId"`
	Decision     string `json:"decision"`
	StateVersion uint64 `json:"stateVersion"`
}

type RequestEvent struct {
	RequestID string `json:"requestId"`
	Intent    string `json:"intent"`
}

type pending struct {
	event     RequestEvent
	action    func() error
	emitted   bool
	executing bool
}

type Gate struct {
	mu       sync.Mutex
	state    State
	hasState bool
	next     uint64
	pending  *pending
	emit     func(RequestEvent)
}

func New(emit func(RequestEvent)) *Gate { return &Gate{emit: emit} }

func (g *Gate) UpdateState(state State) error {
	g.mu.Lock()
	if state.DirtyCount < 0 || (g.hasState && (state.StateVersion < g.state.StateVersion || (state.StateVersion == g.state.StateVersion && state != g.state))) {
		g.mu.Unlock()
		return errors.New("invalid or stale document state")
	}
	g.state, g.hasState = state, true
	event := g.eventLocked()
	g.mu.Unlock()
	g.emitEvent(event)
	return nil
}

// Request never waits for JavaScript. Even cached clean state needs a current
// ACK; a missing frontend therefore always leaves the process alive.
func (g *Gate) Request(intent string, action func() error) (string, error) {
	if (intent != "quit" && intent != "update") || action == nil {
		return "", errors.New("invalid document shutdown request")
	}
	g.mu.Lock()
	if g.pending != nil {
		id := g.pending.event.RequestID
		g.mu.Unlock()
		return id, ErrPending
	}
	g.next++
	id := fmt.Sprintf("documents-%d", g.next)
	g.pending = &pending{event: RequestEvent{RequestID: id, Intent: intent}, action: action}
	event := g.eventLocked()
	g.mu.Unlock()
	g.emitEvent(event)
	return id, nil
}

func (g *Gate) eventLocked() *RequestEvent {
	if g.pending == nil || g.pending.emitted || !g.state.Ready {
		return nil
	}
	g.pending.emitted = true
	event := g.pending.event
	return &event
}

func (g *Gate) emitEvent(event *RequestEvent) {
	if event != nil && g.emit != nil {
		g.emit(*event)
	}
}

func (g *Gate) Resolve(reply Reply) error {
	g.mu.Lock()
	p := g.pending
	if p == nil || p.event.RequestID != reply.RequestID || p.executing || !p.emitted || reply.StateVersion != g.state.StateVersion {
		g.mu.Unlock()
		return errors.New("invalid or stale document shutdown reply")
	}
	if reply.Decision == "cancel" {
		g.pending = nil
		g.mu.Unlock()
		return nil
	}
	if reply.Decision != "allow" || !g.state.Ready || g.state.DirtyCount != 0 {
		g.mu.Unlock()
		return errors.New("documents are not ready and clean")
	}
	// Reserve before dropping the lock. Reentrant and concurrent calls keep this
	// request until failure, or until the process actually exits on success.
	p.executing = true
	g.mu.Unlock()
	err := p.action()
	if err != nil {
		g.mu.Lock()
		g.pending = nil
		g.mu.Unlock()
		return continuationFailure{err}
	}
	return err
}
