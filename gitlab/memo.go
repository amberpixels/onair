package gitlab

import (
	"context"
	"errors"
	"sync"
)

// memo runs one fetch per key and keeps its answer, errors included, for the
// client's lifetime. Concurrent callers of one key wait on a single request.
// A context error is never kept: it belongs to the caller's deadline, not to
// the forge's answer, so the next caller fetches again, and so does a waiter
// whose own context is still live.
type memo[V any] struct {
	mu      sync.Mutex
	entries map[string]*memoEntry[V]
}

type memoEntry[V any] struct {
	done chan struct{}
	val  V
	err  error
}

func (m *memo[V]) get(ctx context.Context, key string, fetch func() (V, error)) (V, error) {
	for {
		val, err, retry := m.try(ctx, key, fetch)
		if !retry {
			return val, err
		}
	}
}

// try runs or waits on one fetch. retry is true when this caller waited on
// another caller's fetch that ended in that caller's context error.
func (m *memo[V]) try(ctx context.Context, key string, fetch func() (V, error)) (val V, err error, retry bool) {
	m.mu.Lock()
	if m.entries == nil {
		m.entries = map[string]*memoEntry[V]{}
	}
	e, ok := m.entries[key]
	if !ok {
		e = &memoEntry[V]{done: make(chan struct{})}
		m.entries[key] = e
		m.mu.Unlock()

		e.val, e.err = fetch()
		if isContextErr(e.err) {
			m.mu.Lock()
			if m.entries[key] == e {
				delete(m.entries, key)
			}
			m.mu.Unlock()
		}
		close(e.done)
		return e.val, e.err, false
	}
	m.mu.Unlock()

	select {
	case <-e.done:
		if isContextErr(e.err) && ctx.Err() == nil {
			return val, nil, true
		}
		return e.val, e.err, false
	case <-ctx.Done():
		return val, ctx.Err(), false
	}
}

func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// put records an answer already known under another key, unless the key has
// one.
func (m *memo[V]) put(key string, val V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.entries == nil {
		m.entries = map[string]*memoEntry[V]{}
	}
	if _, ok := m.entries[key]; ok {
		return
	}
	done := make(chan struct{})
	close(done)
	m.entries[key] = &memoEntry[V]{done: done, val: val}
}
