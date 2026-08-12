package app

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestPostgresServiceCreateFailureRetriesBeforeFailing(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 9, 15, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	lifecycle.CreateErr = errors.New("node create temporarily unavailable")
	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	allocationID := admitted.GetAllocationIds()[0]

	var attempts int
	var lastError string
	var nextRunAt time.Time
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT reconcile_attempts, last_error, next_run_at
		FROM allocation_reconcile_queue
		WHERE allocation_id = $1 AND reason = $2
	`, allocationID, allocationkernel.ReconcileReasonCreate).Scan(&attempts, &lastError, &nextRunAt); err != nil {
		t.Fatalf("load reconcile queue after create failure: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("reconcile attempts = %d, want 1", attempts)
	}
	if lastError != "node create temporarily unavailable" {
		t.Fatalf("last error = %q, want node create temporarily unavailable", lastError)
	}
	if want := now.Add(allocationkernel.CreateRetryDelay(1)); !nextRunAt.Equal(want) {
		t.Fatalf("next_run_at = %v, want %v", nextRunAt, want)
	}
	getResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after create failure) error = %v", err)
	}
	if got := getResp.GetService().GetMessage(); got != "node create temporarily unavailable" {
		t.Fatalf("GetService message after create failure = %q, want lifecycle retry error", got)
	}
	if got := getResp.GetService().GetDiagnosticCode(); got != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("GetService diagnostic_code after create failure = %v, want RUNTIME_START_ERROR", got)
	}
	listResp, err := public.ListServices(context.Background(), &servicev1.ListServicesRequest{})
	if err != nil {
		t.Fatalf("ListServices(after create failure) error = %v", err)
	}
	if len(listResp.GetServices()) != 1 {
		t.Fatalf("ListServices(after create failure) returned %d services, want 1", len(listResp.GetServices()))
	}
	if got := listResp.GetServices()[0].GetMessage(); got != "node create temporarily unavailable" {
		t.Fatalf("ListServices message after create failure = %q, want lifecycle retry error", got)
	}
	if got := listResp.GetServices()[0].GetDiagnosticCode(); got != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("ListServices diagnostic_code after create failure = %v, want RUNTIME_START_ERROR", got)
	}
	replicasResp, err := public.ListServiceReplicas(context.Background(), &servicev1.ListServiceReplicasRequest{
		ServiceID: createResp.GetService().GetID(),
	})
	if err != nil {
		t.Fatalf("ListServiceReplicas(after create failure) error = %v", err)
	}
	if len(replicasResp.GetReplicas()) != 1 {
		t.Fatalf("replicas after create failure = %d, want 1", len(replicasResp.GetReplicas()))
	}
	retry := replicasResp.GetReplicas()[0].GetLifecycleRetry()
	if retry == nil {
		t.Fatal("replica lifecycle retry = nil, want create retry details")
	}
	if retry.GetReason() != servicev1.ServiceReplicaLifecycleRetryReason_SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_CREATE || retry.GetAttempts() != 1 || retry.GetLastError() != "node create temporarily unavailable" || !retry.GetNextRunAt().AsTime().Equal(nextRunAt) {
		t.Fatalf("replica lifecycle retry = %+v, want create retry details", retry)
	}
	queueMetrics, err := app.allocationReconcileQueueMetrics(context.Background())
	if err != nil {
		t.Fatalf("allocationReconcileQueueMetrics() error = %v", err)
	}
	var foundMetric bool
	for _, metric := range queueMetrics {
		if metric.ownerType == allocationkernel.OwnerService && metric.reason == allocationkernel.ReconcileReasonCreate {
			foundMetric = true
			if metric.count != 1 || metric.maxAttempts != 1 {
				t.Fatalf("service create queue metric = %+v, want count=1 max_attempts=1", metric)
			}
		}
	}
	if !foundMetric {
		t.Fatal("service create queue metric not found")
	}

	lifecycle.CreateErr = nil
	now = nextRunAt
	if _, err := app.allocationReconciler.ReconcileAllocationBatch(context.Background(), now); err != nil {
		t.Fatalf("ReconcileAllocationBatch() error = %v", err)
	}

	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = $1
	`, allocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count reconcile queue after retry success: %v", err)
	}
	if queueItems != 0 {
		t.Fatalf("reconcile queue items after retry success = %d, want 0", queueItems)
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("create attempts = %d, want initial failure plus retry success", len(lifecycle.CreateRequests))
	}
}

