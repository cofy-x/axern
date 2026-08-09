package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RunscServiceHandler) DeleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	force := options.ForceDelete || request.Timeout == 0
	err := r.deleteRuntimeContainer(ctx, options.ContainerID, force)
	if err != nil {
		return &apipb.DeleteContainerResponse{}, err
	}
	waitLock := r.waitLock(options.ContainerID)
	waitLock.Lock()
	exitStateErr := r.common.RemoveExitState(options.ContainerID)
	waitLock.Unlock()
	r.waitLocks.Delete(options.ContainerID)
	storageErr := cleanupOwnedRootfsStorage(options.ContainerID, r.rootfsViews.Remove, r.writableCapacity.Release)
	return &apipb.DeleteContainerResponse{}, errors.Join(exitStateErr, storageErr)
}

// deleteRuntimeContainer implements the runsc stop/delete protocol. Axnoded
// launches runsc through the foreground "run" command, which retains the
// sandbox state lock until the workload exits. Calling "delete --force"
// directly can therefore deadlock: delete waits for run while run cannot
// finish its state transition. A forced delete must signal the sandbox first,
// allowing the runtime runner to reap runsc and release the lock, and only
// then remove the stopped OCI state.
func (r *RunscServiceHandler) deleteRuntimeContainer(ctx context.Context, containerID string, force bool) error {
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
	if runtimeDeleteTargetAbsent(deleteErr, containerID) {
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
		logrus.WithField("trace_id", traceID).Warnf("runtime cleanup for %s failed; retaining rootfs and writable reservation: %v", containerID, err)
		return
	}
	if err := cleanupOwnedRootfsStorage(containerID, r.rootfsViews.Remove, r.writableCapacity.Release); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("cleanup writable storage for %s failed: %v", containerID, err)
	}
}
