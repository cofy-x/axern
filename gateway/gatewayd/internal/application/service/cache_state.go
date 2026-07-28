package service

import (
	"container/heap"
	"container/list"
	"hash/maphash"
	"sync"
	"time"

	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
)

type cacheShard struct {
	mu          sync.Mutex
	items       map[string]*cacheItem
	lru         *list.List
	expirations expirationQueue
	inflight    map[string]*resolveCall
}

func (s *cacheShard) init() {
	s.items = make(map[string]*cacheItem)
	s.lru = list.New()
	s.inflight = make(map[string]*resolveCall)
}

type cacheItem struct {
	key           string
	route         *gatewayv1.ResolveServiceRouteResponse
	expiresAt     time.Time
	next          int
	element       *list.Element
	heapIndex     int
	lastAccess    time.Time
	quarantined   map[string]time.Time
	endpointStats map[string]*endpointStats
}

type cacheDelta struct {
	routes      int64
	endpoints   int64
	quarantined int64
	inflight    int64
}

func (d cacheDelta) add(other cacheDelta) cacheDelta {
	d.routes += other.routes
	d.endpoints += other.endpoints
	d.quarantined += other.quarantined
	d.inflight += other.inflight
	return d
}

func (d cacheDelta) empty() bool {
	return d.routes == 0 && d.endpoints == 0 && d.quarantined == 0 && d.inflight == 0
}

type cacheState struct {
	routes      int
	endpoints   int
	quarantined int
	inflight    int
}

func (c *Cache) shard(key string) *cacheShard {
	index := maphash.String(c.hashSeed, key) % uint64(len(c.shards))
	return &c.shards[index]
}

func (c *Cache) resolveCachedRoute(key string, now time.Time) (*gatewayv1.ServiceRouteEndpoint, *gatewayv1.ServiceRoutePort, bool, bool, time.Duration) {
	shard := c.shard(key)
	shard.mu.Lock()
	itemBeforePrune := shard.items[key]
	expired := itemBeforePrune != nil && !now.Before(itemBeforePrune.expiresAt)
	delta, expiredCount := shard.pruneExpiredLocked(now)
	item := shard.items[key]
	if item == nil {
		c.applyDelta(delta)
		shard.mu.Unlock()
		c.observeCacheN("expire", expiredCount)
		if !delta.empty() {
			c.observeState()
		}
		return nil, nil, false, expired, 0
	}
	if len(item.route.GetEndpoints()) == 0 {
		delta = delta.add(shard.removeLocked(item))
		c.applyDelta(delta)
		shard.mu.Unlock()
		c.observeCacheN("expire", expiredCount)
		c.observeState()
		return nil, nil, false, true, 0
	}
	item.lastAccess = now
	shard.lru.MoveToFront(item.element)
	selectStart := time.Now()
	ep, selectDelta := pickEndpointLocked(item, now)
	selectDuration := time.Since(selectStart)
	delta = delta.add(selectDelta)
	port := item.route.GetPort()
	c.applyDelta(delta)
	shard.mu.Unlock()
	c.observeCacheN("expire", expiredCount)
	if !delta.empty() {
		c.observeState()
	}
	return ep, port, true, false, selectDuration
}

func (c *Cache) insertAndSelect(key string, route *gatewayv1.ResolveServiceRouteResponse, now time.Time) *gatewayv1.ServiceRouteEndpoint {
	shard := c.shard(key)
	shard.mu.Lock()
	delta, expiredCount := shard.pruneExpiredLocked(now)
	item := shard.items[key]
	inserted := item == nil
	if inserted {
		item = &cacheItem{
			key:           key,
			route:         route,
			expiresAt:     now.Add(c.ttl),
			heapIndex:     -1,
			lastAccess:    now,
			quarantined:   make(map[string]time.Time),
			endpointStats: make(map[string]*endpointStats),
		}
		item.element = shard.lru.PushFront(item)
		shard.items[key] = item
		heap.Push(&shard.expirations, item)
		delta.routes++
	} else {
		delta = delta.add(pruneEndpointStateLocked(item, route))
		shard.lru.MoveToFront(item.element)
	}
	if !inserted {
		item.route = route
		item.expiresAt = now.Add(c.ttl)
		item.lastAccess = now
		heap.Fix(&shard.expirations, item.heapIndex)
	}
	ep, selectDelta := pickEndpointLocked(item, now)
	delta = delta.add(selectDelta)
	c.applyDelta(delta)
	shard.mu.Unlock()
	c.observeCacheN("expire", expiredCount)
	if !delta.empty() {
		c.observeState()
	}
	if inserted {
		c.enforceCapacity(now)
	}
	return ep
}

