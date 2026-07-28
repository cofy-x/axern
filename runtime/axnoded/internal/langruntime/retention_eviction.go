package langruntime

import (
	"context"
	"sort"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

type retentionEviction struct {
	runtime    *LanguageRuntime
	rootfs     *RootFS
	rootfsType string
	reason     string
	retained   bool
	envelope   *ExecutionEnvelope
}

func (lm *LangRTManager) runSweeper(stopCh <-chan struct{}, doneCh chan<- struct{}) {
	defer close(doneCh)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case now := <-ticker.C:
			evictions := lm.collectExpiredRetained(now, RetentionReasonTTLExpired)
			lm.executeEvictions(context.Background(), evictions)
		}
	}
}

func (lm *LangRTManager) collectExpiredRetained(now time.Time, reason string) []retentionEviction {
	lm.lrMu.Lock()
	defer lm.lrMu.Unlock()

	if len(lm.retainedMap) == 0 {
		return nil
	}

	ids := make([]string, 0, len(lm.retainedMap))
	for id, lr := range lm.retainedMap {
		if lr == nil || lr.expireAt.IsZero() || lr.expireAt.After(now) {
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

func (lm *LangRTManager) collectAllRetained(reason string) []retentionEviction {
	lm.lrMu.Lock()
	defer lm.lrMu.Unlock()

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

func (lm *LangRTManager) collectCapEvictionsLocked() []retentionEviction {
	if !lm.retentionEnabledLocked() || len(lm.retainedMap) <= lm.retentionMax {
		return nil
	}

	retained := make([]*LanguageRuntime, 0, len(lm.retainedMap))
	for _, lr := range lm.retainedMap {
		retained = append(retained, lr)
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

func (lm *LangRTManager) prepareEvictionLocked(lr *LanguageRuntime, reason string) retentionEviction {
	if lr == nil || lr.released {
		return retentionEviction{}
	}

	lm.deleteRuntimeIndexLocked(lr)

	eviction := retentionEviction{
		runtime:    lr,
		rootfs:     lr.RootFS,
		rootfsType: rootfsTypeLabelFromConfig(lr.RootFS.Config()),
		reason:     reason,
		retained:   lr.retained,
		envelope:   lr.ClearExecutionEnvelope(),
	}

	lr.retained = false
	lr.released = true
	lr.idleSince = time.Time{}
	lr.expireAt = time.Time{}
	lr.ClearBundleTemplate()
	return eviction
}

func (lm *LangRTManager) executeEvictions(ctx context.Context, evictions []retentionEviction) {
	if len(evictions) == 0 {
		return
	}

	for _, eviction := range evictions {
		if eviction.runtime == nil {
			continue
		}

		releasedRootfs := false
		if eviction.envelope != nil && eviction.envelope.Destroy != nil {
			if err := destroyExecutionEnvelope(ctx, eviction.envelope); err != nil {
				logrus.WithError(err).Warnf("destroy execution envelope for runtime %s failed", eviction.runtime.ID)
			}
		}
		if eviction.rootfs != nil {
			if eviction.retained {
				releasedRootfs = eviction.rootfs.ReleaseRetainedRef()
			} else {
				releasedRootfs = eviction.rootfs.ReleaseActiveRef()
			}
		}

		metrics.RecordRetentionEviction(RetentionReuseKindRuntime, eviction.rootfsType, eviction.reason)
		if releasedRootfs {
			metrics.RecordRetentionEviction(RetentionReuseKindRootfs, eviction.rootfsType, eviction.reason)
		}

		logrus.WithFields(logrus.Fields{
			"runtime_id":        eviction.runtime.ID,
			"rootfs_type":       eviction.rootfsType,
			"reason":            eviction.reason,
			"rootfs_released":   releasedRootfs,
			"was_retained":      eviction.retained,
			"temporary_runtime": eviction.runtime.temporary,
		}).Info("evicted language runtime")
	}

	lm.updateRetentionGauges()
}

func (lm *LangRTManager) retentionEnabled() bool {
	lm.lrMu.RLock()
	defer lm.lrMu.RUnlock()
	return lm.retentionEnabledLocked()
}

func (lm *LangRTManager) retentionEnabledLocked() bool {
	return lm.retentionTTL > 0 && lm.retentionMax > 0
}

func (lm *LangRTManager) updateRetentionGauges() {
	lm.lrMu.RLock()
	lm.updateRetentionGaugesLocked()
	lm.lrMu.RUnlock()
}

func (lm *LangRTManager) updateRetentionGaugesLocked() {
	runtimeCounts := map[string]float64{
		contract.StartupRootfsTypeLocal:   0,
		contract.StartupRootfsTypeImage:   0,
		contract.StartupRootfsTypeS3:      0,
		contract.StartupRootfsTypeUnknown: 0,
	}
	for _, lr := range lm.retainedMap {
		if lr == nil || lr.RootFS == nil {
			continue
		}
		runtimeCounts[rootfsTypeLabelFromConfig(lr.RootFS.Config())]++
	}
	for rootfsType, count := range runtimeCounts {
		metrics.RecordRetainedRuntimeGauge(rootfsType, count)
	}

	rootfsCounts := map[string]float64{
		contract.StartupRootfsTypeLocal:   0,
		contract.StartupRootfsTypeImage:   0,
		contract.StartupRootfsTypeS3:      0,
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
	lm.updateExecutionEnvelopeGaugesLocked()
}
