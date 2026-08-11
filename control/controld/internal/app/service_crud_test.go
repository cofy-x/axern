package app

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestPostgresServiceCRUD(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	if _, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace: "default", EnvironmentID: env.GetID(), Replicas: -1,
	}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateService(negative replicas) code = %v, want InvalidArgument", grpcstatus.Code(err))
	}

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      2,
		Labels:        map[string]string{"tier": "api"},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceID := createResp.GetService().GetID()
	if serviceID == "" {
		t.Fatal("CreateService() returned empty service id")
	}
	if createResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("create service status = %v, want RECONCILING before replicas report RUNNING", createResp.GetService().GetStatus())
	}
	if createResp.GetService().GetReadyReplicas() != 0 || createResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("create service counters = ready:%d unhealthy:%d, want 0/0", createResp.GetService().GetReadyReplicas(), createResp.GetService().GetUnhealthyReplicas())
	}
	if createResp.GetService().GetRolloutPolicy().GetMaxSurge() != 1 || createResp.GetService().GetRolloutPolicy().GetMaxUnavailable() != 0 {
		t.Fatalf("default rollout policy = %+v, want max_surge=1 max_unavailable=0", createResp.GetService().GetRolloutPolicy())
	}
	if len(createResp.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("durable create allocation_ids = %#v, want reconciliation to admit replicas", createResp.GetService().GetAllocationIds())
	}
	admitted := reconcileCreatedService(t, app, serviceID, now)
	if len(admitted.GetAllocationIds()) != 2 {
		t.Fatalf("admitted allocation_ids = %#v, want 2 replicas", admitted.GetAllocationIds())
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("node lifecycle create requests = %d, want 2", len(lifecycle.CreateRequests))
	}
	initialAllocationIDs := append([]string(nil), admitted.GetAllocationIds()...)
	observations := make([]*nodev1.AllocationStatusObservation, 0, len(initialAllocationIDs))
	for _, allocationID := range initialAllocationIDs {
		observations = append(observations, &nodev1.AllocationStatusObservation{
			AllocationID: allocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		})
	}
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations:  observations,
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING) error = %v", err)
	}

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after RUNNING) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("service status after RUNNING = %v, want READY", gotResp.GetService().GetStatus())
	}
	if gotResp.GetService().GetReadyReplicas() != 2 || gotResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("service counters after RUNNING = ready:%d unhealthy:%d, want 2/0", gotResp.GetService().GetReadyReplicas(), gotResp.GetService().GetUnhealthyReplicas())
	}
	if gotResp.GetService().GetVersion() != admitted.GetVersion()+1 {
		t.Fatalf("service version after batch = %d, want %d", gotResp.GetService().GetVersion(), admitted.GetVersion()+1)
	}
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations:  observations,
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(retry) error = %v", err)
	}
	idempotentResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after status retry) error = %v", err)
	}
	if idempotentResp.GetService().GetVersion() != gotResp.GetService().GetVersion() {
		t.Fatalf("service version after identical retry = %d, want %d", idempotentResp.GetService().GetVersion(), gotResp.GetService().GetVersion())
	}

	listResp, err := public.ListServices(context.Background(), &servicev1.ListServicesRequest{
		Filter: &servicev1.ServiceListFilter{Namespace: "default", Labels: map[string]string{"tier": "api"}},
	})
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(listResp.GetServices()) != 1 || listResp.GetServices()[0].GetID() != serviceID {
		t.Fatalf("ListServices() returned %#v, want service %q", listResp.GetServices(), serviceID)
	}

	updateResp, err := public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       serviceID,
		ExpectedVersion: gotResp.GetService().GetVersion(),
		Replicas:        int32ptr(0),
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas"}},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	if updateResp.GetService().GetReplicas() != 0 {
		t.Fatalf("updated replicas = %d, want 0", updateResp.GetService().GetReplicas())
	}
	if updateResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("scale-to-zero status = %v, want READY", updateResp.GetService().GetStatus())
	}
	if updateResp.GetService().GetReadyReplicas() != 0 || updateResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("scale-to-zero counters = ready:%d unhealthy:%d, want 0/0", updateResp.GetService().GetReadyReplicas(), updateResp.GetService().GetUnhealthyReplicas())
	}
	if len(updateResp.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("updated allocation_ids = %#v, want empty after scale-to-zero", updateResp.GetService().GetAllocationIds())
	}
	if len(lifecycle.DeleteRequests) != 2 {
		t.Fatalf("node lifecycle delete requests after scale-to-zero = %d, want 2", len(lifecycle.DeleteRequests))
	}

	updateResp, err = public.UpdateService(context.Background(), &servicev1.UpdateServiceRequest{
		ServiceID:       serviceID,
		ExpectedVersion: updateResp.GetService().GetVersion(),
		Replicas:        int32ptr(1),
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas"}},
	})
	if err != nil {
		t.Fatalf("UpdateService(scale-up) error = %v", err)
	}
	reconcileAllocationLifecycle(t, app, now)
	if updateResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		t.Fatalf("scaled-up status = %v, want RECONCILING before RUNNING report", updateResp.GetService().GetStatus())
	}
	if updateResp.GetService().GetReadyReplicas() != 0 || updateResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("scaled-up counters = ready:%d unhealthy:%d, want 0/0", updateResp.GetService().GetReadyReplicas(), updateResp.GetService().GetUnhealthyReplicas())
	}
	if len(updateResp.GetService().GetAllocationIds()) != 1 {
		t.Fatalf("scaled-up allocation_ids = %#v, want 1", updateResp.GetService().GetAllocationIds())
	}

	deleteResp, err := public.DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if deleteResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("delete request status = %v, want DELETING", deleteResp.GetService().GetStatus())
	}
	reconcileDeletedServiceAllocations(t, app, now)
	deletedResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService(after delete reconciliation) error = %v", err)
	}
	if deletedResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("reconciled delete status = %v, want DELETED", deletedResp.GetService().GetStatus())
	}
	staleStatus, err := app.servicePG.UpdateStatus(
		context.Background(),
		serviceID,
		servicev1.ServiceStatus_SERVICE_STATUS_READY,
		"stale reconcile",
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("UpdateStatus(after delete) error = %v", err)
	}
	if staleStatus.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("stale status update resurrected service as %v", staleStatus.GetStatus())
	}
	if len(lifecycle.DeleteRequests) != 3 {
		t.Fatalf("node lifecycle delete requests after delete = %d, want 3", len(lifecycle.DeleteRequests))
	}

	purgeResp, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"})
	if err != nil {
		t.Fatalf("PurgeService() error = %v", err)
	}
	if purgeResp.GetServiceID() != serviceID {
		t.Fatalf("purged service id = %q, want %q", purgeResp.GetServiceID(), serviceID)
	}
	if _, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID}); err == nil {
		t.Fatal("GetService() unexpectedly succeeded after purge")
	}
	listAfterPurge, err := public.ListServices(context.Background(), &servicev1.ListServicesRequest{
		Filter: &servicev1.ServiceListFilter{Namespace: "default", Labels: map[string]string{"tier": "api"}},
	})
	if err != nil {
		t.Fatalf("ListServices(after purge) error = %v", err)
	}
	if len(listAfterPurge.GetServices()) != 0 {
		t.Fatalf("ListServices(after purge) = %#v, want empty", listAfterPurge.GetServices())
	}

	var allocationCount int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocations WHERE owner_type = $1 AND owner_id = $2
	`, allocationkernel.OwnerService, serviceID).Scan(&allocationCount); err != nil {
		t.Fatalf("count service allocations after purge: %v", err)
	}
	if allocationCount != 0 {
		t.Fatalf("service allocations after purge = %d, want 0", allocationCount)
	}

	var eventCount int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM service_events WHERE service_id = $1
	`, serviceID).Scan(&eventCount); err != nil {
		t.Fatalf("count service events after purge: %v", err)
	}
	if eventCount != 0 {
		t.Fatalf("service events after purge = %d, want 0", eventCount)
	}
}
