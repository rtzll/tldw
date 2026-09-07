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

func (g *workGates) acquire(ctx context.Context, key string) (func(), error) {
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
	select {
	case entry.token <- struct{}{}:
		release := func() { <-entry.token; leave() }
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
}
