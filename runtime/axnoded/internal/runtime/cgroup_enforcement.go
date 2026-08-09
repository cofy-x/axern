package runtime

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func (r *RuncServiceHandler) verifyMemoryEnforcement(ctx context.Context, options contract.HandlerOptions) error {
	if options.MemoryLimitBytes <= 0 {
		return nil
	}
	state, err := r.state(ctx, options.ContainerID)
	if err != nil {
		return fmt.Errorf("read runc state for memory enforcement: %w", err)
	}
	pid := 0
	if state.Pid != nil && *state.Pid > 0 {
		pid = *state.Pid
	} else {
		pid, err = r.common.RuntimePID(options.ContainerID)
		if err != nil {
			return fmt.Errorf("resolve runc host pid for memory enforcement: %w", err)
		}
	}
	if err := hostlinux.VerifyCgroupPIDs(options.CgroupPath, pid, 1); err != nil {
		return err
	}
	return nil
}

func (r *RunscServiceHandler) verifyMemoryEnforcement(ctx context.Context, options contract.HandlerOptions) error {
	if options.MemoryLimitBytes <= 0 {
		return nil
	}
	state, err := r.state(ctx, options.ContainerID)
	if err != nil {
		return fmt.Errorf("read runsc state for memory enforcement: %w", err)
	}
	if state.Pid <= 0 {
		return fmt.Errorf("runsc state has no Sentry host pid for memory enforcement")
	}
	if err := hostlinux.VerifyRunscCgroupProcesses(options.CgroupPath, state.Pid); err != nil {
		return fmt.Errorf("verify runsc Sentry/gofer cgroup attribution: %w", err)
	}
	return nil
}
