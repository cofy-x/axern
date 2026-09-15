package allocationkernel

import (
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func ParseLifecycleState(value string) commonv1.AllocationLifecycleState {
	if n, ok := commonv1.AllocationLifecycleState_value[value]; ok {
		return commonv1.AllocationLifecycleState(n)
	}
	return commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_UNSPECIFIED
}

func IsCleanupState(state commonv1.AllocationLifecycleState) bool {
	return state == commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING ||
		state == commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED
}

func RunStatusFromObservation(state commonv1.AllocationLifecycleState, exitCode *int32, diagnosticCode commonv1.WorkloadDiagnosticCode) runv1.RunStatus {
	switch state {
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND:
		return runv1.RunStatus_RUN_STATUS_PLACED
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING:
		return runv1.RunStatus_RUN_STATUS_STARTING
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE:
		return runv1.RunStatus_RUN_STATUS_RUNNING
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED:
		if diagnosticCode == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED && exitCode != nil && *exitCode == 0 {
			return runv1.RunStatus_RUN_STATUS_SUCCEEDED
		}
		return runv1.RunStatus_RUN_STATUS_FAILED
	default:
		return runv1.RunStatus_RUN_STATUS_UNSPECIFIED
	}
}
