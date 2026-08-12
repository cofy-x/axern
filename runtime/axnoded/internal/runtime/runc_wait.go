package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func (r *RuncServiceHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	waitLock := r.waitLock(options.ContainerID)
	waitLock.Lock()
	defer waitLock.Unlock()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var stoppedWithoutExitAt time.Time

	for {
		if exit, ok, err := r.readExitState(options.ContainerID); ok {
			if !stoppedWithoutExitAt.IsZero() {
				metrics.RecordRuntimeWaitGrace(config.RuntimeNameRunc, "recovered")
			}
			return exit, err
		} else if err != nil {
			return contract.Exit{}, fmt.Errorf("runc exit state is unreadable for %s: %w: %w", options.ContainerID, err, contract.ErrExitStatusUnavailable)
		}
		if err := ctx.Err(); err != nil {
			return contract.Exit{}, err
		}

		state, err := r.state(ctx, options.ContainerID)
		if err != nil {
			if runtimeContainerAbsent(err, options.ContainerID) {
				return contract.Exit{Timestamp: time.Now().UTC(), Status: -1}, fmt.Errorf(
					"runc container %s is absent before its init monitor exit state was consumed: %v: %w",
					options.ContainerID,
					err,
					contract.ErrExitStatusUnavailable,
				)
			}
			return contract.Exit{}, fmt.Errorf("read runc state while waiting for %s: %w", options.ContainerID, err)
		}
		if state.Status == string(contract.ContainerStatusExited) {
			if stoppedWithoutExitAt.IsZero() {
				stoppedWithoutExitAt = time.Now()
			}
			if time.Since(stoppedWithoutExitAt) >= runcExitStateGracePeriod {
				metrics.RecordRuntimeWaitGrace(config.RuntimeNameRunc, "unavailable")
				return contract.Exit{Timestamp: time.Now(), Status: -1}, fmt.Errorf("runc reported %s as stopped but its init monitor did not provide an exit status: %w", options.ContainerID, contract.ErrExitStatusUnavailable)
			}
		} else {
			stoppedWithoutExitAt = time.Time{}
		}

		select {
		case <-ctx.Done():
			return contract.Exit{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
