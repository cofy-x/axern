package allocationkernel

import (
	"fmt"
	"strings"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

type LifecycleRetryClearanceInput struct {
	AllocationID                     string
	AllocationStatus                 string
	OwnerType                        string
	OwnerRunStatus                   string
	OwnerServiceReferencesAllocation bool
	HasActiveReservation             bool
	HasActiveLease                   bool
	HasActiveTunnelSession           bool
}

type LifecycleRetryClearance struct {
	Clearable     bool
	BlockedReason string
}

func EvaluateLifecycleRetryClearance(in LifecycleRetryClearanceInput) LifecycleRetryClearance {
	allocationStatus := strings.TrimSpace(in.AllocationStatus)
	if !IsEnded(ParseStatus(allocationStatus)) {
		return blockedLifecycleRetryClearance("allocation status is %s", statusOrUnknown(allocationStatus))
	}
	if in.HasActiveReservation {
		return blockedLifecycleRetryClearance("active reservations")
	}
	if in.HasActiveLease {
		return blockedLifecycleRetryClearance("active leases")
	}
	if in.HasActiveTunnelSession {
		return blockedLifecycleRetryClearance("active tunnel sessions")
	}
	switch strings.TrimSpace(in.OwnerType) {
	case OwnerRun:
		return evaluateRunLifecycleRetryClearance(in)
	case OwnerService:
		if in.OwnerServiceReferencesAllocation {
			return blockedLifecycleRetryClearance("owner service still references allocation")
		}
		return LifecycleRetryClearance{Clearable: true}
	default:
		return blockedLifecycleRetryClearance("unsupported owner type %s", statusOrUnknown(in.OwnerType))
	}
}

func evaluateRunLifecycleRetryClearance(in LifecycleRetryClearanceInput) LifecycleRetryClearance {
	status := strings.TrimSpace(in.OwnerRunStatus)
	if status == "" {
		return LifecycleRetryClearance{Clearable: true}
	}
	if isTerminalRunStatus(status) {
		return LifecycleRetryClearance{Clearable: true}
	}
	return blockedLifecycleRetryClearance("owner run status is %s", statusOrUnknown(status))
}

func isTerminalRunStatus(value string) bool {
	switch value {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(),
		runv1.RunStatus_RUN_STATUS_FAILED.String(),
		runv1.RunStatus_RUN_STATUS_CANCELLED.String():
		return true
	default:
		return false
	}
}

func blockedLifecycleRetryClearance(format string, args ...any) LifecycleRetryClearance {
	return LifecycleRetryClearance{BlockedReason: fmt.Sprintf(format, args...)}
}

func statusOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
