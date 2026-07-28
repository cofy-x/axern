package imagefsd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// buildMountArgs generates CLI arguments for imagefsd based on source type
func (d *Daemon) buildMountArgs() []string {
	args := []string{
		"mount",
		"--daemon",
		"--log-file", d.meta.DaemonLogPath,
		"--pid-file", d.meta.PidFilePath,
		"--name", d.meta.Name,
		"--mountpoint", d.meta.MountPoint,
		"--node-id", d.nodeID,
	}

	switch normalizeSourceType(d.meta.SourceType) {
	case SourceTypeNydus:
		args = append(args,
			"--cache-dir", d.meta.CacheDir,
			"--src", SourceTypeNydus,
			"--bootstrap", d.meta.BootstrapPath,
		)
		if d.meta.ReadaheadWorkers > 0 {
			args = append(args, "--nydus-readahead-workers", fmt.Sprint(d.meta.ReadaheadWorkers))
			args = append(args, "--nydus-readahead-window-bytes", fmt.Sprint(d.meta.ReadaheadWindowBytes))
		}
		args = append(args, "--nydus-decoded-cache-bytes", fmt.Sprint(d.meta.DecodedCacheBytes))
	default:
		args = append(args,
			"--cache-file", d.meta.CachePath,
			"--src", SourceTypeOSS,
		)
	}

	args = append(args,
		"--cfg", d.meta.CfgPath,
		"--chunk-db-dir", d.meta.ChunkDBDir,
		"--image-meta-dir", d.meta.ImageMetaDir,
	)

	return args
}

// startDaemonProcess starts the daemon process in background
func (d *Daemon) startDaemonProcess() {
	timing, ctx := StartTimedOperation(d.ctx, "daemon.startDaemonProcess", d.meta.ID)
	defer timing.End()

	defer func() {
		if d.getState() == DaemonStateMounting {
			d.setState(DaemonStateStopped)
		}
	}()

	if !d.watcherActive.Load() {
		if err := d.initializeDaemon(ctx, timing); err != nil {
			return
		}
	}

	d.waitForMountReady(timing)
}

func (d *Daemon) startMountCommand(ctx context.Context, timing *TimedOperation) error {
	stageStart := time.Now()
	args := d.buildMountArgs()
	buildCmd := func() *exec.Cmd {
		return exec.CommandContext(ctx, d.binPath, args...)
	}
	c := buildCmd()
	logrus.WithFields(d.daemonLogFields()).WithField("command", fmt.Sprintf("%s %s", d.binPath, strings.Join(args, " "))).Info("mounting daemon")

	usedDirectPlacement := d.cgroupCtrl.Apply(c)

	err := c.Start()
	if err != nil && usedDirectPlacement && shouldRetryWithoutDirectPlacement(err) {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Warn("failed to start daemon with cgroup fd placement, retrying with cgroup.procs fallback")
		d.cgroupCtrl.DisableDirectPlacement()
		c = buildCmd()
		err = c.Start()
	}
	if err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("failed to start distill daemon")
		timing.Fail(err)
		return err
	}
	if err = c.Wait(); err != nil {
		logrus.WithFields(d.daemonLogFields()).WithError(err).Error("distill daemon exited abnormal")
		timing.Fail(err)
		return err
	}
	timing.Stage("start_daemon_process", time.Since(stageStart))

	if pid := d.getPid(); pid > 0 {
		if err := d.cgroupCtrl.AddPID(pid); err != nil {
			logrus.WithFields(d.daemonLogFields()).Warnf("cgroup: failed to add pid %d: %v", pid, err)
		}
	}

	return nil
}

func shouldRetryWithoutDirectPlacement(err error) bool {
	return errors.Is(err, syscall.ENOSYS) ||
		errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}
