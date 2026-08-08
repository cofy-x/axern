package runtime

import (
	"context"
	"errors"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RunscServiceHandler) DeleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	args := []string{"delete"}
	if options.ForceDelete || request.Timeout == 0 {
		args = append(args, "--force")
	}
	args = append(args, options.ContainerID)
	_, err := r.common.Run(ctx, args...)
	if runtimeDeleteTargetAbsent(err, options.ContainerID) {
		err = nil
	}
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

func (r *RunscServiceHandler) cleanupContainer(ctx context.Context, traceID, containerID, msg string) {
	if err := r.common.CleanupOnFailure(ctx, traceID, containerID, msg); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("runtime cleanup for %s failed; retaining rootfs and writable reservation: %v", containerID, err)
		return
	}
	if err := cleanupOwnedRootfsStorage(containerID, r.rootfsViews.Remove, r.writableCapacity.Release); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("cleanup writable storage for %s failed: %v", containerID, err)
	}
}
