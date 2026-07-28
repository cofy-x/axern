package langruntime

import (
	"context"
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
