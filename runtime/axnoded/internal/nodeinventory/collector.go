package nodeinventory

import (
	"context"
	"sync"
	"time"
)

type CollectFunc func(context.Context) (NodeInventorySnapshot, bool)

type Collector struct {
	interval time.Duration
	collect  CollectFunc

	refreshMu sync.Mutex
	mu        sync.RWMutex
	snapshot  NodeInventorySnapshot
	ready     bool

	stopCh chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
}

func NewCollector(interval time.Duration, collect CollectFunc) *Collector {
	return &Collector{
		interval: interval,
		collect:  collect,
		stopCh:   make(chan struct{}),
		snapshot: NewSnapshot(),
	}
}

func (c *Collector) Start() {
	if c == nil {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.Refresh(context.Background())

		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				c.Refresh(context.Background())
			case <-c.stopCh:
				return
			}
		}
	}()
}

func (c *Collector) Refresh(ctx context.Context) (NodeInventorySnapshot, bool) {
	if c == nil {
		return NewSnapshot(), false
	}
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	snapshot, ready := c.collect(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot.Version == "" {
		snapshot.Version = SnapshotVersion
	}
	if snapshot.Sources == nil {
		snapshot.Sources = make(map[string]SourceStatus)
	}
	c.snapshot = snapshot
	if ready {
		c.ready = true
	}
	return c.snapshot, c.ready
}

func (c *Collector) Snapshot() (NodeInventorySnapshot, bool) {
	if c == nil {
		return NewSnapshot(), false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot, c.ready
}

func (c *Collector) Stop() {
	if c == nil {
		return
	}
	c.once.Do(func() {
		close(c.stopCh)
		c.wg.Wait()
	})
}
