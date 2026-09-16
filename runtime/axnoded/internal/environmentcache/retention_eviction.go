package environmentcache

import (
	"errors"
	"sort"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

type retentionEviction struct {
	environment *PreparedEnvironment
	rootfs      *RootFS
	rootfsType  string
	reason      string
	retained    bool
}

func (lm *EnvironmentCache) runSweeper(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case now := <-ticker.C:
			if err := lm.sweep(now); err != nil {
				logrus.WithError(err).Warn("evict expired environments")
			}
		}
	}
}

func (lm *EnvironmentCache) sweep(now time.Time) error {
	// Retry older cleanup independently of whether this tick expires any new
	// environments. Ordinary reference releases must not trigger unrelated RPCs.
	retryErr := lm.retryReleasedRootfs()
	evictions := lm.collectExpiredRetained(now, RetentionReasonTTLExpired)
	return errors.Join(retryErr, lm.executeEvictions(evictions))
}

func (lm *EnvironmentCache) collectExpiredRetained(now time.Time, reason string) []retentionEviction {
	lm.environmentMu.Lock()
	defer lm.environmentMu.Unlock()

	if len(lm.retainedMap) == 0 {
		return nil
	}

	ids := make([]string, 0, len(lm.retainedMap))
	for id, environment := range lm.retainedMap {
		if environment == nil || environment.expireAt.IsZero() || environment.expireAt.After(now) {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	evictions := make([]retentionEviction, 0, len(ids))
	for _, id := range ids {
		evictions = append(evictions, lm.prepareEvictionLocked(lm.retainedMap[id], reason))
	}
	lm.updateRetentionGaugesLocked()
	return evictions
}

func (lm *EnvironmentCache) collectAllRetained(reason string) []retentionEviction {
	lm.environmentMu.Lock()
	defer lm.environmentMu.Unlock()

	if len(lm.retainedMap) == 0 {
		return nil
	}

	ids := make([]string, 0, len(lm.retainedMap))
	for id := range lm.retainedMap {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	evictions := make([]retentionEviction, 0, len(ids))
	for _, id := range ids {
		evictions = append(evictions, lm.prepareEvictionLocked(lm.retainedMap[id], reason))
	}
	lm.updateRetentionGaugesLocked()
	return evictions
}

func (lm *EnvironmentCache) collectCapEvictionsLocked() []retentionEviction {
	if !lm.retentionEnabledLocked() || len(lm.retainedMap) <= lm.retentionMax {
		return nil
	}

	retained := make([]*PreparedEnvironment, 0, len(lm.retainedMap))
	for _, environment := range lm.retainedMap {
		retained = append(retained, environment)
	}
	sort.Slice(retained, func(i, j int) bool {
		if retained[i].idleSince.Equal(retained[j].idleSince) {
			return retained[i].ID < retained[j].ID
		}
		return retained[i].idleSince.Before(retained[j].idleSince)
	})

	excess := len(retained) - lm.retentionMax
	evictions := make([]retentionEviction, 0, excess)
	for i := 0; i < excess; i++ {
		evictions = append(evictions, lm.prepareEvictionLocked(retained[i], RetentionReasonCapacity))
	}
	return evictions
}

func (lm *EnvironmentCache) prepareEvictionLocked(environment *PreparedEnvironment, reason string) retentionEviction {
	if environment == nil || environment.released {
		return retentionEviction{}
	}

	lm.deleteEnvironmentIndexLocked(environment)

	eviction := retentionEviction{
		environment: environment,
		rootfs:      environment.RootFS,
		rootfsType:  rootfsTypeLabelFromConfig(environment.RootFS.Config()),
		reason:      reason,
		retained:    environment.retained,
	}

	environment.retained = false
	environment.released = true
	environment.idleSince = time.Time{}
	environment.expireAt = time.Time{}
	environment.ClearBundleTemplate()
	return eviction
}

func (lm *EnvironmentCache) executeEvictions(evictions []retentionEviction) error {
	if len(evictions) == 0 {
		return nil
	}

	var cleanupErr error
	for _, eviction := range evictions {
		if eviction.environment == nil {
			continue
		}

		releasedRootfs := false
		var err error
		if eviction.rootfs != nil {
			if eviction.retained {
				releasedRootfs, err = eviction.rootfs.ReleaseRetainedRef()
			} else {
				releasedRootfs, err = eviction.rootfs.ReleaseActiveRef()
			}
		}
		cleanupErr = errors.Join(cleanupErr, err)

		metrics.RecordRetentionEviction(RetentionReuseKindEnvironment, eviction.rootfsType, eviction.reason)
		if releasedRootfs {
			metrics.RecordRetentionEviction(RetentionReuseKindRootfs, eviction.rootfsType, eviction.reason)
		}

		logrus.WithFields(logrus.Fields{
			"environment_id":             eviction.environment.ID,
			"rootfs_type":                eviction.rootfsType,
			"reason":                     eviction.reason,
			"rootfs_references_released": releasedRootfs,
			"was_retained":               eviction.retained,
		}).Info("evicted prepared environment")
	}

	lm.updateRetentionGauges()
	return cleanupErr
}

func (lm *EnvironmentCache) retentionEnabledLocked() bool {
	return lm.retentionTTL > 0 && lm.retentionMax > 0
}

func (lm *EnvironmentCache) updateRetentionGauges() {
	lm.environmentMu.RLock()
	lm.updateRetentionGaugesLocked()
	lm.environmentMu.RUnlock()
}

func (lm *EnvironmentCache) updateRetentionGaugesLocked() {
	environmentCounts := map[string]float64{
		contract.StartupRootfsTypeLocal:   0,
		contract.StartupRootfsTypeImage:   0,
		contract.StartupRootfsTypeUnknown: 0,
	}
	for _, environment := range lm.retainedMap {
		if environment == nil || environment.RootFS == nil {
			continue
		}
		environmentCounts[rootfsTypeLabelFromConfig(environment.RootFS.Config())]++
	}
	for rootfsType, count := range environmentCounts {
		metrics.RecordRetainedEnvironmentGauge(rootfsType, count)
	}

	rootfsCounts := map[string]float64{
		contract.StartupRootfsTypeLocal:   0,
		contract.StartupRootfsTypeImage:   0,
		contract.StartupRootfsTypeUnknown: 0,
	}
	lm.rfMu.Lock()
	for _, entry := range lm.rootfsMap {
		if entry == nil || entry.rootfs == nil || entry.rootfs.RetainedRefCount() == 0 {
			continue
		}
		rootfsCounts[entry.rootfs.RootfsTypeLabel()]++
	}
	lm.rfMu.Unlock()
	for rootfsType, count := range rootfsCounts {
		metrics.RecordRetainedRootfsGauge(rootfsType, count)
	}
}
