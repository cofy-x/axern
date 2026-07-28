package app

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPostgresServiceReadinessRolloutWaitsForReady(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 12, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}},
		ReadinessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{
				Http: &servicev1.HttpProbe{Port: 8080, Path: "/ready"},
			},
			Period:           durationpb.New(5 * time.Second),
			Timeout:          durationpb.New(2 * time.Second),
			SuccessThreshold: 1,
			FailureThreshold: 1,
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	originalAllocationID := admitted.GetAllocationIds()[0]
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: originalAllocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(original ready) error = %v", err)
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before rollout) error = %v", err)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		Config:          &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"config"}},
	})
	if err != nil {
		t.Fatalf("UpdateService(config) error = %v", err)
	}
	replacementAllocationID := ""
	for _, allocationID := range updateResp.GetService().GetAllocationIds() {
		if allocationID != originalAllocationID {
			replacementAllocationID = allocationID
			break
		}
	}
	if replacementAllocationID == "" {
		t.Fatal("replacement allocation id not found after config update")
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:     replacementAllocationID,
			Attempt:          1,
			Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:            false,
			ReadinessMessage: "warming up",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement running not ready) error = %v", err)
	}

	app.reconcileV1()

	waitingResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(while waiting for ready) error = %v", err)
	}
	if waitingResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("status while waiting for ready = %v, want RECONCILING", waitingResp.GetService().GetStatus())
	}
	if waitingResp.GetService().GetRolloutStatus() == nil || waitingResp.GetService().GetRolloutStatus().GetPhase() != servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY {
		t.Fatalf("rollout status while waiting for ready = %+v, want WAITING_FOR_UPDATED_READY", waitingResp.GetService().GetRolloutStatus())
	}
	if len(waitingResp.GetService().GetAllocationIds()) != 2 {
		t.Fatalf("allocation_ids while waiting for ready = %#v, want old + replacement", waitingResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 0 {
		t.Fatalf("delete requests while waiting for ready = %#v, want none", lifecycle.DeleteRequests)
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replacementAllocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement ready) error = %v", err)
	}

	app.reconcileV1()

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after replacement ready) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status after replacement ready = %v, want READY", gotResp.GetService().GetStatus())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 || gotResp.GetService().GetAllocationIds()[0] != replacementAllocationID {
		t.Fatalf("allocation_ids after replacement ready = %#v, want only replacement %q", gotResp.GetService().GetAllocationIds(), replacementAllocationID)
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: createResp.GetService().GetID(),
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	var sawReplacementReady bool
	for _, event := range eventsResp.GetEvents() {
		if event.GetType() == servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_READY {
			sawReplacementReady = true
			break
		}
	}
	if !sawReplacementReady {
		t.Fatalf("service events missing replacement-ready marker: %#v", eventsResp.GetEvents())
	}
}
