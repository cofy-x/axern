package app

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPostgresServiceReplicaObservability(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceID := createResp.GetService().GetID()
	admitted := reconcileCreatedService(t, app, serviceID, now)
	firstReplicaID := admitted.GetAllocationIds()[0]

	listResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(create) error = %v", err)
	}
	if len(listResp.GetReplicas()) != 1 {
		t.Fatalf("replicas after create = %d, want 1", len(listResp.GetReplicas()))
	}
	if listResp.GetReplicas()[0].GetID() != firstReplicaID {
		t.Fatalf("replica id after create = %q, want %q", listResp.GetReplicas()[0].GetID(), firstReplicaID)
	}
	if listResp.GetReplicas()[0].GetEnded() {
		t.Fatal("replica after create unexpectedly marked ended")
	}
	if listResp.GetReplicas()[0].GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND {
		t.Fatalf("replica status after create = %v, want BOUND", listResp.GetReplicas()[0].GetStatus())
	}

	getReplicaResp, err := public.GetServiceReplica(context.Background(), &servicev1.GetServiceReplicaRequest{
		ServiceID: serviceID,
		ReplicaID: firstReplicaID,
	})
	if err != nil {
		t.Fatalf("GetServiceReplica() error = %v", err)
	}
	if getReplicaResp.GetReplica().GetID() != firstReplicaID {
		t.Fatalf("GetServiceReplica() id = %q, want %q", getReplicaResp.GetReplica().GetID(), firstReplicaID)
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: firstReplicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING) error = %v", err)
	}

	getServiceResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after RUNNING) error = %v", err)
	}
	if getServiceResp.GetService().GetReadyReplicas() != 1 {
		t.Fatalf("ready_replicas after RUNNING = %d, want 1", getServiceResp.GetService().GetReadyReplicas())
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:  firstReplicaID,
			Attempt:       1,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
			ExitCode:      17,
			ExitCodeKnown: true,
			Message:       "process exited",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(EXITED) error = %v", err)
	}

	app.reconcileV1()

	listResp, err = public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(after reconcile) error = %v", err)
	}
	if len(listResp.GetReplicas()) != 2 {
		t.Fatalf("replicas after reconcile = %d, want current replacement + ended replica", len(listResp.GetReplicas()))
	}
	var replacementID string
	var sawEnded bool
	for _, replica := range listResp.GetReplicas() {
		if replica.GetID() == firstReplicaID {
			sawEnded = true
			if !replica.GetEnded() || replica.GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
				t.Fatalf("ended replica = %+v, want EXITED ended", replica)
			}
			continue
		}
		replacementID = replica.GetID()
		if replica.GetEnded() {
			t.Fatalf("replacement replica unexpectedly ended: %+v", replica)
		}
	}
	if !sawEnded || replacementID == "" {
		t.Fatalf("after reconcile sawEnded=%v replacementID=%q, want both set", sawEnded, replacementID)
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: replacementID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement RUNNING) error = %v", err)
	}

	getServiceResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after replacement RUNNING) error = %v", err)
	}
	if getServiceResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("service status after replacement RUNNING = %v, want READY", getServiceResp.GetService().GetStatus())
	}
	if getServiceResp.GetService().GetReadyReplicas() != 1 {
		t.Fatalf("ready_replicas after replacement RUNNING = %d, want 1", getServiceResp.GetService().GetReadyReplicas())
	}

	listResp, err = public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(after recovery) error = %v", err)
	}
	if len(listResp.GetReplicas()) != 2 {
		t.Fatalf("replicas after recovery = %d, want replacement + recent ended", len(listResp.GetReplicas()))
	}

	currentOnly, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(current) error = %v", err)
	}
	if len(currentOnly.GetReplicas()) != 1 || currentOnly.GetReplicas()[0].GetID() != replacementID {
		t.Fatalf("current replicas = %#v, want only replacement %q", currentOnly.GetReplicas(), replacementID)
	}

	endedOnly, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ENDED},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(ended) error = %v", err)
	}
	if len(endedOnly.GetReplicas()) != 1 || endedOnly.GetReplicas()[0].GetID() != firstReplicaID {
		t.Fatalf("ended replicas = %#v, want only exited replica %q", endedOnly.GetReplicas(), firstReplicaID)
	}

	unhealthyOnly, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: serviceID,
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNHEALTHY},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(unhealthy) error = %v", err)
	}
	if len(unhealthyOnly.GetReplicas()) != 1 || unhealthyOnly.GetReplicas()[0].GetID() != firstReplicaID {
		t.Fatalf("unhealthy replicas = %#v, want only exited replica %q", unhealthyOnly.GetReplicas(), firstReplicaID)
	}

	eventsResp, err := public.ListServiceEvents(context.Background(), &servicev1.ListServiceEventsRequest{
		ServiceID: serviceID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListServiceEvents() error = %v", err)
	}
	var sawReplacementRunning, sawRecovered bool
	for _, event := range eventsResp.GetEvents() {
		switch event.GetType() {
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_RUNNING:
			sawReplacementRunning = true
		case servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_RECOVERED:
			sawRecovered = true
		}
	}
	if !sawReplacementRunning || !sawRecovered {
		t.Fatalf("service events missing replacement running/recovered markers: %#v", eventsResp.GetEvents())
	}
}

func TestPostgresServiceReplicaRolloutViews(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 15, 0, 0, 0, time.UTC)
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
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	oldReplicaID := admitted.GetAllocationIds()[0]
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: oldReplicaID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(old RUNNING) error = %v", err)
	}
	currentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(before rollout) error = %v", err)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       createResp.GetService().GetID(),
		ExpectedVersion: currentResp.GetService().GetVersion(),
		Config:          &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		UpdateMask:      updateMask("config"),
	})
	if err != nil {
		t.Fatalf("UpdateService(config) error = %v", err)
	}
	if updateResp.GetService().GetRolloutStatus() == nil || !updateResp.GetService().GetRolloutStatus().GetInProgress() {
		t.Fatalf("rollout status after update = %+v, want in_progress", updateResp.GetService().GetRolloutStatus())
	}

	outdatedResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_OUTDATED},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(outdated) error = %v", err)
	}
	if len(outdatedResp.GetReplicas()) != 1 || outdatedResp.GetReplicas()[0].GetID() != oldReplicaID || !outdatedResp.GetReplicas()[0].GetOutdated() {
		t.Fatalf("outdated replicas = %#v, want old replica %q marked outdated", outdatedResp.GetReplicas(), oldReplicaID)
	}

	updatedResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
		Filter:    &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UPDATED},
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(updated) error = %v", err)
	}
	if len(updatedResp.GetReplicas()) != 1 || updatedResp.GetReplicas()[0].GetOutdated() {
		t.Fatalf("updated replicas = %#v, want only replacement and not outdated", updatedResp.GetReplicas())
	}
}

func TestGetServiceReplicaRejectsCrossServiceLookup(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 25, 11, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)

	first, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService(first) error = %v", err)
	}
	firstAdmitted := reconcileCreatedService(t, app, first.GetService().GetID(), now)
	second, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService(second) error = %v", err)
	}

	_, err = public.GetServiceReplica(context.Background(), &servicev1.GetServiceReplicaRequest{
		ServiceID: second.GetService().GetID(),
		ReplicaID: firstAdmitted.GetAllocationIds()[0],
	})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetServiceReplica(cross-service) code = %v, want %v", grpcstatus.Code(err), codes.NotFound)
	}
}
