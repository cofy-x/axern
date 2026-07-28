package imagefsd

import (
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

func (d *Daemon) tick() bool {
	if !d.IsAlive() {
		return false
	}
	d.updateExpired()
	return true
}

func (d *Daemon) shouldRemount() bool {
	// Never remount if explicitly stopped by user/API
	if d.userStopped.Load() {
		return false
	}
	// Only remount if daemon was running (not mounting/unmounting/stopped)
	return d.getState() == DaemonStateRunning
}

func (d *Daemon) startWatch() {
	d.watcherActive.Store(true)
	go d.watch()
}

func (d *Daemon) watch() {
	ticker := time.NewTicker(5 * time.Second)
	defer func() {
		ticker.Stop()
		logrus.WithFields(d.daemonLogFields()).Info("daemon exited")
		close(d.stopChan)
		// Set watcherActive to false before remount
		// remount will call startWatch() which sets it back to true
		d.watcherActive.Store(false)
		if d.shouldRemount() {
			go d.remount()
		}
	}()

	for {
		select {
		case <-ticker.C:
			if !d.tick() {
				return
			}
		case <-d.kickStop.Done():
			for d.IsAlive() {
				time.Sleep(10 * time.Millisecond)
			}
			return
		}
	}
}

func (d *Daemon) remount() {
	// Early check: skip if user explicitly unmounted.
	// The authoritative check happens inside mount(false) under d.mu to close the race
	// window where Unmount() could execute between this check and acquiring the lock.
	if d.userStopped.Load() {
		logrus.WithFields(d.daemonLogFields()).Info("skip remount: daemon was explicitly unmounted")
		return
	}
	logrus.WithFields(d.daemonLogFields()).Info("try remount daemon")
	os.Remove(d.meta.PidFilePath)
	if err := d.mount(false); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to remount daemon")
	}
}
