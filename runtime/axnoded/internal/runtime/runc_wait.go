package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (r *RuncServiceHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	if exit, ok, err := r.readExitState(options.ContainerID); ok || err != nil {
		return exit, err
	}

	exit, ok, err := r.common.Wait(ctx, "wait", options.ContainerID)
	if ok && err == nil {
		return exit, r.persistExitState(options.ContainerID, exit)
	}
	if ok {
		return contract.Exit{}, err
	}
	if ctx.Err() != nil {
		return contract.Exit{}, ctx.Err()
	}
	logrus.WithError(err).Debugf("runc wait failed for %s, falling back to exit-state polling", options.ContainerID)
	return r.waitByState(ctx, options.ContainerID, err)
}

func (r *RuncServiceHandler) waitByState(ctx context.Context, containerID string, waitErr error) (contract.Exit, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if exit, ok, err := r.readExitState(containerID); ok || err != nil {
			return exit, err
		}

		state, err := r.state(ctx, containerID)
		if err != nil {
			return contract.Exit{}, fmt.Errorf("runc wait failed: %w; state fallback failed: %v", waitErr, err)
		}
		if state.Status == string(contract.ContainerStatusExited) {
			exitCode := 0
			switch {
			case state.ExitStatus != nil:
				exitCode = *state.ExitStatus
			case state.ExitCode != nil:
				exitCode = *state.ExitCode
			}
			exit := contract.Exit{Timestamp: time.Now(), Status: exitCode}
			if persistErr := r.persistExitState(containerID, exit); persistErr != nil {
				return contract.Exit{}, persistErr
			}
			return exit, nil
		}

		select {
		case <-ctx.Done():
			return contract.Exit{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