func TestPostgresServiceReenqueuePreservesActiveAllocationClaim(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 7, 16, 7, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	created, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	service := reconcileCreatedService(t, app, created.GetService().GetID(), now)
	allocationID := service.GetAllocationIds()[0]
	request := allocationkernel.ScheduleDeleteRequest(allocationID, now)
	if err := app.servicePG.ScheduleReconcile(context.Background(), request, now); err != nil {
		t.Fatalf("ScheduleReconcile() error = %v", err)
	}
	items, err := app.servicePG.ClaimDueReconcileItems(context.Background(), "worker-a", 1, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimDueReconcileItems() error = %v", err)
	}
	if len(items) != 1 || items[0].AllocationID != allocationID {
		t.Fatalf("claimed items = %+v, want %s", items, allocationID)
	}

	if err := app.servicePG.ScheduleReconcile(context.Background(), request, now.Add(time.Second)); err != nil {
		t.Fatalf("ScheduleReconcile(reenqueue) error = %v", err)
	}
	renewed, err := app.servicePG.RenewReconcileClaim(context.Background(), allocationID, "worker-a", now.Add(2*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("RenewReconcileClaim() error = %v", err)
	}
	if !renewed {
		t.Fatal("same-reason reenqueue revoked the active allocation claim")
	}
}

func TestPostgresServiceCreateRetryExhaustionReleasesReservationAndAdmitsReplacement(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 9, 16, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	if _, err := public.SetNamespaceQuota(context.Background(), &quotav1.SetNamespaceQuotaRequest{
		Namespace: "default",
		Limits: &quotav1.NamespaceQuotaLimits{
			CpuMilli:    wrapperspb.Int64(1000),
			MemoryBytes: wrapperspb.Int64(8 << 30),
		},
	}); err != nil {
		t.Fatalf("SetNamespaceQuota() error = %v", err)
	}
	lifecycle.CreateErr = errors.New("node create unavailable")
	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	failedAllocationID := admitted.GetAllocationIds()[0]

	for i := 1; i < allocationkernel.CreateRetryMaxAttempts; i++ {
		now = now.Add(allocationkernel.CreateRetryDelay(i))
		if _, err := app.allocationReconciler.ReconcileAllocationBatch(context.Background(), now); err != nil {
			t.Fatalf("ReconcileAllocationBatch(attempt %d) error = %v", i+1, err)
		}
	}

	var failedStatus string
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT status FROM allocations WHERE allocation_id = $1
	`, failedAllocationID).Scan(&failedStatus); err != nil {
		t.Fatalf("load failed allocation: %v", err)
	}
	if failedStatus != commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String() {
		t.Fatalf("failed allocation status = %q, want FAILED", failedStatus)
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, failedAllocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations for failed allocation: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations for exhausted create retry = %d, want 0", activeReservations)
	}
	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = $1
	`, failedAllocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count reconcile queue after exhausted create retry: %v", err)
	}
	if queueItems != 0 {
		t.Fatalf("reconcile queue items after exhausted create retry = %d, want 0", queueItems)
	}

	lifecycle.CreateErr = nil
	reportReadyNodeSnapshot(t, app, "node-a", now, 2)
	replacement := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	if len(replacement.GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids after replacement reconcile = %#v, want one replacement", replacement.GetAllocationIds())
	}
	replacementID := replacement.GetAllocationIds()[0]
	if replacementID == failedAllocationID {
		t.Fatal("replacement reused exhausted allocation id")
	}
	assertActiveReservation(t, app, replacementID, executionkernel.DefaultCPUMilli, executionkernel.DefaultMemoryBytes)
}

