package deploycoord

import (
	"errors"
	"sync"
	"testing"
)

func TestCoordinatorSerializesLocalAndHostedAliases(t *testing.T) {
	var c Coordinator
	release, err := c.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Acquire(); !errors.Is(err, ErrBusy) {
				t.Errorf("concurrent mutation was not rejected: %v", err)
			}
		}()
	}
	wg.Wait()
	release()
	next, err := c.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	// A duplicated old completion cannot release a newer operation.
	release()
	if _, err := c.Acquire(); !errors.Is(err, ErrBusy) {
		t.Fatalf("old release unlocked new operation: %v", err)
	}
	next()
}

func TestCoordinatorInstancesDoNotBlockUnrelatedDesktopTests(t *testing.T) {
	var first, second Coordinator
	r1, err := first.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer r1()
	r2, err := second.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer r2()
}
