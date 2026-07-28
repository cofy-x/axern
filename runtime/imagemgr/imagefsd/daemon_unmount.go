package imagefsd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

func isMountPoint(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat path %s: %w", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("could not get syscall.Stat_t for %s", path)
	}
	parentPath := filepath.Dir(path)
	parentInfo, err := os.Lstat(parentPath)
	if err != nil {
		return false, fmt.Errorf("failed to stat parent path %s: %w", parentPath, err)
	}

	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("could not get syscall.Stat_t for parent %s", parentPath)
	}
	if stat.Dev != parentStat.Dev {
		return true, nil
	}
	if path == parentPath {
		return true, nil
	}
	return false, nil
}

func (d *Daemon) cleanMountPoint() error {
	isMount, err := isMountPoint(d.meta.MountPoint)
	if err != nil {
		return fmt.Errorf("could not determine if '%s' is a mount point: %w", d.meta.MountPoint, err)
	}
	if !isMount {
		return nil
	}
	if err := syscall.Unmount(d.meta.MountPoint, 0); err != nil {
		if errors.Is(err, syscall.EPERM) {
			return fmt.Errorf("failed to unmount '%s' due to permission denied. Did you run as root? Error: %w",
				d.meta.MountPoint, err)
		}
		if errors.Is(err, syscall.EBUSY) {
			return fmt.Errorf("failed to unmount '%s' because it is busy. Error: %w", d.meta.MountPoint, err)
		}
		return fmt.Errorf("unmount syscall failed for '%s': %w", d.meta.MountPoint, err)
	}
	logrus.WithFields(d.daemonLogFields()).Info("successfully unmount using syscall")
	return nil
}

func (d *Daemon) Unmount() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.unmountLocked()
}

// unmountForGC attempts to unmount a daemon that was marked as mount-failed.
// It checks mountFailed under d.mu: if a new mount() cleared the flag, the
// unmount is aborted. This closes the race window where GC releases mgr.mu
// and a mount request arrives before unmount begins.
func (d *Daemon) unmountForGC() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.mountFailed.Load() {
		logrus.WithFields(d.daemonLogFields()).Info("gc: abort unmount, new mount cleared mountFailed")
		return false
	}
	d.unmountLocked()
	return true
}

// unmountLocked performs the actual unmount. Caller must hold d.mu.
func (d *Daemon) unmountLocked() error {
	// Start timing operation
	timing, _ := StartTimedOperation(d.ctx, "daemon.Unmount", d.meta.ID)
	defer timing.End()
	logrus.WithFields(d.daemonLogFields()).Info("daemon unmount path started")

	timeout := time.NewTimer(daemonUnmountTimeout)
	defer timeout.Stop()
	defer func() {
		stageStart := time.Now()
		err := d.cleanMountPoint()
		if err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to clean mount point")
		}
		timing.Stage("clean_mount_point", time.Since(stageStart))

		// clean pid file
		if err = os.Remove(d.meta.PidFilePath); err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to clean daemon pid file")
		}
		d.setState(DaemonStateStopped)
		d.updateExpired()
	}()

	// Mark as explicitly stopped to prevent automatic remount
	d.userStopped.Store(true)

	// Set state to Unmounting to prevent remount
	d.setState(DaemonStateUnmounting)

	// Stage 1: Signal daemon to stop
	stageStart := time.Now()
	if d.kickStop != nil {
		d.kickStop.Close()
	}
	timing.Stage("signal_stop", time.Since(stageStart))

	// Check if process is alive
	if !d.IsAlive() {
		logrus.WithFields(d.daemonLogFields()).Info("daemon is not alive, skip signal and wait for watch goroutine")
		// Wait for watch goroutine to exit only if it was started
		// The watch goroutine is started only when stopChan is created in startDaemonProcess()
		// If daemon never started successfully, stopChan will be nil
		if d.stopChan != nil {
			select {
			case <-d.stopChan:
				logrus.WithFields(d.daemonLogFields()).Info("daemon watch goroutine exited")
			case <-timeout.C:
				logrus.WithFields(d.daemonLogFields()).Warn("wait daemon watch goroutine timeout")
			}
		}
		logrus.WithFields(d.daemonLogFields()).Info("daemon unmount path completed")
		return nil
	}

	pid := d.getPid()
	if pid <= 0 {
		return nil
	}

	// Stage 2: Send SIGTERM
	stageStart = time.Now()
	p, _ := os.FindProcess(pid)
	logrus.WithFields(d.daemonLogFields()).WithField("pid", p.Pid).Info("send termination signal to daemon")
	err := p.Signal(syscall.SIGTERM)
	if err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("daemon unmount path failed")
		timing.Fail(err)
		return fmt.Errorf("failed to send terminal signal to daemon %s, err = %w", d.meta.ID, err)
	}
	timing.Stage("send_sigterm", time.Since(stageStart))

	// Stage 3: Wait for graceful shutdown
	stageStart = time.Now()
	select {
	case <-d.stopChan:
		timing.Stage("wait_graceful_exit", time.Since(stageStart))
		logrus.WithFields(d.daemonLogFields()).Info("daemon unmount path completed")
		return nil
	case <-timeout.C:
		timing.Stage("wait_graceful_exit_timeout", time.Since(stageStart))
		logrus.WithFields(d.daemonLogFields()).Warn("wait daemon exited timeout, force kill it")

		// Stage 4: Force kill
		stageStart = time.Now()
		err = p.Kill()
		if err != nil {
			logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to send kill signal to daemon")
		}
		timing.Stage("send_sigkill", time.Since(stageStart))
	}

	timeout.Reset(5 * time.Second)

	// Stage 5: Final wait
	stageStart = time.Now()
	select {
	case <-d.stopChan:
		timing.Stage("wait_forced_exit", time.Since(stageStart))
		logrus.WithFields(d.daemonLogFields()).Info("daemon unmount path completed")
		return nil
	case <-timeout.C:
		timing.Stage("wait_forced_exit_timeout", time.Since(stageStart))
		err := fmt.Errorf("daemon %s is not exited", d.meta.ID)
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("daemon unmount path failed")
		timing.Fail(err)
		return err
	}
}
