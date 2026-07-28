package imagefsd

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

type nydusRuntimePolicy struct {
	readaheadWorkers     int
	readaheadWindowBytes int
	decodedCacheBytes    int
}

func (mgr *manager) nydusRuntimePolicy() nydusRuntimePolicy {
	return nydusRuntimePolicy{
		readaheadWorkers:     mgr.nydusReadaheadWorkers,
		readaheadWindowBytes: mgr.nydusReadaheadWindowBytes,
		decodedCacheBytes:    mgr.nydusDecodedCacheBytes,
	}
}

func (p nydusRuntimePolicy) matches(meta *DaemonMeta) bool {
	return meta.ReadaheadWorkers == p.readaheadWorkers &&
		meta.ReadaheadWindowBytes == p.readaheadWindowBytes &&
		meta.DecodedCacheBytes == p.decodedCacheBytes
}

func (p nydusRuntimePolicy) apply(meta *DaemonMeta) {
	meta.ReadaheadWorkers = p.readaheadWorkers
	meta.ReadaheadWindowBytes = p.readaheadWindowBytes
	meta.DecodedCacheBytes = p.decodedCacheBytes
}

// reconcileNydusRuntimePolicy prevents recovered daemons from silently keeping
// stale process-level tuning after an imagemgr rollout. An active daemon must
// stop before its immutable launch arguments can be replaced.
func (mgr *manager) reconcileNydusRuntimePolicy(d *Daemon) error {
	if normalizeSourceType(d.meta.SourceType) != SourceTypeNydus {
		return nil
	}

	policy := mgr.nydusRuntimePolicy()
	if policy.matches(&d.meta) {
		return nil
	}

	if d.IsAlive() {
		logrus.WithFields(d.daemonLogFields()).Info("stopping daemon to apply updated Nydus runtime policy")
		if err := d.Unmount(); err != nil {
			return fmt.Errorf("stop daemon %s for Nydus runtime policy update: %w", d.meta.ID, err)
		}
	}

	policy.apply(&d.meta)
	if err := d.saveMeta(); err != nil {
		return fmt.Errorf("persist Nydus runtime policy for daemon %s: %w", d.meta.ID, err)
	}
	return nil
}
