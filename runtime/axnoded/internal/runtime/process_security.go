package runtime

import (
	"context"
	"fmt"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

// verifyManagedProcessPolicy is an activation fence, never a cleanup gate.
// Older authority remains available for confirmed termination and retirement.
func (r *RunscServiceHandler) verifyManagedProcessPolicy(ctx context.Context, id string, durable *apipb.AllocationEnforcementManifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validPersistentContainerID(id) || id == ".." {
		return fmt.Errorf("process security requires an exact allocation identity")
	}
	if durable.GetProcessSecurityProfile() != wire.ProcessSecurityProfile {
		return fmt.Errorf("allocation predates the managed-process security policy; stop and recreate its execution")
	}
	projected, err := r.AllocationEnforcementManifest(ctx, id)
	if err != nil {
		return fmt.Errorf("read managed-process enforcement policy: %w", err)
	}
	if err := verifyDurableEnforcementManifest(durable, projected); err != nil {
		return err
	}
	bundle, err := r.common.ContainerSpec(id)
	if err != nil {
		return fmt.Errorf("read managed-process spec: %w", err)
	}
	return runtimeoci.ValidateProcessSecurity(bundle.Process)
}
