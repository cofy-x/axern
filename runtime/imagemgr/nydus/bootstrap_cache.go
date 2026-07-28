package nydus

import (
	"container/list"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	bootstrapCacheDirName    = ".bootstrap_cache"
	bootstrapCacheFileExt    = ".bootstrap"
	bootstrapCacheEnvExt     = ".env"
	bootstrapCacheOutputName = "bootstrap"
	defaultBootstrapCacheCap = 128
)

type bootstrapCache struct {
	mu       sync.Mutex
	roots    map[string]*bootstrapCacheRoot
	now      func() time.Time
	capacity int
}

type bootstrapCacheRoot struct {
	root        string
	now         func() time.Time
	capacity    int
	mu          sync.Mutex
	initialized bool
	entries     map[string]*list.Element
	lru         *list.List
	keyLocks    map[string]*bootstrapCacheLock
}

type bootstrapCacheEntry struct {
	key  string
	path string
}

type bootstrapCacheLock struct {
	mu   sync.Mutex
	refs int
}

type bootstrapCacheFile struct {
	key     string
	path    string
	modTime time.Time
}

func newBootstrapCache() *bootstrapCache {
	return newBootstrapCacheWithCapacity(defaultBootstrapCacheCap)
}

func newBootstrapCacheWithCapacity(capacity int) *bootstrapCache {
	return &bootstrapCache{
		roots:    make(map[string]*bootstrapCacheRoot),
		now:      time.Now,
		capacity: capacity,
	}
}

// Link returns (outputPath, env, hit, error).
// env is nil when the cache entry predates env caching (caller should fetch from registry).
// env is non-nil (possibly empty) when the env sidecar was found.
func (c *bootstrapCache) Link(imageURL string, outputDir string) (string, []string, bool, error) {
	root := c.rootForOutput(outputDir)
	key := bootstrapCacheKey(imageURL)

	unlock := root.acquireKeyLock(key)
	defer unlock()

	if err := root.ensureInitialized(); err != nil {
		return "", nil, false, err
	}

	outputPath := bootstrapOutputPath(outputDir)

	root.mu.Lock()
	elem := root.entries[key]
	if elem == nil {
		root.mu.Unlock()
		logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, "", outputPath, "")).Debug("nydus bootstrap cache miss")
		return "", nil, false, nil
	}

	entry := elem.Value.(*bootstrapCacheEntry)
	if !fileExists(entry.path) {
		root.removeEntryLocked(elem)
		root.mu.Unlock()
		logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, entry.path, "", "")).Warn("nydus bootstrap cache entry disappeared, dropping it from cache")
		return "", nil, false, nil
	}
	root.lru.MoveToFront(elem)
	cachePath := entry.path
	root.mu.Unlock()

	_ = touchFile(cachePath, root.now())
	if err := ensureHardLink(cachePath, outputPath); err != nil {
		return "", nil, false, fmt.Errorf("failed to hardlink cached bootstrap %s to %s: %w", cachePath, outputPath, err)
	}

	// Read env sidecar — nil means old entry without env cache.
	env := readEnvSidecar(root.envPath(key))

	logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, cachePath, outputPath, "")).
		WithField("env_cached", env != nil).
		Debug("reused cached nydus bootstrap")

	return outputPath, env, true, nil
}

func (c *bootstrapCache) Store(imageURL string, outputDir string, bootstrapPath string, env []string) error {
	if bootstrapPath == "" {
		return fmt.Errorf("bootstrap path is empty")
	}

	root := c.rootForOutput(outputDir)
	key := bootstrapCacheKey(imageURL)

	unlock := root.acquireKeyLock(key)
	defer unlock()

	if err := root.ensureInitialized(); err != nil {
		return err
	}

	cachePath := root.cachePath(key)
	if err := ensureHardLink(bootstrapPath, cachePath); err != nil {
		return fmt.Errorf("failed to cache bootstrap %s at %s: %w", bootstrapPath, cachePath, err)
	}

	// Write env sidecar (even if env is empty, so we know it was cached).
	if err := writeEnvSidecar(root.envPath(key), env); err != nil {
		logrus.WithError(err).Warnf("failed to write env sidecar for %s", imageURL)
	}

	evictedPaths := root.recordAccess(key, cachePath)
	_ = touchFile(cachePath, root.now())

	logrus.WithFields(bootstrapCacheLogFields(imageURL, key, root.root, cachePath, "", bootstrapPath)).
		WithField("evicted_entries", len(evictedPaths)).
		Debug("stored nydus bootstrap in cache")

	for _, path := range evictedPaths {
		logrus.WithFields(bootstrapCacheLogFields("", "", root.root, path, "", "")).Info("evicted nydus bootstrap cache entry")
		_ = os.Remove(path)
		// Also remove env sidecar for evicted entries.
		envPath := strings.TrimSuffix(path, bootstrapCacheFileExt) + bootstrapCacheEnvExt
		_ = os.Remove(envPath)
	}
	return nil
}

func (c *bootstrapCache) rootForOutput(outputDir string) *bootstrapCacheRoot {
	rootDir := filepath.Join(filepath.Dir(filepath.Clean(outputDir)), bootstrapCacheDirName)

	c.mu.Lock()
	defer c.mu.Unlock()

	root := c.roots[rootDir]
	if root == nil {
		root = &bootstrapCacheRoot{
			root:     rootDir,
			now:      c.now,
			capacity: c.capacity,
			entries:  make(map[string]*list.Element),
			lru:      list.New(),
			keyLocks: make(map[string]*bootstrapCacheLock),
		}
		c.roots[rootDir] = root
	}
	return root
}
