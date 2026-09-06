package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Egress is owned by egressd, not by the OCI handler. Reconciliation must
// revalidate the durable attempt-specific proof even when node observations
// change; an unavailable observation alone does not prove this policy lost.
func verifyActiveEgressPolicy(ctx context.Context, manager egress.Manager, allocationID string, manifest allocation.EgressPolicyManifest, mode NetworkPolicyMode) contract.CapabilityVerification {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if manager == nil || manifest.Proof == nil {
		return contract.LostCapability(fmt.Errorf("egress manager or durable proof is unavailable"))
	}
	health, err := manager.Health(ctx)
	if err != nil {
		return contract.InconclusiveCapability(fmt.Errorf("read egress enforcement health: %w", err))
	}
	if !networkPolicyEnforcementHealthy(health, mode) {
		return contract.LostCapability(fmt.Errorf("egress enforcement is unhealthy"))
	}
	record, err := manager.Get(ctx, allocationID, manifest.Attempt)
	if err != nil {
		if status.Code(err) == codes.NotFound || status.Code(err) == codes.FailedPrecondition {
			return contract.LostCapability(fmt.Errorf("exact egress policy is absent or fenced"))
		}
		return contract.InconclusiveCapability(fmt.Errorf("read exact egress policy: %w", err))
	}
	if record == nil {
		return contract.LostCapability(fmt.Errorf("exact egress policy is absent"))
	}
	var diagnostic NetworkPolicyDiagnostics
	applyNetworkPolicyRecord(&diagnostic, record, manifest, mode, allocationID)
	if !diagnostic.ExactProof {
		return contract.LostCapability(fmt.Errorf("egress policy differs from durable allocation proof"))
	}
	return contract.VerifiedCapability()
}

// verifyPreparedEgressPolicy is the exact pre-activation proof. Capability
// health alone is insufficient: the record must bind this allocation attempt,
// current execution revision, normalized policy, and allocated source IP.
func (h *sandboxService) verifyPreparedEgressPolicy(ctx context.Context, request *runtime.StartRequest, allocationID string) error {
	if request.GetEgressPolicy() == nil {
		return fmt.Errorf("egress capability is required without a policy contract")
	}
	if h.egressClient == nil {
		return fmt.Errorf("egressd client is unavailable")
	}
	health, err := h.egressClient.Health(ctx)
	if err != nil {
		return fmt.Errorf("verify egressd health: %w", err)
	}
	if health == nil || health.GetStatus() != runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK {
		return fmt.Errorf("egressd enforcement is not healthy")
	}
	record, err := h.egressClient.Get(ctx, allocationID, request.GetAllocationAttempt())
	if err != nil {
		return fmt.Errorf("read prepared egress policy: %w", err)
	}
	revision := int64(1)
	if conditions := h.allocationController().CapabilityConditions(allocationID); conditions != nil && conditions.GetRevision() > 0 {
		revision = conditions.GetRevision()
	}
	if record == nil || record.GetAllocationID() != allocationID || record.GetAttempt() != request.GetAllocationAttempt() ||
		strings.TrimSpace(record.GetSandboxIp()) != h.allocationController().ContainerIP(allocationID) ||
		record.GetExecutionRevision() != revision || !proto.Equal(record.GetPolicy(), request.GetEgressPolicy()) {
		return fmt.Errorf("prepared egress policy does not exactly match the allocation")
	}
	return nil
}
