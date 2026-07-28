package pgservice

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestBuildAllocationStatusReportsMarksInitialServiceReadiness(t *testing.T) {
	createdAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	now := createdAt.Add(3 * time.Second)
	current := &servicev1.Service{
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		Replicas:      2,
		ReadyReplicas: 1,
		CreatedAt:     timestamppb.New(createdAt),
	}
	next := &servicev1.Service{
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Replicas:      2,
		ReadyReplicas: 2,
	}
	reports := buildAllocationStatusReports(current, next, []*serviceStatusTransition{{
		allocation:   &allocationRecord{CreatedAt: createdAt},
		currentReady: false,
		nextStatus:   commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		nextReady:    true,
	}}, now)
	if len(reports) != 1 || !reports[0].ServiceBecameReady {
		t.Fatalf("reports = %+v, want one initial service-ready report", reports)
	}
}

func TestBuildAllocationStatusReportsDoesNotCountRecoveryAsInitialReadiness(t *testing.T) {
	createdAt := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	current := &servicev1.Service{
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Replicas:      2,
		ReadyReplicas: 1,
		CreatedAt:     timestamppb.New(createdAt),
	}
	next := &servicev1.Service{
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Replicas:      2,
		ReadyReplicas: 2,
	}
	reports := buildAllocationStatusReports(current, next, []*serviceStatusTransition{{
		allocation:   &allocationRecord{CreatedAt: createdAt},
		currentReady: false,
		nextStatus:   commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		nextReady:    true,
	}}, createdAt.Add(time.Minute))
	if len(reports) != 1 {
		t.Fatalf("reports = %+v, want one replica-ready report", reports)
	}
	if reports[0].ServiceBecameReady {
		t.Fatal("recovery report must not be counted as initial service readiness")
	}
}
