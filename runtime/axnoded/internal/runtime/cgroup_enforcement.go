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
	manifest, err := r.AllocationEnforcementManifest(ctx, options.ContainerID)
	if err != nil {
		return fmt.Errorf("read immutable runc enforcement manifest: %w", err)
	}
	if err := verifyDurableEnforcementManifest(options.EnforcementManifest, manifest); err != nil {
		return err
	}
	cgroupPath := options.RuntimeCgroupPath
	if err := hostlinux.VerifyCgroupMemoryLimit(cgroupPath, options.MemoryLimitBytes); err != nil {
		return fmt.Errorf("verify runc memory.max: %w", err)
	}
	state, err := r.state(ctx, options.ContainerID)
	if err != nil {
		return inconclusiveCapabilityErrorf("read runc state for memory enforcement: %w", err)
	}
	if state.Pid == nil || *state.Pid <= 0 {
		return fmt.Errorf("runc state has no init host pid for memory enforcement")
	}
	launchPID, err := r.common.RuntimePID(options.ContainerID)
	if err != nil {
		return fmt.Errorf("resolve runc launch pid for memory enforcement: %w", err)
	}
	if launchPID != *state.Pid {
		return fmt.Errorf("runc state pid %d differs from immutable launch pid %d", *state.Pid, launchPID)
	}
	if err := hostlinux.VerifyCgroupPIDs(cgroupPath, *state.Pid, 1); err != nil {
		return err
	}
	return nil
}

func (r *RunscServiceHandler) verifyMemoryEnforcement(ctx context.Context, options contract.HandlerOptions) error {
	if options.MemoryLimitBytes <= 0 {
		return nil
	}
	manifest, err := r.AllocationEnforcementManifest(ctx, options.ContainerID)
	if err != nil {
		return fmt.Errorf("read immutable runsc enforcement manifest: %w", err)
	}
	if err := verifyDurableEnforcementManifest(options.EnforcementManifest, manifest); err != nil {
		return err
	}
	cgroupPath := options.RuntimeCgroupPath
	if err := hostlinux.VerifyCgroupMemoryLimit(cgroupPath, options.MemoryLimitBytes); err != nil {
		return fmt.Errorf("verify runsc memory.max: %w", err)
	}
	state, err := r.state(ctx, options.ContainerID)
	if err != nil {
		return inconclusiveCapabilityErrorf("read runsc state for memory enforcement: %w", err)
	}
	if state.Pid <= 0 {
		return fmt.Errorf("runsc state has no Sentry host pid for memory enforcement")
	}
	if err := hostlinux.VerifyRunscCgroupProcesses(cgroupPath, state.Pid, r.common.Binary()); err != nil {
		return fmt.Errorf("verify runsc Sentry/gofer cgroup attribution: %w", err)
	}
	return nil
}
