package runtime

import (
	"context"
	"errors"

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
