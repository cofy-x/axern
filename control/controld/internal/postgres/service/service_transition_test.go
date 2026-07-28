package pgservice

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestApplyObservedStatusBatchMessagePrioritizesFailure(t *testing.T) {
	service := &servicev1.Service{Status: servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED}
	transitions := []*serviceStatusTransition{
		{
			allocation: &allocationRecord{AllocationID: "alloc-a"},
			nextStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message:    "liveness probe failed",
		},
		{
			allocation: &allocationRecord{AllocationID: "alloc-b"},
			nextStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			message:    "running",
		},
	}

	applyObservedStatusBatchMessage(service, transitions)
	if service.GetMessage() != "liveness probe failed" {
		t.Fatalf("service message = %q, want liveness failure", service.GetMessage())
	}
	cause := selectServiceStatusTransition(transitions, func(transition *serviceStatusTransition) bool {
		return transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED
	})
	if cause == nil || cause.allocation.AllocationID != "alloc-a" {
		t.Fatalf("degraded cause = %#v, want alloc-a", cause)
	}
}

func TestApplyObservedStatusBatchMessagePrioritizesReadiness(t *testing.T) {
	service := &servicev1.Service{Status: servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING}
	transitions := []*serviceStatusTransition{
		{
			allocation:       &allocationRecord{AllocationID: "alloc-a"},
			nextStatus:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			readinessMessage: "warming cache",
		},
		{
			allocation: &allocationRecord{AllocationID: "alloc-b"},
			nextStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING,
			message:    "starting",
		},
	}

	applyObservedStatusBatchMessage(service, transitions)
	if service.GetMessage() != "warming cache" {
		t.Fatalf("service message = %q, want readiness message", service.GetMessage())
	}
}
