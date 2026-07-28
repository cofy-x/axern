package parse

import (
	"fmt"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

const validAllocationStatuses = "reserved, bound, starting, running, exited, failed, releasing, released"

func AllocationStatuses(values []string) ([]commonv1.AllocationStatus, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]commonv1.AllocationStatus, 0, len(values))
	for _, value := range splitList(values) {
		switch normalizeToken(value) {
		case "reserved":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED)
		case "bound":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND)
		case "starting":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING)
		case "running":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING)
		case "exited":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED)
		case "failed":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED)
		case "releasing":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING)
		case "released":
			out = append(out, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED)
		default:
			return nil, fmt.Errorf("invalid allocation status %q, want one of: %s", value, validAllocationStatuses)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
