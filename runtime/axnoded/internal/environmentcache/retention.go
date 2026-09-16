package environmentcache

import (
	"errors"
	"fmt"
	"time"
)

const (
	RetentionReuseKindEnvironment = "environment"
	RetentionReuseKindRootfs      = "rootfs"

	RetentionReasonTTLExpired  = "ttl_expired"
	RetentionReasonCapacity    = "capacity"
	RetentionReasonShutdown    = "shutdown"
	RetentionReasonDisabled    = "disabled"
	RetentionReasonConfigDrift = "config_drift"
	RetentionReasonSelfTest    = "self_test_cleanup"
)

type RetentionStats struct {
	RetainedEnvironmentCount int
	RetainedRootfsCount      int
	EnvironmentByType        map[string]int
	RootfsByType             map[string]int
}

func (lm *EnvironmentCache) ConfigureRetention(ttl time.Duration, maxRetained int) {
	lm.environmentMu.Lock()
	lm.retentionTTL = ttl
	lm.retentionMax = maxRetained
	lm.environmentMu.Unlock()
	lm.updateRetentionGauges()
}

func (lm *EnvironmentCache) Start() {
	lm.sweeperMu.Lock()
	defer lm.sweeperMu.Unlock()

	if lm.sweeperStop != nil {
		return
	}

	lm.sweeperStop = make(chan struct{})
	lm.sweeperDone = make(chan struct{})
	go lm.runSweeper(lm.sweeperStop, lm.sweeperDone)
}

func (lm *EnvironmentCache) Close() {
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

func (lm *EnvironmentCache) DrainRetained(reason string) error {
	retryErr := lm.retryReleasedRootfs()
	evictions := lm.collectAllRetained(reason)
	return errors.Join(retryErr, lm.executeEvictions(evictions))
}

// EvictIdleEnvironment removes one prepared environment after its last allocation has been
// deleted. It is intentionally scoped by environment ID so internal probes can
// prove that their own prepared environment and rootfs references were cleaned without
// disturbing unrelated retained workloads.
func (lm *EnvironmentCache) EvictIdleEnvironment(environmentID, reason string) error {
	lm.environmentMu.Lock()
	environment := lm.environments[environmentID]
	if environment == nil {
		lm.environmentMu.Unlock()
		return lm.retryReleasedRootfs()
	}
	if environment.refcnt != 0 {
		refCount := environment.refcnt
		lm.environmentMu.Unlock()
		return fmt.Errorf("prepared environment %q still has %d active references", environmentID, refCount)
	}
	eviction := lm.prepareEvictionLocked(environment, reason)
	lm.updateRetentionGaugesLocked()
	lm.environmentMu.Unlock()

	return lm.executeEvictions([]retentionEviction{eviction})
}

func (lm *EnvironmentCache) RetentionStats() RetentionStats {
	stats := RetentionStats{
		EnvironmentByType: make(map[string]int),
		RootfsByType:      make(map[string]int),
	}

	lm.environmentMu.RLock()
	for _, environment := range lm.retainedMap {
		if environment == nil || environment.RootFS == nil {
			continue
		}
		stats.RetainedEnvironmentCount++
		stats.EnvironmentByType[rootfsTypeLabelFromConfig(environment.RootFS.Config())]++
	}
	lm.environmentMu.RUnlock()

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
