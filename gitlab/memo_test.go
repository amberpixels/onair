package gitlab

import (
	"context"
	"testing"
)

// A caller waiting on another caller's fetch that died with that caller's
// context error fetches again under its own live context.
func TestMemoWaiterRetriesOwnersContextError(t *testing.T) {
	var m memo[string]
	ownerCtx, cancel := context.WithCancel(context.Background())
	started, waiting := make(chan struct{}), make(chan struct{})

	ownerDone := make(chan error)
	go func() {
		_, err := m.get(ownerCtx, "k", func() (string, error) {
			close(started)
			<-waiting
			cancel()
			return "", ownerCtx.Err()
		})
		ownerDone <- err
	}()
	<-started

	waiterDone := make(chan string)
	go func() {
		v, err := m.get(context.Background(), "k", func() (string, error) { return "fresh", nil })
		if err != nil {
			t.Errorf("waiter got %v, want a retry", err)
		}
		waiterDone <- v
	}()
	// Let the waiter reach the entry before the owner's fetch ends.
	for {
		m.mu.Lock()
		_, ok := m.entries["k"]
		m.mu.Unlock()
		if ok {
			break
		}
	}
	close(waiting)

	if err := <-ownerDone; err == nil {
		t.Error("owner should get its own context error")
	}
	if v := <-waiterDone; v != "fresh" {
		t.Errorf("waiter = %q, want a fresh fetch", v)
	}
}
