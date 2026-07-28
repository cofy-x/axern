package allocationkernel

import (
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func ParseStatus(value string) commonv1.AllocationStatus {
	if n, ok := commonv1.AllocationStatus_value[value]; ok {
		return commonv1.AllocationStatus(n)
	}
	return commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED
}

func IsEnded(status commonv1.AllocationStatus) bool {
	return status == commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED ||
		status == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED ||
		status == commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED
}

func RunStatusFromAllocation(status commonv1.AllocationStatus, exitCode int32) runv1.RunStatus {
	switch status {
	case commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED, commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND:
		return runv1.RunStatus_RUN_STATUS_PLACED
	case commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING:
		return runv1.RunStatus_RUN_STATUS_STARTING
	case commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING:
		return runv1.RunStatus_RUN_STATUS_RUNNING
	case commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED:
		if exitCode == 0 {
			return runv1.RunStatus_RUN_STATUS_SUCCEEDED
		}
		return runv1.RunStatus_RUN_STATUS_FAILED
	case commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED:
		return runv1.RunStatus_RUN_STATUS_FAILED
	default:
		return runv1.RunStatus_RUN_STATUS_UNSPECIFIED
	}
}
