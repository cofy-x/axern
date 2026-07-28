package api

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

type nydusCacheResult uint8

const (
	nydusCacheMiss nydusCacheResult = iota
	nydusCacheOCI
	nydusCacheNydus
)

// nydusImageCache caches image format detection to avoid repeated registry fetches.
type nydusImageCache struct {
	mu       sync.RWMutex
	positive *expirable.LRU[string, struct{}]
	negative *expirable.LRU[string, struct{}]
}

// CacheConfig holds configuration for the cache
type CacheConfig struct {
	PositiveTTL     time.Duration // TTL for positive results, default 1 hour
	NegativeTTL     time.Duration // TTL for negative results, default 5 minutes
	MaxCacheEntries int           // Maximum cache entries, default 1000
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() *CacheConfig {
	return &CacheConfig{
		PositiveTTL:     1 * time.Hour,
		NegativeTTL:     5 * time.Minute,
		MaxCacheEntries: 1000,
	}
}

// newNydusImageCache creates a cache with the default policy.
func newNydusImageCache() *nydusImageCache {
	return newNydusImageCacheWithConfig(DefaultCacheConfig())
}

// newNydusImageCacheWithConfig creates a cache with a custom policy.
func newNydusImageCacheWithConfig(config *CacheConfig) *nydusImageCache {
	if config == nil {
		config = DefaultCacheConfig()
	}
	if config.MaxCacheEntries <= 0 {
		config.MaxCacheEntries = DefaultCacheConfig().MaxCacheEntries
	}
	if config.PositiveTTL <= 0 {
		config.PositiveTTL = DefaultCacheConfig().PositiveTTL
	}
	if config.NegativeTTL <= 0 {
		config.NegativeTTL = DefaultCacheConfig().NegativeTTL
	}

	positiveCap := config.MaxCacheEntries / 2
	if positiveCap == 0 {
		positiveCap = 1
	}
	negativeCap := config.MaxCacheEntries - positiveCap
	if negativeCap == 0 {
		negativeCap = 1
	}

	return &nydusImageCache{
		positive: expirable.NewLRU[string, struct{}](positiveCap, nil, config.PositiveTTL),
		negative: expirable.NewLRU[string, struct{}](negativeCap, nil, config.NegativeTTL),
	}
}

func (c *nydusImageCache) lookup(imageURL string) nydusCacheResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if _, found := c.positive.Get(imageURL); found {
		return nydusCacheNydus
	}
	if _, found := c.negative.Get(imageURL); found {
		return nydusCacheOCI
	}
	return nydusCacheMiss
}

// Set stores the result for an image URL
func (c *nydusImageCache) set(imageURL string, isNydus bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if isNydus {
		c.negative.Remove(imageURL)
		c.positive.Add(imageURL, struct{}{})
		return
	}
	c.positive.Remove(imageURL)
	c.negative.Add(imageURL, struct{}{})
}

// Clear removes all cached entries
func (c *nydusImageCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.positive.Purge()
	c.negative.Purge()
}

// Size returns the number of cached entries
func (c *nydusImageCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.positive.Len() + c.negative.Len()
}

// Invalidate removes a specific entry from the cache
func (c *nydusImageCache) invalidate(imageURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.positive.Remove(imageURL)
	c.negative.Remove(imageURL)
}
