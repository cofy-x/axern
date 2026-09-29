package allocation

import (
	"context"
	"strings"
	"sync"
)

// allocationKeyedLocks serializes work for one allocation without blocking
// independent allocations. Callers must not recursively acquire the same key.
type allocationKeyedLocks struct {
	mu    sync.Mutex
	locks map[string]*allocationKeyedLock
}

type allocationKeyedLock struct {
	gate chan struct{}
	refs int
}

func (l *allocationKeyedLocks) Lock(allocationID string) func() {
	unlock, _ := l.LockContext(context.Background(), allocationID)
	return unlock
}

func (l *allocationKeyedLocks) LockContext(ctx context.Context, allocationID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return func() {}, nil
	}

	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*allocationKeyedLock)
	}
	lock := l.locks[allocationID]
	if lock == nil {
		lock = &allocationKeyedLock{gate: make(chan struct{}, 1)}
		l.locks[allocationID] = lock
	}
	lock.refs++
	l.mu.Unlock()

	releaseRef := func() {
		l.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(l.locks, allocationID)
		}
		l.mu.Unlock()
	}
	select {
	case lock.gate <- struct{}{}:
		// Cancellation may have raced the available permit. Never enter a
		// lifecycle operation after its shutdown context has been cancelled.
		if err := ctx.Err(); err != nil {
			<-lock.gate
			releaseRef()
			return nil, err
		}
		return func() { <-lock.gate; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}
