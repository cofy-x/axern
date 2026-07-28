package imagefsd

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

func (d *Daemon) Mount() error {
	return d.mount(true)
}

// mount is the internal mount implementation.
// When userInitiated is true (explicit Mount call), userStopped is cleared to re-enable auto-remount.
// When userInitiated is false (auto-remount), userStopped is checked under lock and mount is
// rejected if the daemon was explicitly unmounted, closing the race window between remount()
// checking the flag and acquiring the lock.
func (d *Daemon) mount(userInitiated bool) error {
	timing, _ := StartTimedOperation(d.ctx, "daemon.Mount", d.meta.ID)
	defer timing.End()
	logrus.WithFields(d.daemonLogFields()).Info("daemon mount path started")

	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear mountFailed: a new mount attempt supersedes any prior timeout.
	// GC's unmountForGC checks this flag under d.mu, so they serialize correctly.
	d.mountFailed.Store(false)

	if userInitiated {
		d.userStopped.Store(false)
	} else if d.userStopped.Load() {
		logrus.WithFields(d.daemonLogFields()).Info("skip mount: daemon was explicitly unmounted")
		return fmt.Errorf("daemon %s was explicitly unmounted, skip auto-remount", d.meta.ID)
	}

	isAlive := d.IsAlive()
	state := d.getState()
	if state == DaemonStateRunning && isAlive {
		logrus.WithFields(d.daemonLogFields()).Info("daemon already mounted")
		return nil
	}

	if state != DaemonStateMounting {
		d.setState(DaemonStateMounting)
		go d.startDaemonProcess()
	}

	timeout := time.NewTimer(daemonMountTimeout)
	defer timeout.Stop()

	checkStart := time.Now()
	for {
		select {
		case <-timeout.C:
			d.mountFailed.Store(true)
			err := fmt.Errorf("timeout waiting for daemon %s to start", d.meta.ID)
			logrus.WithFields(d.daemonLogFields()).WithError(err).Error("daemon mount path failed")
			timing.Fail(err)
			return err
		default:
			state := d.getState()
			if state == DaemonStateRunning {
				timing.Stage("wait_daemon_running", time.Since(checkStart))
				logrus.WithFields(d.daemonLogFields()).Info("daemon mount path completed")
				return nil
			}
			if state == DaemonStateStopped {
				err := fmt.Errorf("daemon %s failed to start", d.meta.ID)
				logrus.WithFields(d.daemonLogFields()).WithError(err).Error("daemon mount path failed")
				timing.Fail(err)
				return err
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