func TestPostgresServiceAllocationStatusDrivesDegradeAndReplacement(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
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
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	if len(admitted.GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids = %#v, want 1", admitted.GetAllocationIds())
	}
	firstAllocationID := admitted.GetAllocationIds()[0]

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID:  firstAllocationID,
			Attempt:       1,
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
			ExitCode:      1,
			ExitCodeKnown: true,
			Message:       "boom",
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(EXITED) error = %v", err)
	}

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("service status = %v, want DEGRADED", gotResp.GetService().GetStatus())
	}
	if gotResp.GetService().GetReadyReplicas() != 0 || gotResp.GetService().GetUnhealthyReplicas() == 0 {
		t.Fatalf("service counters after EXITED = ready:%d unhealthy:%d, want 0/>0", gotResp.GetService().GetReadyReplicas(), gotResp.GetService().GetUnhealthyReplicas())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("allocation_ids after exit = %#v, want empty", gotResp.GetService().GetAllocationIds())
	}

	app.reconcileV1()

	gotResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after reconcile) error = %v", err)
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids after reconcile = %#v, want 1 replacement", gotResp.GetService().GetAllocationIds())
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("service status after reconcile = %v, want DEGRADED until replacement RUNNING", gotResp.GetService().GetStatus())
	}
	if gotResp.GetService().GetReadyReplicas() != 0 || gotResp.GetService().GetUnhealthyReplicas() == 0 {
		t.Fatalf("service counters after reconcile = ready:%d unhealthy:%d, want 0/>0", gotResp.GetService().GetReadyReplicas(), gotResp.GetService().GetUnhealthyReplicas())
	}
	if gotResp.GetService().GetAllocationIds()[0] == firstAllocationID {
		t.Fatal("service reconcile reused exited allocation id")
	}
	if len(lifecycle.CreateRequests) != 2 {
		t.Fatalf("node lifecycle create requests = %d, want 2 including replacement", len(lifecycle.CreateRequests))
	}

	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: gotResp.GetService().GetAllocationIds()[0],
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(replacement RUNNING) error = %v", err)
	}

	gotResp, err = public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after replacement RUNNING) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("service status after recovery = %v, want READY", gotResp.GetService().GetStatus())
	}
	if gotResp.GetService().GetReadyReplicas() != 1 || gotResp.GetService().GetUnhealthyReplicas() != 0 {
		t.Fatalf("service counters after recovery = ready:%d unhealthy:%d, want 1/0", gotResp.GetService().GetReadyReplicas(), gotResp.GetService().GetUnhealthyReplicas())
	}
}

func TestPostgresServiceMissingFromNodeInventoryDegradesService(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
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
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	allocationID := admitted.GetAllocationIds()[0]
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: allocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(RUNNING) error = %v", err)
	}

	readyResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(ready) error = %v", err)
	}
	if readyResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("service status after RUNNING = %v, want READY", readyResp.GetService().GetStatus())
	}

	now = now.Add(30 * time.Second)
	summary := controldtest.ReadySummary(now)
	summary.Components.Axnoded.RunningContainers = 0
	summary.Components.Axnoded.RunningAllocationIds = nil
	summary.Components.Axnoded.ActiveAllocationIds = nil
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{
		NodeID:        "node-a",
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
		Summary:       summary,
	}); err != nil {
		t.Fatalf("ReportNode(missing inventory) error = %v", err)
	}

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after missing inventory) error = %v", err)
	}
	if gotResp.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("service status after missing inventory = %v, want DEGRADED", gotResp.GetService().GetStatus())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("allocation_ids after missing inventory = %#v, want empty", gotResp.GetService().GetAllocationIds())
	}
	if gotResp.GetService().GetUnhealthyReplicas() == 0 {
		t.Fatalf("unhealthy_replicas after missing inventory = %d, want >0", gotResp.GetService().GetUnhealthyReplicas())
	}
	if gotResp.GetService().GetMessage() != "allocation missing from node inventory" {
		t.Fatalf("service message after missing inventory = %q, want inventory-missing message", gotResp.GetService().GetMessage())
	}

	listResp, err := public.ListServices(context.Background(), &servicev1.ListServicesRequest{
		Filter: &servicev1.ServiceListFilter{Namespace: "default"},
	})
	if err != nil {
		t.Fatalf("ListServices(after missing inventory) error = %v", err)
	}
	if len(listResp.GetServices()) != 1 || listResp.GetServices()[0].GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("ListServices(after missing inventory) = %#v, want one DEGRADED service", listResp.GetServices())
	}
}

