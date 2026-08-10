package allocation

import (
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
	mu   sync.Mutex
	refs int
}

func (l *allocationKeyedLocks) Lock(allocationID string) func() {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return func() {}
	}

	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*allocationKeyedLock)
	}
	lock := l.locks[allocationID]
	if lock == nil {
		lock = &allocationKeyedLock{}
		l.locks[allocationID] = lock
	}
	lock.refs++
	l.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()

		l.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(l.locks, allocationID)
		}
		l.mu.Unlock()
	}
}
