package api

import (
	"testing"
	"time"
)

func TestNydusImageCache_SetAndGet(t *testing.T) {
	cache := newNydusImageCache()

	// Test cache miss
	if got := cache.lookup("test-image:latest"); got != nydusCacheMiss {
		t.Error("Expected cache miss, got hit")
	}

	// Test set and get
	cache.set("test-image:latest", true)
	if got := cache.lookup("test-image:latest"); got != nydusCacheNydus {
		t.Errorf("Expected Nydus cache result, got %v", got)
	}

	// Test negative cache
	cache.set("regular-image:latest", false)
	if got := cache.lookup("regular-image:latest"); got != nydusCacheOCI {
		t.Errorf("Expected OCI cache result, got %v", got)
	}
}

func TestNydusImageCache_CustomTTL(t *testing.T) {
	config := &CacheConfig{
		PositiveTTL:     2 * time.Second,
		NegativeTTL:     1 * time.Second,
		MaxCacheEntries: 100,
	}
	cache := newNydusImageCacheWithConfig(config)

	// Set positive entry
	cache.set("nydus-image:latest", true)
	// Set negative entry
	cache.set("regular-image:latest", false)

	// Should be in cache immediately
	if got := cache.lookup("nydus-image:latest"); got != nydusCacheNydus {
		t.Error("Expected positive cache hit immediately")
	}

	if got := cache.lookup("regular-image:latest"); got != nydusCacheOCI {
		t.Error("Expected negative cache hit immediately")
	}

	// Wait for negative cache to expire
	time.Sleep(1100 * time.Millisecond)

	if got := cache.lookup("regular-image:latest"); got != nydusCacheMiss {
		t.Error("Expected negative cache miss after TTL")
	}

	// Positive cache should still be valid
	if got := cache.lookup("nydus-image:latest"); got != nydusCacheNydus {
		t.Error("Expected positive cache hit after 1s")
	}

	// Wait for positive cache to expire
	time.Sleep(1 * time.Second)

	if got := cache.lookup("nydus-image:latest"); got != nydusCacheMiss {
		t.Error("Expected positive cache miss after TTL")
	}
}

func TestNydusImageCache_Size(t *testing.T) {
	cache := newNydusImageCache()

	if cache.size() != 0 {
		t.Errorf("Expected size=0, got %d", cache.size())
	}

	cache.set("image1:latest", true)
	cache.set("image2:latest", false)
	cache.set("image3:latest", true)

	if cache.size() != 3 {
		t.Errorf("Expected size=3, got %d", cache.size())
	}
}

func TestNydusImageCache_Clear(t *testing.T) {
	cache := newNydusImageCache()

	cache.set("image1:latest", true)
	cache.set("image2:latest", false)

	cache.clear()

	if cache.size() != 0 {
		t.Errorf("Expected size=0 after clear, got %d", cache.size())
	}

	if got := cache.lookup("image1:latest"); got != nydusCacheMiss {
		t.Error("Expected cache miss after clear")
	}
}

func TestNydusImageCache_Invalidate(t *testing.T) {
	cache := newNydusImageCache()

	cache.set("image1:latest", true)
	cache.set("image2:latest", false)

	// Invalidate one entry
	cache.invalidate("image1:latest")

	if cache.size() != 1 {
		t.Errorf("Expected size=1 after invalidate, got %d", cache.size())
	}

	if got := cache.lookup("image1:latest"); got != nydusCacheMiss {
		t.Error("Expected cache miss after invalidate")
	}

	// image2 should still exist
	if got := cache.lookup("image2:latest"); got != nydusCacheOCI {
		t.Error("Expected cache hit for non-invalidated entry")
	}
}

func TestNydusImageCache_Expiration(t *testing.T) {
	cache := newNydusImageCacheWithConfig(&CacheConfig{
		PositiveTTL:     200 * time.Millisecond,
		NegativeTTL:     100 * time.Millisecond,
		MaxCacheEntries: 10,
	})

	cache.set("regular-image:latest", false)
	cache.set("nydus-image:latest", true)

	time.Sleep(150 * time.Millisecond)

	if got := cache.lookup("regular-image:latest"); got != nydusCacheMiss {
		t.Error("Expected negative entry to expire first")
	}
	if got := cache.lookup("nydus-image:latest"); got != nydusCacheNydus {
		t.Error("Expected positive entry to still be valid")
	}

	time.Sleep(100 * time.Millisecond)

	if got := cache.lookup("nydus-image:latest"); got != nydusCacheMiss {
		t.Error("Expected positive entry to expire")
	}
}

func TestNydusImageCache_ConcurrentAccess(t *testing.T) {
	cache := newNydusImageCache()
	done := make(chan bool)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(id int) {
			cache.set("image", id%2 == 0)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			cache.lookup("image")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}

	// Should not panic
	if cache.size() == 0 {
		t.Error("Expected at least one entry in cache")
	}
}

func TestNydusImageCache_DefaultConfig(t *testing.T) {
	config := DefaultCacheConfig()

	if config.PositiveTTL != 1*time.Hour {
		t.Errorf("Expected default positive TTL=1h, got %v", config.PositiveTTL)
	}

	if config.NegativeTTL != 5*time.Minute {
		t.Errorf("Expected default negative TTL=5m, got %v", config.NegativeTTL)
	}

	if config.MaxCacheEntries != 1000 {
		t.Errorf("Expected default max entries=1000, got %d", config.MaxCacheEntries)
	}
}
