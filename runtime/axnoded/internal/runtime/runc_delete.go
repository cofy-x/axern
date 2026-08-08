package runtime

import (
	"context"
	"errors"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

const rootfsViewCleanupTimeout = 30 * time.Second

func (r *RuncServiceHandler) DeleteContainer(ctx context.Context, request *apipb.DeleteContainerRequest, options contract.HandlerOptions) (*apipb.DeleteContainerResponse, error) {
	args := []string{"delete"}
	if options.ForceDelete || request.Timeout == 0 {
		args = append(args, "--force")
	}
	args = append(args, options.ContainerID)
	_, err := r.common.Run(ctx, args...)
	if runtimeDeleteTargetAbsent(err) {
		err = nil
	}
	if err != nil {
		return &apipb.DeleteContainerResponse{}, err
	}
	waitLock := r.waitLock(options.ContainerID)
	waitLock.Lock()
	err = r.common.RemoveExitState(options.ContainerID)
	waitLock.Unlock()
	r.waitLocks.Delete(options.ContainerID)
	cleanupCtx, cancel := rootfsViewCleanupContext()
	defer cancel()
	err = errors.Join(err, r.rootfsViews.Remove(cleanupCtx, options.ContainerID))
	if err == nil {
		err = r.writableCapacity.Release(options.ContainerID)
	}
	return &apipb.DeleteContainerResponse{}, err
}

func (r *RuncServiceHandler) cleanupContainer(ctx context.Context, traceID, containerID, msg string) {
	if err := r.common.CleanupOnFailure(ctx, traceID, containerID, msg); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("runtime cleanup for %s failed; retaining rootfs and writable reservation: %v", containerID, err)
		return
	}
	cleanupCtx, cancel := rootfsViewCleanupContext()
	defer cancel()
	if err := r.rootfsViews.Remove(cleanupCtx, containerID); err != nil {
		// Cleanup errors are best-effort here because the caller is already
		// returning the launch failure that explains why the container failed.
		logrus.WithField("trace_id", traceID).Warnf("cleanup writable rootfs view for %s failed: %v", containerID, err)
		return
	}
	if err := r.writableCapacity.Release(containerID); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("release writable reservation for %s failed: %v", containerID, err)
	}
}

func rootfsViewCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), rootfsViewCleanupTimeout)
}
