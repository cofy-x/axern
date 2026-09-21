package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

// inconclusiveCapabilityError marks a probe transport/state-read failure. It
// is distinct from a successfully read kernel/runtime state that proves the
// configured boundary is gone. Runtime reconciliation retries inconclusive
// results at 0/2/5 seconds before applying fail-stop.
type inconclusiveCapabilityError struct{ err error }

func (e *inconclusiveCapabilityError) Error() string { return e.err.Error() }
func (e *inconclusiveCapabilityError) Unwrap() error { return e.err }

func inconclusiveCapabilityErrorf(format string, args ...any) error {
	return &inconclusiveCapabilityError{err: fmt.Errorf(format, args...)}
}

func (r *RunscServiceHandler) VerifyAllocationCapability(ctx context.Context, dependency *capabilityv1.CapabilityRequirement, options contract.HandlerOptions) contract.CapabilityVerification {
	switch dependency.GetKey().GetPlatform() {
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT:
		if options.MemoryLimitBytes <= 0 {
			return contract.LostCapability(fmt.Errorf("runsc memory capability requires a positive enforced limit"))
		}
		if err := r.verifyMemoryEnforcement(ctx, options); err != nil {
			return classifyCapabilityVerificationError(err)
		}
		return contract.VerifiedCapability()
	case capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT:
		state, err := r.state(ctx, options.ContainerID)
		if err != nil {
			return contract.InconclusiveCapability(fmt.Errorf("read runsc state for ephemeral enforcement: %w", err))
		}
		if state.Pid <= 0 || (state.Status != "created" && state.Status != "running") {
			return contract.LostCapability(fmt.Errorf("runsc runtime process is neither created nor running"))
		}
		manifest, err := r.AllocationEnforcementManifest(ctx, options.ContainerID)
		if err != nil {
			return classifyCapabilityVerificationError(fmt.Errorf("read immutable runsc enforcement manifest: %w", err))
		}
		if err := verifyDurableEnforcementManifest(options.EnforcementManifest, manifest); err != nil {
			return contract.LostCapability(err)
		}
		facts, err := hostlinux.ReadFilestoreCapabilities(r.filestoreDir)
		if err != nil {
			return contract.InconclusiveCapability(err)
		}
		if manifest.GetFilestoreMountIdentity() == "" || manifest.GetFilestoreMountIdentity() != facts.MountIdentity {
			return contract.LostCapability(fmt.Errorf("runsc immutable launch filestore identity changed"))
		}
		if options.EphemeralStorageLimitBytes <= 0 || manifest.GetEphemeralStorageLimitBytes() != options.EphemeralStorageLimitBytes {
			return contract.LostCapability(fmt.Errorf("runsc ephemeral limit does not match durable allocation manifest"))
		}
		expectedOverlay, err := r.overlay2Value(false, options.EphemeralStorageLimitBytes)
		if err != nil {
			return contract.LostCapability(fmt.Errorf("derive expected runsc overlay argument: %w", err))
		}
		if manifest.GetRunscOverlayArg() != expectedOverlay {
			return contract.LostCapability(fmt.Errorf("runsc immutable overlay argument changed"))
		}
		if err := hostlinux.VerifyProcessExecutable(state.Pid, hostlinux.RunscSentryBinary(r.common.Binary())); err != nil {
			return classifyCapabilityVerificationError(err)
		}
		expectedBackingDirectory := filepath.Join(r.filestoreDir, "runsc")
		if manifest.GetRunscBackingDirectory() != expectedBackingDirectory {
			return contract.LostCapability(fmt.Errorf("runsc overlay backing directory changed"))
		}
		backingIdentity, err := hostlinux.DirectoryIdentity(expectedBackingDirectory)
		if err != nil {
			wrapped := fmt.Errorf("read runsc overlay backing directory identity: %w", err)
			if errors.Is(err, os.ErrNotExist) {
				return contract.LostCapability(wrapped)
			}
			return contract.InconclusiveCapability(wrapped)
		}
		if backingIdentity != manifest.GetRunscBackingDirectoryIdentity() {
			return contract.LostCapability(fmt.Errorf("runsc overlay backing directory identity changed"))
		}
		return contract.VerifiedCapability()
	default:
		return contract.LostCapability(fmt.Errorf("runsc has no allocation verifier for %s", dependency.GetKey().GetPlatform()))
	}
}

func verifyDurableEnforcementManifest(durable, runtime *apipb.AllocationEnforcementManifest) error {
	if durable == nil {
		return fmt.Errorf("durable allocation enforcement manifest is unavailable")
	}
	if runtime == nil || !proto.Equal(durable, runtime) {
		return fmt.Errorf("runtime enforcement manifest differs from durable allocation manifest")
	}
	return nil
}

func classifyCapabilityVerificationError(err error) contract.CapabilityVerification {
	if err == nil {
		return contract.VerifiedCapability()
	}
	var inconclusive *inconclusiveCapabilityError
	if errors.As(err, &inconclusive) {
		return contract.InconclusiveCapability(err)
	}
	if errors.Is(err, os.ErrNotExist) {
		return contract.LostCapability(err)
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return contract.InconclusiveCapability(err)
	}
	return contract.LostCapability(err)
}
