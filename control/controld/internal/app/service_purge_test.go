package app

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPostgresServicePurgeRequiresDeletedService(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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
	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: createResp.GetService().GetID(), OperatorReason: "test cleanup"}); err == nil {
		t.Fatal("PurgeService() unexpectedly succeeded for non-deleted service")
	}
}

func TestPostgresServicePurgeRequiresReleasedAllocations(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 12, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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
	allocationID := admitted.GetAllocationIds()[0]

	lifecycle.KeepDeletedVisible = true
	if _, err := public.DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID}); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE allocations
		SET status = $2
		WHERE allocation_id = $1
	`, allocationID, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING.String()); err != nil {
		t.Fatalf("force allocation releasing state: %v", err)
	}

	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"}); err == nil {
		t.Fatal("PurgeService() unexpectedly succeeded with releasing allocation")
	} else if got := grpcstatus.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("PurgeService() code = %v, want FailedPrecondition", got)
	}
}

func TestPostgresDeletedServiceReconcileReleasesEndedAllocationsBeforePurge(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 12, 45, 0, 0, time.UTC)
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
	allocationID := admitted.GetAllocationIds()[0]

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:  allocationID,
			Attempt:       1,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			Message:       "allocation missing from node inventory",
			ExitCodeKnown: false,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(FAILED) error = %v", err)
	}
	if _, err := public.DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID}); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	reconcileDeletedServiceAllocations(t, app, now)
	deleteRequests := len(lifecycle.DeleteRequests)
	if deleteRequests == 0 {
		t.Fatal("deleted service reconciliation did not call node lifecycle delete")
	}
	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"}); err != nil {
		t.Fatalf("PurgeService() error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != deleteRequests {
		t.Fatalf("purge added node lifecycle deletes: before=%d after=%d", deleteRequests, len(lifecycle.DeleteRequests))
	}
	if got := lifecycle.DeleteRequests[len(lifecycle.DeleteRequests)-1].GetAllocationID(); got != allocationID {
		t.Fatalf("purge delete allocation = %q, want %q", got, allocationID)
	}
}

func TestPostgresServiceCompleteAllocationReleaseRevokesExecutionLeases(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 13, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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
	allocationID := admitted.GetAllocationIds()[0]

	leaseResp, err := app.runStore.IssueExecutionLease(context.Background(), allocationID, 1, commonv1.LeaseType_LEASE_TYPE_SERVICE, 5*time.Minute, now)
	if err != nil {
		t.Fatalf("AcquireServiceLease() error = %v", err)
	}
	if leaseResp.GetLeaseID() == "" {
		t.Fatal("IssueExecutionLease() returned empty lease id")
	}
	if _, err := app.db.Pool().Exec(context.Background(), `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, attempt, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 0, false, $8, $9)
	`, "lease-expired-"+allocationID, allocationID, "node-a", "node-a", int64(1), commonv1.LeaseType_LEASE_TYPE_SERVICE.String(), now.Add(-time.Minute), "expired-token-hash", now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert expired service lease: %v", err)
	}
	if err := app.servicePG.CompleteAllocationRelease(context.Background(), allocationID, now); err != nil {
		t.Fatalf("CompleteAllocationRelease() error = %v", err)
	}
	var activeLeases int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM execution_leases
		WHERE allocation_id = $1 AND revoked = false
	`, allocationID).Scan(&activeLeases); err != nil {
		t.Fatalf("count unrevoked service leases: %v", err)
	}
	if activeLeases != 0 {
		t.Fatalf("unrevoked service leases after allocation release = %d, want 0", activeLeases)
	}
}

