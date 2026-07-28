package pgrun

import (
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func parseRunStatus(value string) runv1.RunStatus {
	if n, ok := runv1.RunStatus_value[value]; ok {
		return runv1.RunStatus(n)
	}
	return runv1.RunStatus_RUN_STATUS_UNSPECIFIED
}

func parseAllocationStatus(value string) commonv1.AllocationStatus {
	return allocationkernel.ParseStatus(value)
}
