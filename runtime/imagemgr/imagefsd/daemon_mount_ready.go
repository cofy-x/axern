package imagefsd

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func (d *Daemon) waitForMountReady(timing *TimedOperation) {
	stageStart := time.Now()
	var lastProgressLog time.Time
	var lastErrorLog time.Time
	logrus.WithFields(d.daemonLogFields()).Info("waiting for mount readiness via statfs zero blocks")
	for {
		select {
		case <-d.stopChan:
			d.setState(DaemonStateStopped)
			logrus.WithFields(d.daemonLogFields()).Error("daemon exited abnormally")
			timing.Fail(fmt.Errorf("daemon exited"))
			return
		default:
			now := time.Now()
			isMountReady, fs, err := checkMountReady(d.MountPoint())
			if err != nil {
				if shouldLogMountReadiness(now, lastErrorLog) {
					lastErrorLog = now
					fields := d.daemonLogFields()
					fields["wait_elapsed_ms"] = now.Sub(stageStart).Milliseconds()
					logrus.WithFields(fields).WithError(err).Warn("mount readiness statfs failed, keep waiting")
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if isMountReady {
				elapsed := time.Since(stageStart)
				d.setState(DaemonStateRunning)
				timing.Stage("wait_mount_ready", elapsed)
				fields := d.daemonLogFields()
				fields["wait_elapsed_ms"] = elapsed.Milliseconds()
				logrus.WithFields(fields).Info("mount daemon successfully")
				return
			}
			if shouldLogMountReadiness(now, lastProgressLog) {
				lastProgressLog = now
				fields := d.daemonLogFields()
				fields["wait_elapsed_ms"] = now.Sub(stageStart).Milliseconds()
				fields["statfs_blocks"] = fs.Blocks
				fields["statfs_bsize"] = fs.Bsize
				logrus.WithFields(fields).Info("mount not ready yet, waiting for statfs zero blocks")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func checkMountReady(path string) (bool, unix.Statfs_t, error) {
	var fs unix.Statfs_t
	if err := statfsFunc(path, &fs); err != nil {
		return false, fs, fmt.Errorf("failed to statfs mountpoint %s: %w", path, err)
	}

	return reportsZeroSize(&fs), fs, nil
}

func reportsZeroSize(fs *unix.Statfs_t) bool {
	return fs.Blocks == 0
}

func shouldLogMountReadiness(now, lastLog time.Time) bool {
	return lastLog.IsZero() || now.Sub(lastLog) >= mountReadinessLogPeriod
}
