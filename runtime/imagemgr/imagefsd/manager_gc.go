package imagefsd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/diskusage"
)

// cleanupDaemonResources removes all daemon resources from disk
// Caller must hold mgr.mu lock
func (mgr *manager) cleanupDaemonResources(d *Daemon) {
	logrus.WithFields(d.daemonLogFields()).Info("cleaning daemon resources")

	// Clean mount point to ensure FUSE filesystem is unmounted
	if err := d.cleanMountPoint(); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to clean mount point")
	}

	// Remove mount point directory if it's using the default path
	if d.meta.MountPoint != "" {
		defaultMountPath := filepath.Join(mgr.root, "mnt", d.meta.ID)
		if d.meta.MountPoint == defaultMountPath {
			if err := os.Remove(d.meta.MountPoint); err != nil && !os.IsNotExist(err) {
				logrus.Warnf("failed to remove mount point directory %s: %v", d.meta.MountPoint, err)
			}
		}
	}

	// Rescue daemon log for WARN/ERROR extraction before removing daemon dir
	rescueDaemonLog(mgr.root, d)

	// Clean daemon working directory
	if d.meta.DaemonDir != "" {
		if err := os.RemoveAll(d.meta.DaemonDir); err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to remove daemon dir")
		}
	}

	// Clean image metadata directory
	if d.meta.ImageMetaDir != "" {
		if err := os.RemoveAll(d.meta.ImageMetaDir); err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to remove image meta dir")
		}
	}

	// Clean daemon config file
	if d.savedPath != "" {
		if err := os.Remove(d.savedPath); err != nil && !os.IsNotExist(err) {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to remove daemon config")
		}
	}
}

// getDiskUsage returns the disk usage percentage for the given path
func (mgr *manager) getDiskUsage(path string) (float64, error) {
	return diskusage.UsedPercentByFree(path)
}

func (mgr *manager) gcDaemons() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// First pass: collect daemons stuck after mount timeout.
	var failedDaemons []*Daemon
	for _, d := range mgr.daemons {
		if d.mountFailed.Load() {
			failedDaemons = append(failedDaemons, d)
		}
	}

	// Unmount alive failed daemons outside mgr.mu to avoid contention.
	// unmountForGC checks mountFailed under d.mu — if a new mount() cleared
	// the flag since we released mgr.mu, the unmount is safely aborted.
	if len(failedDaemons) > 0 {
		mgr.mu.Unlock()
		for _, d := range failedDaemons {
			if d.IsAlive() {
				logrus.WithFields(d.daemonLogFields()).Warn("gc: unmounting daemon with failed mount but running process")
				d.unmountForGC()
			}
		}
		mgr.mu.Lock()

		// Re-check and clean up: only delete daemons still marked as failed.
		for _, d := range failedDaemons {
			if !d.mountFailed.Load() {
				continue
			}
			logrus.WithFields(d.daemonLogFields()).Info("gc: cleaning daemon with failed mount")
			mgr.cleanupDaemonResources(d)
			delete(mgr.daemons, d.meta.ID)
		}
	}

	// Second pass: normal expiry-based GC.
	nrToDelete := 4
	for _, d := range mgr.daemons {
		if d.IsAlive() || time.Now().UnixNano() < d.expiredAt {
			continue
		}
		logrus.WithFields(d.daemonLogFields()).Info("delete daemon")

		mgr.cleanupDaemonResources(d)
		delete(mgr.daemons, d.meta.ID)

		nrToDelete--
		if nrToDelete <= 0 {
			break
		}
	}
}

