package appservice

import "sync"

type serviceSyncLockEntry struct {
	mu   sync.Mutex
	refs int
}

type serviceSyncLocks struct {
	mu      sync.Mutex
	entries map[string]*serviceSyncLockEntry
}

func (l *serviceSyncLocks) lock(serviceID string) func() {
	l.mu.Lock()
	if l.entries == nil {
		l.entries = make(map[string]*serviceSyncLockEntry)
	}
	entry := l.entries[serviceID]
	if entry == nil {
		entry = &serviceSyncLockEntry{}
		l.entries[serviceID] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.entries, serviceID)
		}
		l.mu.Unlock()
	}
}
