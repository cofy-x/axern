package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RunscServiceHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	waitLock := r.waitLock(options.ContainerID)
	waitLock.Lock()
	defer waitLock.Unlock()

	if exit, ok, err := r.readExitState(options.ContainerID); ok {
		return exit, err
	} else if err != nil {
		return contract.Exit{}, fmt.Errorf("runsc exit state is unreadable for %s: %w: %w", options.ContainerID, err, contract.ErrExitStatusUnavailable)
	}

	if exit, ok, err := r.waitWithOCI(ctx, options.ContainerID); ok {
		if err != nil {
			return exit, err
		}
		accepted, persistErr := r.acceptWaitExit(ctx, options.ContainerID, exit)
		if persistErr != nil {
			return exit, persistErr
		}
		if accepted {
			return exit, nil
		}
	}
	if ctx.Err() != nil {
		return contract.Exit{}, ctx.Err()
	}
	logrus.Debugf("runsc wait did not produce an exit status yet for %s, falling back to exit-state polling", options.ContainerID)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var stoppedWithoutExitAt time.Time

	for {
		if exit, ok, err := r.readExitState(options.ContainerID); ok {
			if !stoppedWithoutExitAt.IsZero() {
				metrics.RecordRuntimeWaitGrace(config.RuntimeNameRunsc, "recovered")
			}
			return exit, err
		} else if err != nil {
			return contract.Exit{}, fmt.Errorf("runsc exit state is unreadable for %s: %w: %w", options.ContainerID, err, contract.ErrExitStatusUnavailable)
		}

		if state, err := r.state(ctx, options.ContainerID); err == nil && state.Status == string(contract.ContainerStatusExited) {
			if stoppedWithoutExitAt.IsZero() {
				stoppedWithoutExitAt = time.Now()
			}
			waitCtx, cancel := r.waitRetryContext(ctx)
			exit, ok, waitErr := r.waitWithOCI(waitCtx, options.ContainerID)
			cancel()
			if ok {
				if waitErr != nil {
					return exit, waitErr
				}
				accepted, persistErr := r.acceptWaitExit(ctx, options.ContainerID, exit)
				if persistErr != nil {
					return exit, persistErr
				}
				if accepted {
					metrics.RecordRuntimeWaitGrace(config.RuntimeNameRunsc, "recovered")
					return exit, nil
				}
				stoppedWithoutExitAt = time.Time{}
				continue
			}
			if time.Since(stoppedWithoutExitAt) >= runscExitStateGracePeriod {
				metrics.RecordRuntimeWaitGrace(config.RuntimeNameRunsc, "unavailable")
				return contract.Exit{Timestamp: time.Now(), Status: -1}, fmt.Errorf("runsc reported %s as stopped but did not provide an exit status: %w", options.ContainerID, contract.ErrExitStatusUnavailable)
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