// gcDaemonsByDiskPressure cleans up inactive daemons when disk usage exceeds threshold
func (mgr *manager) gcDaemonsByDiskPressure() {
	// Check disk usage
	usagePercent, err := mgr.getDiskUsage(mgr.root)
	if err != nil {
		logrus.Errorf("failed to get disk usage: %v", err)
		return
	}

	logrus.Debugf("disk usage: %.2f%%", usagePercent)

	// Only trigger GC when disk usage > 90%
	if usagePercent <= 90.0 {
		return
	}

	logrus.Warnf("disk usage %.2f%% exceeds threshold, triggering daemon GC", usagePercent)

	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	// Collect inactive daemons (not alive and expired)
	type daemonWithExpiry struct {
		daemon    *Daemon
		expiredAt int64
	}
	var inactiveDaemons []daemonWithExpiry

	for _, d := range mgr.daemons {
		// Only collect inactive daemons (not alive)
		if !d.IsAlive() {
			inactiveDaemons = append(inactiveDaemons, daemonWithExpiry{
				daemon:    d,
				expiredAt: d.expiredAt,
			})
		}
	}

	if len(inactiveDaemons) == 0 {
		logrus.Info("no inactive daemons available for cleanup")
		return
	}

	// Sort by expiredAt from smallest to largest (oldest first)
	sort.Slice(inactiveDaemons, func(i, j int) bool {
		return inactiveDaemons[i].expiredAt < inactiveDaemons[j].expiredAt
	})

	// Clean up daemons until disk usage drops below 85% or no more daemons to clean
	cleaned := 0
	for _, item := range inactiveDaemons {
		d := item.daemon
		logrus.WithFields(d.daemonLogFields()).WithField("expired_at", d.expiredAt).Info("cleaning up inactive daemon due to disk pressure")

		mgr.cleanupDaemonResources(d)
		delete(mgr.daemons, d.meta.ID)
		cleaned++

		// Re-check disk usage after each cleanup
		usagePercent, err = mgr.getDiskUsage(mgr.root)
		if err != nil {
			logrus.Errorf("failed to re-check disk usage: %v", err)
			break
		}

		logrus.Infof("disk usage after cleanup: %.2f%%", usagePercent)

		// Stop if usage drops below 85%
		if usagePercent < 85.0 {
			logrus.Infof("disk usage dropped to %.2f%%, stopping cleanup", usagePercent)
			break
		}
	}

	logrus.Infof("disk pressure GC completed, cleaned %d inactive daemons", cleaned)
}

func (mgr *manager) gcWorker() {
	gcTicker := time.NewTicker(2 * time.Minute)
	defer gcTicker.Stop()

	for range gcTicker.C {
		mgr.mu.RLock()
		total := len(mgr.daemons)
		mgr.mu.RUnlock()
		logrus.Infof("try gc daemons, total daemons number: %d", total)

		// First check if disk pressure requires urgent cleanup
		mgr.gcDaemonsByDiskPressure()

		// Then run normal GC for expired daemons
		mgr.gcDaemons()
	}
}

// CleanupDaemon manually cleans up a daemon and all its resources.
// The daemon must be in Stopped state. Caller should Unmount() first if needed.
func (mgr *manager) CleanupDaemon(daemonID string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	d, ok := mgr.daemons[daemonID]
	if !ok {
		return fmt.Errorf("daemon not found: %s", daemonID)
	}

	// Only allow cleanup when daemon is stopped
	state := d.getState()
	if state != DaemonStateStopped {
		return fmt.Errorf("daemon %s is not stopped (state=%d), unmount it first", daemonID, state)
	}

	mgr.cleanupDaemonResources(d)
	delete(mgr.daemons, d.meta.ID)
	logrus.WithFields(d.daemonLogFields()).Info("successfully cleaned daemon")

	return nil
}

// ListDaemons returns basic information about all daemons
func (mgr *manager) ListDaemons() []DaemonInfo {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	infos := make([]DaemonInfo, 0, len(mgr.daemons))
	for _, d := range mgr.daemons {
		endpoint, bucket, objectPrefix := daemonObjectStoreIdentity(d)
		infos = append(infos, DaemonInfo{
			ID:           d.meta.ID,
			Name:         d.meta.Name,
			MountPoint:   d.meta.MountPoint,
			SourceType:   d.meta.SourceType,
			IsAlive:      d.IsAlive(),
			ImageURL:     d.meta.ImageURL,
			Endpoint:     endpoint,
			Bucket:       bucket,
			ObjectPrefix: objectPrefix,
		})
	}

	return infos
}

func daemonObjectStoreIdentity(d *Daemon) (endpoint, bucket, objectPrefix string) {
	if d == nil || d.config == nil {
		return "", "", ""
	}
	if d.config.S3 != nil {
		return d.config.S3.Endpoint, d.config.S3.BucketName, d.config.S3.ObjectPrefix
	}
	if d.config.Oss != nil {
		return d.config.Oss.Endpoint, d.config.Oss.BucketName, d.config.Oss.ObjectPrefix
	}
	return "", "", ""
}
