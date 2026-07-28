package allocationkernel

import (
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func AcceptsObservation(currentStatus commonv1.AllocationStatus, currentAttempt int64, currentNodeID, reportingNodeID string, obs *nodev1.AllocationStatusObservation) bool {
	if obs == nil {
		return false
	}
	if currentAttempt != obs.GetAttempt() {
		return false
	}
	if reportingNodeID != "" && reportingNodeID != currentNodeID {
		return false
	}
	if obs.GetStatus() == commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED {
		return false
	}
	if IsEnded(currentStatus) || currentStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING {
		return false
	}
	return true
}

func NodeActiveObservationTime(obs *nodev1.AllocationStatusObservation, fallback time.Time) time.Time {
	if obs == nil || obs.GetObservedAt() == nil {
		return fallback.UTC()
	}
	observedAt := obs.GetObservedAt().AsTime()
	if observedAt.IsZero() {
		return fallback.UTC()
	}
	return observedAt.UTC()
}
