package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

var conformanceCgroupStopped = hostlinux.ConformanceCgroupStopped

func (r *RunscServiceHandler) DeleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	if r.nativeRecovery && (!validPersistentContainerID(options.ContainerID) || options.ContainerID == "..") {
		return &apipb.DeleteContainerResponse{}, fmt.Errorf("native cleanup requires exact allocation identity")
	}
	force := options.ForceDelete || request.Timeout == 0
	var err error
	if r.nativeRecovery && options.EnforcementManifest != nil {
		err = r.deleteNativeRuntimeContainer(ctx, options, force)
	} else {
		err = r.deleteRuntimeContainer(ctx, options.ContainerID, force)
	}
	if err != nil {
		return &apipb.DeleteContainerResponse{}, err
	}
	waitLock := r.waitLock(options.ContainerID)
	waitLock.Lock()
	storageErr := cleanupOwnedRootfsStorage(options.ContainerID, r.rootfsViews.Remove, r.writableCapacity.Release)
	if storageErr != nil {
		waitLock.Unlock()
		return &apipb.DeleteContainerResponse{}, storageErr
	}
	exitStateErr := r.common.RemoveContainerState(options.ContainerID)
	waitLock.Unlock()
	r.waitLocks.Delete(options.ContainerID)
	return &apipb.DeleteContainerResponse{}, exitStateErr
}

// deleteRuntimeContainer implements the runsc stop/delete protocol. Axnoded
// launches runsc through the foreground "run" command, which retains the
// sandbox state lock until the workload exits. Calling "delete --force"
// directly can therefore deadlock: delete waits for run while run cannot
// finish its state transition. A forced delete must signal the sandbox first,
// allowing runsc to finish and release the lock, and only then remove the
// stopped OCI state.
func (r *RunscServiceHandler) deleteRuntimeContainer(ctx context.Context, containerID string, force bool) error {
	if r.nativeRecovery {
		_, err := r.state(ctx, containerID)
		absent, absentErr := r.nativeRuntimeContainerAbsent(ctx, containerID, err)
		if absentErr != nil {
			return fmt.Errorf("verify exact native runtime absence: %w", absentErr)
		}
		if absent {
			return nil
		}
		if err != nil {
			return fmt.Errorf("native cleanup cannot prove runtime absence without admitted enforcement ownership: %w", err)
		}
		return fmt.Errorf("native runtime cleanup requires admitted enforcement ownership")
	}
	var stopErr error
	if force {
		_, stopErr = r.runLifecycle(ctx, "kill", containerID, "KILL")
		if stopErr == nil {
			if err := r.waitForForegroundRunExit(ctx, containerID); err != nil {
				// Do not issue delete while the foreground run command may still
				// own the sandbox lock. The caller must retain rootfs storage and
				// let reconciliation retry the ordered stop protocol.
				return err
			}
		}
	}

	args := []string{"delete"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, containerID)
	_, deleteErr := r.runLifecycle(ctx, args...)
	if runtimeContainerAbsent(deleteErr, containerID) {
		return nil
	}
	if deleteErr != nil {
		return errors.Join(stopErr, deleteErr)
	}
	// A successful delete is authoritative. The preceding kill may legitimately
	// report that an already-stopped sandbox was not running.
	return nil
}

func (r *RunscServiceHandler) waitForForegroundRunExit(parent context.Context, containerID string) error {
	ctx, cancel := context.WithTimeout(parent, runscForceStopTimeout)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, ok, err := r.readExitState(containerID); err != nil {
			return fmt.Errorf("read runsc exit state after forced stop: %w", err)
		} else if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for foreground runsc command to release sandbox state: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *RunscServiceHandler) cleanupContainer(ctx context.Context, traceID, containerID, msg string) {
	logrus.WithField("trace_id", traceID).Warn(msg)
	if err := r.deleteRuntimeContainer(ctx, containerID, true); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("runtime cleanup for %s failed; retaining rootfs and writable charge: %v", containerID, err)
		return
	}
	if err := cleanupOwnedRootfsStorage(containerID, r.rootfsViews.Remove, r.writableCapacity.Release); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("cleanup writable storage for %s failed: %v", containerID, err)
		return
	}
	if err := r.common.RemoveContainerState(containerID); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("cleanup runtime exit state for %s failed: %v", containerID, err)
	}
}

// deleteNativeRuntimeContainer is entered only after the caller's lifecycle
// and output barriers, using its admitted manifest. Never ask runsc delete to
// signal saved host PIDs: after daemon downtime they may identify other tasks.
func (r *RunscServiceHandler) deleteNativeRuntimeContainer(parent context.Context, options contract.HandlerOptions, force bool) error {
	ctx, cancel := context.WithTimeout(parent, runscForceStopTimeout)
	defer cancel()
	id := options.ContainerID
	_, stateErr := r.state(ctx, id)
	absent, absentErr := r.nativeRuntimeContainerAbsent(ctx, id, stateErr)
	if absentErr != nil {
		return fmt.Errorf("verify exact native runtime absence: %w", absentErr)
	}
	if absent {
		// A previous no-signal retirement may have completed before resource
		// cleanup. Keep the normal owned-storage cleanup idempotent without
		// requiring a private projection that may already have been removed.
		return nil
	} else if stateErr != nil {
		return fmt.Errorf("inspect native runtime before cleanup: %w", stateErr)
	}
	if err := r.validateRecoveryManifest(ctx, id, options.EnforcementManifest); err != nil {
		return err
	}
	legacyConformance := options.RuntimeConformanceCleanup && options.EnforcementManifest.GetMemoryLimitBytes() == 0
	if force && !legacyConformance {
		if err := r.KillSandbox(ctx, options); err != nil && !runtimeContainerAbsent(err, id) {
			return err
		}
	}
	manifest := options.EnforcementManifest
	return forgetNativeRuntimeState(ctx, r.common.RuntimeRoot(), id, manifest.GetBundlePath(), func(pid int) error {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			var stopped bool
			var err error
			if legacyConformance {
				stopped, err = conformanceCgroupStopped(manifest.GetCgroupPath(), pid)
			} else {
				stopped, err = recoveryCgroupStopped(manifest.GetRuntimeCgroupPath(), pid, manifest.GetCgroupBootID(), manifest.GetCgroupMountIdentity(), manifest.GetCgroupLeafInode())
			}
			if err != nil {
				return err
			}
			if stopped {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("wait for native sandbox kernel termination: %w", ctx.Err())
			case <-ticker.C:
			}
		}
	})
}
