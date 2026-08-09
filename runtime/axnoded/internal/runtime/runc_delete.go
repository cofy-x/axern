package runtime

import (
	"context"
	"fmt"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RuncServiceHandler) DeleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
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
	monitorCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runcDeleteMonitorWaitTimeout)
	_, monitorErr := r.common.AwaitInitMonitorExitState(monitorCtx, options.ContainerID, r.Name())
	cancel()
	if monitorErr != nil {
		waitLock.Unlock()
		return &apipb.DeleteContainerResponse{}, fmt.Errorf("preserve runc allocation state until init monitor completes: %w", monitorErr)
	}
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

func (r *RuncServiceHandler) cleanupContainer(ctx context.Context, traceID, containerID, msg string) {
	if err := r.common.CleanupOnFailure(ctx, traceID, containerID, msg); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("runtime cleanup for %s failed; retaining rootfs and writable reservation: %v", containerID, err)
		return
	}
	monitorCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), runcDeleteMonitorWaitTimeout)
	_, monitorErr := r.common.AwaitInitMonitorExitState(monitorCtx, containerID, r.Name())
	cancel()
	if monitorErr != nil {
		logrus.WithField("trace_id", traceID).Warnf("runtime cleanup for %s is waiting for init monitor state; retaining rootfs and writable reservation: %v", containerID, monitorErr)
		return
	}
	if err := cleanupOwnedRootfsStorage(containerID, r.rootfsViews.Remove, r.writableCapacity.Release); err != nil {
		// Cleanup errors are best-effort here because the caller is already
		// returning the launch failure that explains why the container failed.
		logrus.WithField("trace_id", traceID).Warnf("cleanup writable storage for %s failed: %v", containerID, err)
		return
	}
	if err := r.common.RemoveContainerState(containerID); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("cleanup runtime monitor state for %s failed: %v", containerID, err)
	}
}
