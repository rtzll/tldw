package tldw

import (
	"context"
	"sync"
)

// workGates serialize cache acquisition for one video while allowing unrelated
// videos to proceed. Waiting callers may cancel without interrupting the owner.
// Entries are removed when the owner and all waiters have left.
type workGates struct {
	mu      sync.Mutex
	entries map[string]*workGate
}
type workGate struct {
	token chan struct{}
	users int
}

func withVideoWork[T any](ctx context.Context, g *workGates, key string, work func() (*T, error)) (*T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	if g.entries == nil {
		g.entries = make(map[string]*workGate)
	}
	entry := g.entries[key]
	if entry == nil {
		entry = &workGate{token: make(chan struct{}, 1)}
		g.entries[key] = entry
	}
	entry.users++
	g.mu.Unlock()
	leave := func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		entry.users--
		if entry.users == 0 {
			delete(g.entries, key)
		}
	}
	defer leave()
	select {
	case entry.token <- struct{}{}:
		defer func() { <-entry.token }()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return work()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