func TestPostgresDeletedServiceReconcileRetriesAllocationRelease(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 13, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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
	allocationID := admitted.GetAllocationIds()[0]

	lifecycle.DeleteErr = errors.New("node delete temporarily unavailable")
	if _, err := public.DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID}); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	reconcileDeletedServiceAllocations(t, app, now)
	var reason, lastError string
	var attempts int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT reason, reconcile_attempts, last_error
		FROM allocation_reconcile_queue
		WHERE allocation_id = $1
	`, allocationID).Scan(&reason, &attempts, &lastError); err != nil {
		t.Fatalf("load reconcile queue after delete failure: %v", err)
	}
	if reason != allocationkernel.ReconcileReasonDelete {
		t.Fatalf("reconcile reason = %q, want %q", reason, allocationkernel.ReconcileReasonDelete)
	}
	if attempts != 1 {
		t.Fatalf("reconcile attempts = %d, want 1", attempts)
	}
	if lastError != "node delete temporarily unavailable" {
		t.Fatalf("last error = %q, want node delete temporarily unavailable", lastError)
	}
	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"}); err == nil {
		t.Fatal("PurgeService() unexpectedly succeeded before release retry")
	}
	lifecycle.DeleteErr = nil
	now = now.Add(allocationkernel.DeleteRetryDelay)
	if _, err := app.allocationReconciler.ReconcileAllocationBatch(context.Background(), now); err != nil {
		t.Fatalf("ReconcileAllocationBatch() error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != 2 {
		t.Fatalf("delete requests = %d, want initial delete and retry", len(lifecycle.DeleteRequests))
	}
	if got := lifecycle.DeleteRequests[1].GetAllocationID(); got != allocationID {
		t.Fatalf("retry delete allocation = %q, want %q", got, allocationID)
	}
	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = $1
	`, allocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count reconcile queue after delete retry success: %v", err)
	}
	if queueItems != 0 {
		t.Fatalf("reconcile queue items after delete retry success = %d, want 0", queueItems)
	}

	purgeResp, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"})
	if err != nil {
		t.Fatalf("PurgeService() after retry error = %v", err)
	}
	if purgeResp.GetServiceID() != serviceID {
		t.Fatalf("purged service id = %q, want %q", purgeResp.GetServiceID(), serviceID)
	}
}

func TestPostgresDeletedServiceDoesNotReleaseWhileNodeStillReportsAllocation(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 13, 45, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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
	allocationID := admitted.GetAllocationIds()[0]

	lifecycle.KeepDeletedVisible = true
	if _, err := public.DeleteService(context.Background(), &servicev1.DeleteServiceRequest{ServiceID: serviceID}); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	reconcileDeletedServiceAllocations(t, app, now)
	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"}); err == nil {
		t.Fatal("PurgeService() unexpectedly succeeded while node still reported allocation")
	} else if got := grpcstatus.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("PurgeService() code = %v, want FailedPrecondition", got)
	}

	lifecycle.KeepDeletedVisible = false
	now = now.Add(allocationkernel.DeleteRetryDelay)
	if _, err := app.allocationReconciler.ReconcileAllocationBatch(context.Background(), now); err != nil {
		t.Fatalf("ReconcileAllocationBatch() error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != 2 {
		t.Fatalf("delete requests = %d, want initial delete and retry", len(lifecycle.DeleteRequests))
	}
	if got := lifecycle.DeleteRequests[1].GetAllocationID(); got != allocationID {
		t.Fatalf("retry delete allocation = %q, want %q", got, allocationID)
	}
	if _, err := app.AdminV1Handler().PurgeService(context.Background(), &adminv1.PurgeServiceRequest{ServiceID: serviceID, OperatorReason: "test cleanup"}); err != nil {
		t.Fatalf("PurgeService() after node disappearance error = %v", err)
	}
}

func reconcileDeletedServiceAllocations(t *testing.T, app *App, now time.Time) {
	t.Helper()
	if err := app.serviceReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending(deleted service) error = %v", err)
	}
	if _, err := app.allocationReconciler.ReconcileAllocationBatch(context.Background(), now); err != nil {
		t.Fatalf("ReconcileAllocationBatch(deleted service) error = %v", err)
	}
}
