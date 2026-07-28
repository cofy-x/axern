package nydus

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
)

func (r *bootstrapCacheRoot) ensureInitialized() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.initialized {
		return nil
	}

	if err := os.MkdirAll(r.root, 0755); err != nil {
		return fmt.Errorf("failed to create bootstrap cache dir %s: %w", r.root, err)
	}

	entries, err := os.ReadDir(r.root)
	if err != nil {
		return fmt.Errorf("failed to read bootstrap cache dir %s: %w", r.root, err)
	}

	files := make([]bootstrapCacheFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), bootstrapCacheFileExt) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		key := strings.TrimSuffix(entry.Name(), bootstrapCacheFileExt)
		files = append(files, bootstrapCacheFile{
			key:     key,
			path:    filepath.Join(r.root, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	for _, file := range files {
		elem := r.lru.PushBack(&bootstrapCacheEntry{key: file.key, path: file.path})
		r.entries[file.key] = elem
	}

	evictedPaths := r.evictLocked()
	for _, path := range evictedPaths {
		_ = os.Remove(path)
	}

	r.initialized = true
	logrus.WithFields(bootstrapCacheLogFields("", "", r.root, "", "", "")).
		WithField("cache_entries", len(r.entries)).
		WithField("evicted_entries", len(evictedPaths)).
		WithField("capacity", r.capacity).
		Debug("initialized nydus bootstrap cache")
	return nil
}

func (r *bootstrapCacheRoot) recordAccess(key string, cachePath string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if elem := r.entries[key]; elem != nil {
		entry := elem.Value.(*bootstrapCacheEntry)
		entry.path = cachePath
		r.lru.MoveToFront(elem)
	} else {
		elem = r.lru.PushFront(&bootstrapCacheEntry{key: key, path: cachePath})
		r.entries[key] = elem
	}

	return r.evictLocked()
}

func (r *bootstrapCacheRoot) evictLocked() []string {
	if r.capacity <= 0 {
		return nil
	}

	evicted := make([]string, 0)
	for len(r.entries) > r.capacity {
		elem := r.lru.Back()
		if elem == nil {
			break
		}
		entry := elem.Value.(*bootstrapCacheEntry)
		evicted = append(evicted, entry.path)
		r.removeEntryLocked(elem)
	}
	return evicted
}

func (r *bootstrapCacheRoot) removeEntryLocked(elem *list.Element) {
	if elem == nil {
		return
	}
	entry := elem.Value.(*bootstrapCacheEntry)
	delete(r.entries, entry.key)
	r.lru.Remove(elem)
}

func (r *bootstrapCacheRoot) acquireKeyLock(key string) func() {
	r.mu.Lock()
	lock := r.keyLocks[key]
	if lock == nil {
		lock = &bootstrapCacheLock{}
		r.keyLocks[key] = lock
	}
	lock.refs++
	r.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		r.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(r.keyLocks, key)
		}
		r.mu.Unlock()
	}
}
