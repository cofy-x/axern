package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

// Read the admitted controls and process membership from the same leaf proved
// at launch. A quota or weight reported by OCI is never the expected value.
func (r *RunscServiceHandler) VerifyAllocationCPUControls(ctx context.Context, id string, manifest *apipb.AllocationEnforcementManifest, expected *apipb.LinuxContainerResources) contract.CapabilityVerification {
	if r.ignoreCgroups && manifest != nil && manifest.GetCgroupPath() == "" && manifest.GetRuntimeCgroupPath() == "" {
		return contract.VerifiedCapability()
	}
	leaf := manifest.GetRuntimeCgroupPath()
	parent := manifest.GetCgroupPath()
	if r.ignoreCgroups || expected.GetCpuShares() == 0 || !validPersistentContainerID(id) || id == ".." ||
		!filepath.IsAbs(parent) || filepath.Clean(parent) != parent || parent == "/" ||
		leaf != filepath.Join(parent, cgroup.CgroupWorkloadLeafName) {
		return contract.LostCapability(errors.New("CPU enforcement requires the admitted cgroup and workload leaf"))
	}
	if err := r.validateRecoveryManifest(ctx, id, manifest); err != nil {
		return contract.InconclusiveCapability(err)
	}
	if err := hostlinux.VerifyCgroupCPUControls(leaf, expected); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return contract.InconclusiveCapability(err)
		}
		return contract.LostCapability(err)
	}
	state, err := r.state(ctx, id)
	if err != nil {
		return contract.InconclusiveCapability(err)
	}
	if state.Pid <= 0 || (state.Status != string(contract.ContainerStatusRunning) && state.Status != string(contract.ContainerStatusPaused)) {
		return contract.InconclusiveCapability(errors.New("runsc has no live Sentry for CPU verification"))
	}
	if err := hostlinux.VerifyRunscCgroupProcesses(leaf, state.Pid, r.common.Binary()); err != nil {
		return contract.InconclusiveCapability(err)
	}
	return contract.VerifiedCapability()
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
