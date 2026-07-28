package nodeinventory

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectorNilSafe(t *testing.T) {
	var collector *Collector

	collector.Start()
	collector.Stop()

	snapshot, ready := collector.Snapshot()
	if ready {
		t.Fatal("nil collector should not be ready")
	}
	if snapshot.Version != SnapshotVersion {
		t.Fatalf("snapshot version = %s, want %s", snapshot.Version, SnapshotVersion)
	}

	snapshot, ready = collector.Refresh(context.Background())
	if ready {
		t.Fatal("nil collector refresh should not be ready")
	}
	if snapshot.Version != SnapshotVersion {
		t.Fatalf("refresh version = %s, want %s", snapshot.Version, SnapshotVersion)
	}
}

func TestCollectorSerializesRefresh(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	collector := NewCollector(time.Hour, func(context.Context) (NodeInventorySnapshot, bool) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		return NewSnapshot(), true
	})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collector.Refresh(context.Background())
		}()
	}
	wg.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent refreshes = %d, want 1", got)
	}
}
