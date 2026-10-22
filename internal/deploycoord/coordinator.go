// Package deploycoord serializes remote deployment mutations in one desktop.
package deploycoord

import (
	"errors"
	"sync"
)

var ErrBusy = errors.New("another deployment operation is already running")

// Coordinator is ready to use without initialization. Each App owns one, shared
// by the local and hosted deployment adapters.
type Coordinator struct {
	mu   sync.Mutex
	busy bool
}

func (c *Coordinator) Acquire() (func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy {
		return nil, ErrBusy
	}
	c.busy = true
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			c.busy = false
			c.mu.Unlock()
		})
	}, nil
}
