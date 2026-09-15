package runtime

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

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
	if err := hostlinux.VerifyCgroupMemoryDomain(options.CgroupPath, cgroupPath, options.MemoryLimitBytes, manifest.GetCgroupBootID(), manifest.GetCgroupMountIdentity(), manifest.GetCgroupParentInode(), manifest.GetCgroupLeafInode()); err != nil {
		return fmt.Errorf("verify runsc memory domain: %w", err)
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
