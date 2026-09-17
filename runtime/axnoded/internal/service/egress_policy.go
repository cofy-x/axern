package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtimeegressv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/runtime/egress/v1"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Egress is owned by egressd, not by the OCI handler. Reconciliation must
// revalidate the durable allocation proof even when node observations
// change; an unavailable observation alone does not prove this policy lost.
func verifyActiveEgressPolicy(ctx context.Context, manager egress.Manager, allocationID, sandboxIP string, mode NetworkPolicyMode) contract.CapabilityVerification {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if manager == nil || strings.TrimSpace(sandboxIP) == "" {
		return contract.LostCapability(fmt.Errorf("egress manager or allocation network binding is unavailable"))
	}
	health, err := manager.Health(ctx)
	if err != nil {
		return contract.InconclusiveCapability(fmt.Errorf("read egress enforcement health: %w", err))
	}
	if !networkPolicyEnforcementHealthy(health, mode) {
		return contract.LostCapability(fmt.Errorf("egress enforcement is unhealthy"))
	}
	record, err := manager.Get(ctx, allocationID)
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
	applyNetworkPolicyRecord(&diagnostic, record, mode, allocationID, sandboxIP)
	if !diagnostic.ExactBinding {
		return contract.LostCapability(fmt.Errorf("egress policy differs from the allocation network binding"))
	}
	return contract.VerifiedCapability()
}

// verifyPreparedEgressPolicy checks egressd's authoritative record immediately
// before activation. Health alone is insufficient: Allocation ID, normalized
// policy, and allocated source IP must all match.
func (h *sandboxService) verifyPreparedEgressPolicy(ctx context.Context, request *runtime.StartRequest, allocationID string) error {
	policy := request.GetNetwork().GetEgressPolicy()
	if policy == nil {
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
	record, err := h.egressClient.Get(ctx, allocationID)
	if err != nil {
		return fmt.Errorf("read prepared egress policy: %w", err)
	}
	if record == nil || record.GetAllocationID() != allocationID ||
		strings.TrimSpace(record.GetSandboxIp()) != h.allocationController().ContainerIP(allocationID) ||
		!proto.Equal(record.GetPolicy(), policy) {
		return fmt.Errorf("prepared egress policy does not exactly match the allocation")
	}
	return nil
}
