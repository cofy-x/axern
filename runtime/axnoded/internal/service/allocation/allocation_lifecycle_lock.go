package allocation

import (
	"strings"
	"sync"
)

type allocationLifecycleLocks struct {
	mu    sync.Mutex
	locks map[string]*allocationLifecycleLock
}

type allocationLifecycleLock struct {
	mu   sync.Mutex
	refs int
}

func (l *allocationLifecycleLocks) Lock(allocationID string) func() {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return func() {}
	}

	l.mu.Lock()
	if l.locks == nil {
		l.locks = make(map[string]*allocationLifecycleLock)
	}
	lock := l.locks[allocationID]
	if lock == nil {
		lock = &allocationLifecycleLock{}
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
