package environmentcache

import (
	"context"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
)

func (lm *EnvironmentCache) acquire(environment *PreparedEnvironment) {
	if environment == nil {
		return
	}

	lm.environmentMu.Lock()
	defer lm.environmentMu.Unlock()

	if environment.released {
		logrus.Warningf("attempt to increment released prepared environment %v", environment.ID)
		return
	}

	if environment.retained {
		if err := environment.RootFS.MoveRetainedToActive(); err != nil {
			logrus.Warningf("failed to reactivate retained rootfs for %v: %v", environment.ID, err)
			return
		}
		if lm.retainedMap[environment.ID] == environment {
			delete(lm.retainedMap, environment.ID)
		}
		environment.retained = false
		environment.idleSince = time.Time{}
		environment.expireAt = time.Time{}
		rootfsType := rootfsTypeLabelFromConfig(environment.RootFS.Config())
		metrics.RecordRetentionReuse(RetentionReuseKindEnvironment, rootfsType)
		metrics.RecordRetentionReuse(RetentionReuseKindRootfs, rootfsType)
		logrus.WithFields(logrus.Fields{
			"environment_id": environment.ID,
			"rootfs_type":    rootfsType,
		}).Info("retained environment reused")
	}

	environment.refcnt++
	lm.updateRetentionGaugesLocked()
}

func (lm *EnvironmentCache) release(environment *PreparedEnvironment) {
	if environment == nil {
		return
	}

	var evictions []retentionEviction

	lm.environmentMu.Lock()
	if environment.released {
		lm.environmentMu.Unlock()
		return
	}

	environment.refcnt--
	if environment.refcnt < 0 {
		logrus.Warningf("Refcount %v < 0, leak happens.", environment.refcnt)
		environment.refcnt = 0
		lm.environmentMu.Unlock()
		return
	}

	if environment.refcnt == 0 && environment.superseded {
		evictions = append(evictions, lm.prepareEvictionLocked(environment, RetentionReasonConfigDrift))
	} else if environment.refcnt == 0 {
		now := time.Now().UTC()
		if lm.retentionEnabledLocked() {
			lm.retainLocked(environment, now)
			evictions = append(evictions, lm.collectCapEvictionsLocked()...)
		} else {
			evictions = append(evictions, lm.prepareEvictionLocked(environment, RetentionReasonDisabled))
		}
	}
	lm.updateRetentionGaugesLocked()
	lm.environmentMu.Unlock()

	lm.executeEvictions(context.Background(), evictions)
}

func (lm *EnvironmentCache) retainLocked(environment *PreparedEnvironment, now time.Time) {
	if environment == nil || environment.released || environment.retained {
		return
	}

	environment.RootFS.MoveActiveToRetained()
	environment.retained = true
	environment.idleSince = now
	environment.expireAt = now.Add(lm.retentionTTL)
	if !environment.superseded {
		lm.retainedMap[environment.ID] = environment
	}

	logrus.WithFields(logrus.Fields{
		"environment_id": environment.ID,
		"rootfs_type":    rootfsTypeLabelFromConfig(environment.RootFS.Config()),
		"expire_at":      environment.expireAt,
	}).Info("retained idle environment")
}
