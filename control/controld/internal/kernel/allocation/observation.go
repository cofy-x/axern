package allocationkernel

import (
	"fmt"
	"strings"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

func ValidateObservationBinding(currentNodeID, reportingNodeID string) error {
	currentNodeID = strings.TrimSpace(currentNodeID)
	reportingNodeID = strings.TrimSpace(reportingNodeID)
	if currentNodeID == "" || reportingNodeID == "" || currentNodeID != reportingNodeID {
		return fmt.Errorf("allocation is bound to node %q, not reporting node %q", currentNodeID, reportingNodeID)
	}
	return nil
}

func AcceptsObservation(currentState commonv1.AllocationLifecycleState, currentNodeID, reportingNodeID string, obs *nodev1.AllocationLifecycleObservation) bool {
	if obs == nil {
		return false
	}
	if ValidateObservationBinding(currentNodeID, reportingNodeID) != nil {
		return false
	}
	switch obs.GetState() {
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED:
	default:
		return false
	}
	switch currentState {
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING:
		return true
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE:
		return obs.GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING
	default:
		return false
	}
}

func NodeActiveObservationTime(obs *nodev1.AllocationLifecycleObservation, fallback time.Time) time.Time {
	if obs == nil || obs.GetObservedAt() == nil {
		return fallback.UTC()
	}
	observedAt := obs.GetObservedAt().AsTime()
	if observedAt.IsZero() {
		return fallback.UTC()
	}
	return observedAt.UTC()
}
