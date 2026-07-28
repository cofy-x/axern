package runtime

import (
	"context"
	"errors"
	"strings"

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
	if err != nil && strings.Contains(err.Error(), "file does not exist") {
		if _, ok, readErr := r.readExitState(options.ContainerID); readErr == nil && ok {
			err = nil
		}
	}
	if err == nil {
		waitLock := r.waitLock(options.ContainerID)
		waitLock.Lock()
		err = r.common.RemoveExitState(options.ContainerID)
		waitLock.Unlock()
		r.waitLocks.Delete(options.ContainerID)
	}
	cleanupCtx, cancel := rootfsViewCleanupContext()
	defer cancel()
	err = errors.Join(err, r.rootfsViews.Remove(cleanupCtx, options.ContainerID))
	return &apipb.DeleteContainerResponse{}, err
}

func (r *RunscServiceHandler) cleanupContainer(ctx context.Context, traceID, containerID, msg string) {
	r.common.CleanupOnFailure(ctx, traceID, containerID, msg)
	cleanupCtx, cancel := rootfsViewCleanupContext()
	defer cancel()
	if err := r.rootfsViews.Remove(cleanupCtx, containerID); err != nil {
		logrus.WithField("trace_id", traceID).Warnf("cleanup writable rootfs view for %s failed: %v", containerID, err)
	}
}