func (c *Cache) enforceCapacity(now time.Time) {
	// Callers must not hold a shard lock: cross-shard work always takes capacityMu first.
	c.capacityMu.Lock()
	defer c.capacityMu.Unlock()

	expiredCount := 0
	changed := false
	for i := range c.shards {
		shard := &c.shards[i]
		shard.mu.Lock()
		delta, removed := shard.pruneExpiredLocked(now)
		c.applyDelta(delta)
		shard.mu.Unlock()
		expiredCount += removed
		changed = changed || !delta.empty()
	}

	evicted := 0
	for c.routes.Load() > c.maxEntries {
		candidate := c.oldestItem()
		if candidate.shard == nil {
			break
		}
		candidate.shard.mu.Lock()
		back := candidate.shard.lru.Back()
		if back != nil {
			delta := candidate.shard.removeLocked(back.Value.(*cacheItem))
			c.applyDelta(delta)
			changed = changed || !delta.empty()
			evicted++
		}
		candidate.shard.mu.Unlock()
	}
	if changed {
		c.observeState()
	}
	c.observeCacheN("expire", expiredCount)
	c.observeCacheN("evict", evicted)
}

type evictionCandidate struct {
	shard      *cacheShard
	lastAccess time.Time
}

func (c *Cache) oldestItem() evictionCandidate {
	selected := evictionCandidate{}
	for i := range c.shards {
		shard := &c.shards[i]
		shard.mu.Lock()
		back := shard.lru.Back()
		if back != nil {
			item := back.Value.(*cacheItem)
			if selected.shard == nil || item.lastAccess.Before(selected.lastAccess) {
				selected = evictionCandidate{shard: shard, lastAccess: item.lastAccess}
			}
		}
		shard.mu.Unlock()
	}
	return selected
}

func (s *cacheShard) pruneExpiredLocked(now time.Time) (cacheDelta, int) {
	delta := cacheDelta{}
	removed := 0
	for s.expirations.Len() > 0 {
		next := s.expirations[0]
		if now.Before(next.expiresAt) {
			break
		}
		delta = delta.add(s.removeLocked(next))
		removed++
	}
	return delta, removed
}

func (s *cacheShard) removeLocked(item *cacheItem) cacheDelta {
	if item == nil || s.items[item.key] != item {
		return cacheDelta{}
	}
	delete(s.items, item.key)
	s.lru.Remove(item.element)
	if item.heapIndex >= 0 {
		heap.Remove(&s.expirations, item.heapIndex)
	}
	return cacheDelta{
		routes:      -1,
		endpoints:   -int64(len(item.endpointStats)),
		quarantined: -int64(len(item.quarantined)),
	}
}

func (c *Cache) applyDelta(delta cacheDelta) {
	if delta.routes != 0 {
		c.routes.Add(delta.routes)
	}
	if delta.endpoints != 0 {
		c.endpoints.Add(delta.endpoints)
	}
	if delta.quarantined != 0 {
		c.quarantined.Add(delta.quarantined)
	}
	if delta.inflight != 0 {
		c.inflight.Add(delta.inflight)
	}
}

func (c *Cache) state() cacheState {
	return cacheState{
		routes:      int(c.routes.Load()),
		endpoints:   int(c.endpoints.Load()),
		quarantined: int(c.quarantined.Load()),
		inflight:    int(c.inflight.Load()),
	}
}

func (c *Cache) observeState() {
	if c.observer == nil {
		return
	}
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()
	state := c.state()
	c.observer.RouteCacheEntries("routes", state.routes)
	c.observer.RouteCacheEntries("endpoints", state.endpoints)
	c.observer.RouteCacheEntries("quarantined", state.quarantined)
	c.observer.RouteCacheEntries("inflight", state.inflight)
}

type expirationQueue []*cacheItem

func (q expirationQueue) Len() int           { return len(q) }
func (q expirationQueue) Less(i, j int) bool { return q[i].expiresAt.Before(q[j].expiresAt) }
func (q expirationQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].heapIndex = i
	q[j].heapIndex = j
}
func (q *expirationQueue) Push(value any) {
	item := value.(*cacheItem)
	item.heapIndex = len(*q)
	*q = append(*q, item)
}
func (q *expirationQueue) Pop() any {
	old := *q
	last := old[len(old)-1]
	old[len(old)-1] = nil
	*q = old[:len(old)-1]
	last.heapIndex = -1
	return last
}
