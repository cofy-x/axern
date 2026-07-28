package langruntime

import (
	"context"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
)

func (lm *LangRTManager) acquire(lr *LanguageRuntime) {
	if lr == nil {
		return
	}

	lm.lrMu.Lock()
	defer lm.lrMu.Unlock()

	if lr.released {
		logrus.Warningf("attempt to increment released language runtime %v", lr.ID)
		return
	}

	if lr.retained {
		if err := lr.RootFS.MoveRetainedToActive(); err != nil {
			logrus.Warningf("failed to reactivate retained rootfs for %v: %v", lr.ID, err)
			return
		}
		if lm.retainedMap[lr.ID] == lr {
			delete(lm.retainedMap, lr.ID)
		}
		lr.retained = false
		lr.idleSince = time.Time{}
		lr.expireAt = time.Time{}
		rootfsType := rootfsTypeLabelFromConfig(lr.RootFS.Config())
		metrics.RecordRetentionReuse(RetentionReuseKindRuntime, rootfsType)
		metrics.RecordRetentionReuse(RetentionReuseKindRootfs, rootfsType)
		logrus.WithFields(logrus.Fields{
			"runtime_id":  lr.ID,
			"rootfs_type": rootfsType,
		}).Info("retained runtime reused")
	}

	lr.refcnt++
	lm.updateRetentionGaugesLocked()
}

func (lm *LangRTManager) release(lr *LanguageRuntime) {
	if lr == nil {
		return
	}

	var evictions []retentionEviction

	lm.lrMu.Lock()
	if lr.released {
		lm.lrMu.Unlock()
		return
	}

	lr.refcnt--
	if lr.refcnt < 0 {
		logrus.Warningf("Refcount %v < 0, leak happens.", lr.refcnt)
		lr.refcnt = 0
		lm.lrMu.Unlock()
		return
	}

	if lr.refcnt == 0 && lr.superseded {
		evictions = append(evictions, lm.prepareEvictionLocked(lr, RetentionReasonConfigDrift))
	} else if lr.refcnt == 0 && lr.temporary {
		now := time.Now().UTC()
		if lm.retentionEnabledLocked() {
			lm.retainLocked(lr, now)
			evictions = append(evictions, lm.collectCapEvictionsLocked()...)
		} else {
			evictions = append(evictions, lm.prepareEvictionLocked(lr, RetentionReasonDisabled))
		}
	}
	lm.updateRetentionGaugesLocked()
	lm.lrMu.Unlock()

	lm.executeEvictions(context.Background(), evictions)
}

func (lm *LangRTManager) setTemporary(lr *LanguageRuntime, temporary bool) {
	if lr == nil {
		return
	}

	var evictions []retentionEviction

	lm.lrMu.Lock()
	if lr.released {
		lm.lrMu.Unlock()
		return
	}

	lr.temporary = temporary
	if !temporary && lr.retained {
		lm.resumeRetainedLocked(lr)
	}
	if temporary && lr.refcnt == 0 && !lr.retained {
		now := time.Now().UTC()
		if lm.retentionEnabledLocked() {
			lm.retainLocked(lr, now)
			evictions = append(evictions, lm.collectCapEvictionsLocked()...)
		} else {
			evictions = append(evictions, lm.prepareEvictionLocked(lr, RetentionReasonDisabled))
		}
	}
	lm.updateRetentionGaugesLocked()
	lm.lrMu.Unlock()

	lm.executeEvictions(context.Background(), evictions)
}

func (lm *LangRTManager) retainLocked(lr *LanguageRuntime, now time.Time) {
	if lr == nil || lr.released || lr.retained {
		return
	}

	lr.RootFS.MoveActiveToRetained()
	lr.retained = true
	lr.idleSince = now
	lr.expireAt = now.Add(lm.retentionTTL)
	if !lr.superseded {
		lm.retainedMap[lr.ID] = lr
	}

	logrus.WithFields(logrus.Fields{
		"runtime_id":  lr.ID,
		"rootfs_type": rootfsTypeLabelFromConfig(lr.RootFS.Config()),
		"expire_at":   lr.expireAt,
	}).Info("retained idle runtime")
}

func (lm *LangRTManager) resumeRetainedLocked(lr *LanguageRuntime) {
	if lr == nil || !lr.retained || lr.released {
		return
	}

	if err := lr.RootFS.MoveRetainedToActive(); err != nil {
		logrus.Warningf("failed to reactivate retained rootfs for %v: %v", lr.ID, err)
		return
	}
	if lm.retainedMap[lr.ID] == lr {
		delete(lm.retainedMap, lr.ID)
	}
	lr.retained = false
	lr.idleSince = time.Time{}
	lr.expireAt = time.Time{}

	rootfsType := rootfsTypeLabelFromConfig(lr.RootFS.Config())
	metrics.RecordRetentionReuse(RetentionReuseKindRuntime, rootfsType)
	metrics.RecordRetentionReuse(RetentionReuseKindRootfs, rootfsType)
	logrus.WithFields(logrus.Fields{
		"runtime_id":  lr.ID,
		"rootfs_type": rootfsType,
	}).Info("retained runtime resumed")
}
