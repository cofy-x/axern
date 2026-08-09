package langruntime

import (
	"context"
	"fmt"
	"time"
)

const (
	RetentionReuseKindRuntime = "runtime"
	RetentionReuseKindRootfs  = "rootfs"

	RetentionReasonTTLExpired  = "ttl_expired"
	RetentionReasonCapacity    = "capacity"
	RetentionReasonShutdown    = "shutdown"
	RetentionReasonDisabled    = "disabled"
	RetentionReasonConfigDrift = "config_drift"
	RetentionReasonSelfTest    = "self_test_cleanup"
)

type RetentionStats struct {
	RetainedRuntimeCount int
	RetainedRootfsCount  int
	RuntimeByType        map[string]int
	RootfsByType         map[string]int
}

func (lm *LangRTManager) ConfigureRetention(ttl time.Duration, maxRetained int) {
	lm.lrMu.Lock()
	lm.retentionTTL = ttl
	lm.retentionMax = maxRetained
	lm.lrMu.Unlock()
	lm.updateRetentionGauges()
}

func (lm *LangRTManager) Start() {
	lm.sweeperMu.Lock()
	defer lm.sweeperMu.Unlock()

	if lm.sweeperStop != nil || !lm.retentionEnabled() {
		return
	}

	lm.sweeperStop = make(chan struct{})
	lm.sweeperDone = make(chan struct{})
	go lm.runSweeper(lm.sweeperStop, lm.sweeperDone)
}

func (lm *LangRTManager) Close() {
	lm.sweeperMu.Lock()
	stopCh := lm.sweeperStop
	doneCh := lm.sweeperDone
	lm.sweeperStop = nil
	lm.sweeperDone = nil
	lm.sweeperMu.Unlock()

	if stopCh != nil {
		close(stopCh)
		<-doneCh
	}
}

func (lm *LangRTManager) DrainRetained(ctx context.Context, reason string) {
	evictions := lm.collectAllRetained(reason)
	lm.executeEvictions(ctx, evictions)
}

// EvictIdleRuntime removes one runtime after its last allocation has been
// deleted. It is intentionally scoped by runtime ID so internal probes can
// prove that their own runtime and rootfs references were cleaned without
// disturbing unrelated retained workloads.
func (lm *LangRTManager) EvictIdleRuntime(ctx context.Context, runtimeID, reason string) error {
	lm.lrMu.Lock()
	lr := lm.lrtMap[runtimeID]
	if lr == nil {
		lm.lrMu.Unlock()
		return nil
	}
	if lr.refcnt != 0 {
		refCount := lr.refcnt
		lm.lrMu.Unlock()
		return fmt.Errorf("runtime %q still has %d active references", runtimeID, refCount)
	}
	eviction := lm.prepareEvictionLocked(lr, reason)
	lm.updateRetentionGaugesLocked()
	lm.lrMu.Unlock()

	return lm.executeEvictions(ctx, []retentionEviction{eviction})
}

func (lm *LangRTManager) RetentionStats() RetentionStats {
	stats := RetentionStats{
		RuntimeByType: make(map[string]int),
		RootfsByType:  make(map[string]int),
	}

	lm.lrMu.RLock()
	for _, lr := range lm.retainedMap {
		if lr == nil || lr.RootFS == nil {
			continue
		}
		stats.RetainedRuntimeCount++
		stats.RuntimeByType[rootfsTypeLabelFromConfig(lr.RootFS.Config())]++
	}
	lm.lrMu.RUnlock()

	lm.rfMu.Lock()
	for _, entry := range lm.rootfsMap {
		if entry == nil || entry.rootfs == nil || entry.rootfs.RetainedRefCount() == 0 {
			continue
		}
		stats.RetainedRootfsCount++
		stats.RootfsByType[entry.rootfs.RootfsTypeLabel()]++
	}
	lm.rfMu.Unlock()

	return stats
}
