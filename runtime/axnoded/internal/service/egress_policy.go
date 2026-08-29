package service

import (
	"context"
	"fmt"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/protobuf/proto"
)

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
