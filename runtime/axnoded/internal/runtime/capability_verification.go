package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func (r *RuncServiceHandler) VerifyAllocationCapability(ctx context.Context, dependency *capabilityv1.CapabilityDependency, options contract.HandlerOptions) contract.CapabilityVerification {
	switch dependency.GetKey().GetPlatform() {
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT:
		if err := r.verifyMemoryEnforcement(ctx, options); err != nil {
			return classifyCapabilityVerificationError(err)
		}
		return contract.VerifiedCapability()
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT:
		if r.writableCapacity == nil {
			return contract.LostCapability(fmt.Errorf("runc writable capacity manager is unavailable"))
		}
		if err := rootfsview.VerifyPersistentView(filepath.Dir(r.writableCapacity.dir), options.ContainerID, r.Name()); err != nil {
			return classifyCapabilityVerificationError(err)
		}
		return contract.VerifiedCapability()
	default:
		return contract.LostCapability(fmt.Errorf("runc has no allocation verifier for %s", dependency.GetKey().GetPlatform()))
	}
}

func (r *RunscServiceHandler) VerifyAllocationCapability(ctx context.Context, dependency *capabilityv1.CapabilityDependency, options contract.HandlerOptions) contract.CapabilityVerification {
	switch dependency.GetKey().GetPlatform() {
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT:
		if err := r.verifyMemoryEnforcement(ctx, options); err != nil {
			return classifyCapabilityVerificationError(err)
		}
		return contract.VerifiedCapability()
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT:
		if _, err := r.state(ctx, options.ContainerID); err != nil {
			return contract.InconclusiveCapability(fmt.Errorf("read runsc state for ephemeral enforcement: %w", err))
		}
		facts, err := hostlinux.ReadFilestoreCapabilities(r.filestoreDir)
		if err != nil {
			return contract.InconclusiveCapability(err)
		}
		if expected := dependencyMountIdentity(dependency); expected != "" && facts.MountIdentity != expected {
			return contract.LostCapability(fmt.Errorf("runsc filestore mount identity changed: expected=%s current=%s", expected, facts.MountIdentity))
		}
		if _, err := os.Stat(filepath.Join(r.filestoreDir, "runsc")); err != nil {
			wrapped := fmt.Errorf("stat runsc overlay backing directory: %w", err)
			if os.IsNotExist(err) {
				return contract.LostCapability(wrapped)
			}
			return contract.InconclusiveCapability(wrapped)
		}
		return contract.VerifiedCapability()
	default:
		return contract.LostCapability(fmt.Errorf("runsc has no allocation verifier for %s", dependency.GetKey().GetPlatform()))
	}
}

func classifyCapabilityVerificationError(err error) contract.CapabilityVerification {
	if err == nil {
		return contract.VerifiedCapability()
	}
	if os.IsNotExist(err) {
		return contract.LostCapability(err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return contract.InconclusiveCapability(err)
	}
	return contract.LostCapability(err)
}

func dependencyMountIdentity(dependency *capabilityv1.CapabilityDependency) string {
	for _, reference := range dependency.GetDependencyEvidence() {
		if identity := reference.GetEvidence().GetMountIdentity(); identity != "" {
			return identity
		}
	}
	return ""
}