func TestPostgresServiceRejectsStaleNodeSummaryWithoutFailingAllocation(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 10, 20, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	staleSnapshotAt := now.Add(5 * time.Second)

	now = now.Add(10 * time.Second)
	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	allocationID := admitted.GetAllocationIds()[0]

	now = now.Add(10 * time.Second)
	if _, err := node.BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "test-node-token",
		Observations: []*nodev1.AllocationStatusObservation{{
			AllocationID: allocationID,
			Attempt:      1,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING,
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus(STARTING after snapshot) error = %v", err)
	}

	now = now.Add(10 * time.Second)
	summary := controldtest.ReadySummary(staleSnapshotAt)
	summary.Components.Axnoded.RunningContainers = 0
	summary.Components.Axnoded.RunningAllocationIds = nil
	summary.Components.Axnoded.ActiveAllocationIds = nil
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{
		NodeID:        "node-a",
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
		Summary:       summary,
	}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("ReportNode(stale summary) code = %v, want InvalidArgument", grpcstatus.Code(err))
	}

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after stale inventory) error = %v", err)
	}
	if gotResp.GetService().GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("service status after stale inventory = %v, want not DEGRADED", gotResp.GetService().GetStatus())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 || gotResp.GetService().GetAllocationIds()[0] != allocationID {
		t.Fatalf("allocation_ids after stale inventory = %#v, want original allocation %q", gotResp.GetService().GetAllocationIds(), allocationID)
	}
	if gotResp.GetService().GetMessage() == allocationkernel.MissingFromNodeInventoryMessage {
		t.Fatalf("service message after stale inventory = %q, want no inventory-missing failure", gotResp.GetService().GetMessage())
	}
}

func TestPostgresServiceStaleNodeHeartbeatFailsAllocationAndAdmitsReplacement(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 5, 9, 13, 0, 0, 0, time.UTC)
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
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	lostAllocationID := admitted.GetAllocationIds()[0]

	now = now.Add(30 * time.Second)
	registerReadyNode(t, app, "node-b", now)
	app.reconcileV1()

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after node unavailable reconcile) error = %v", err)
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 {
		t.Fatalf("allocation_ids after node unavailable reconcile = %#v, want one replacement", gotResp.GetService().GetAllocationIds())
	}
	if gotResp.GetService().GetAllocationIds()[0] == lostAllocationID {
		t.Fatal("service kept allocation from unavailable node")
	}

	var lostStatus, lostMessage, replacementNodeID string
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT status, message FROM allocations WHERE allocation_id = $1
	`, lostAllocationID).Scan(&lostStatus, &lostMessage); err != nil {
		t.Fatalf("load lost allocation: %v", err)
	}
	if lostStatus != commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String() {
		t.Fatalf("lost allocation status = %q, want FAILED", lostStatus)
	}
	if lostMessage != allocationkernel.NodeUnavailableMessage {
		t.Fatalf("lost allocation message = %q, want %q", lostMessage, allocationkernel.NodeUnavailableMessage)
	}
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT node_id FROM allocations WHERE allocation_id = $1
	`, gotResp.GetService().GetAllocationIds()[0]).Scan(&replacementNodeID); err != nil {
		t.Fatalf("load replacement allocation node: %v", err)
	}
	if replacementNodeID != "node-b" {
		t.Fatalf("replacement node = %q, want node-b", replacementNodeID)
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, lostAllocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations for lost allocation = %d, want 0", activeReservations)
	}
}

func TestPostgresServiceStartingAllocationInActiveInventoryDoesNotDegrade(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 10, 30, 0, 0, time.UTC)
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
	admitted := reconcileCreatedService(t, app, createResp.GetService().GetID(), now)
	allocationID := admitted.GetAllocationIds()[0]

	now = now.Add(10 * time.Second)
	summary := controldtest.ReadySummary(now)
	summary.Components.Axnoded.RunningContainers = 0
	summary.Components.Axnoded.RunningAllocationIds = nil
	summary.Components.Axnoded.ActiveAllocationIds = []string{allocationID}
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{
		NodeID:        "node-a",
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
		Summary:       summary,
	}); err != nil {
		t.Fatalf("ReportNode(starting inventory) error = %v", err)
	}

	gotResp, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after starting inventory) error = %v", err)
	}
	if gotResp.GetService().GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("service status after starting inventory = %v, want not DEGRADED", gotResp.GetService().GetStatus())
	}
	if len(gotResp.GetService().GetAllocationIds()) != 1 || gotResp.GetService().GetAllocationIds()[0] != allocationID {
		t.Fatalf("allocation_ids after starting inventory = %#v, want original starting allocation", gotResp.GetService().GetAllocationIds())
	}
	if gotResp.GetService().GetMessage() == "allocation missing from node inventory" {
		t.Fatalf("service message after starting inventory = %q, want no inventory-missing failure", gotResp.GetService().GetMessage())
	}
}
