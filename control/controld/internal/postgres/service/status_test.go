package pgservice

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestApplyRolloutReconciliationKeepsIncompleteRolloutReconciling(t *testing.T) {
	service := &servicev1.Service{Status: servicev1.ServiceStatus_SERVICE_STATUS_READY}
	rollout := &servicev1.ServiceRolloutStatus{InProgress: true}

	applyRolloutReconciliation(service, rollout)

	if service.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("service status = %v, want RECONCILING", service.GetStatus())
	}
	if service.GetRolloutStatus() != rollout {
		t.Fatal("rollout status was not attached to service")
	}
}

func TestServiceStatusBatchNeedsReconcileOnlyForActionableTransitions(t *testing.T) {
	tests := []struct {
		name        string
		result      *serviceObservationResult
		transitions []*serviceStatusTransition
		want        bool
	}{
		{
			name:        "initial replica ready is fully projected",
			result:      &serviceObservationResult{},
			transitions: []*serviceStatusTransition{{nextStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, nextReady: true}},
		},
		{
			name:        "ended allocation needs replacement",
			result:      &serviceObservationResult{},
			transitions: []*serviceStatusTransition{{nextStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED}},
			want:        true,
		},
		{
			name: "rollout waiting needs another status",
			result: &serviceObservationResult{rollout: &servicev1.ServiceRolloutStatus{
				InProgress: true,
				Phase:      servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY,
			}},
		},
		{
			name: "rollout draining has controller work",
			result: &serviceObservationResult{rollout: &servicev1.ServiceRolloutStatus{
				InProgress: true,
				Phase:      servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_DRAINING_OUTDATED,
			}},
			want: true,
		},
		{
			name: "rollout admitting has controller work",
			result: &serviceObservationResult{rollout: &servicev1.ServiceRolloutStatus{
				InProgress: true,
				Phase:      servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_ADMITTING_REPLACEMENT,
			}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceStatusBatchNeedsReconcile(tt.result, tt.transitions); got != tt.want {
				t.Fatalf("serviceStatusBatchNeedsReconcile() = %t, want %t", got, tt.want)
			}
		})
	}
}
