package app

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPostgresAdminForceAllocationLifecycleRetry(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	admin := app.AdminV1Handler()

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

	listResp, err := admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{
			OwnerType: adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE,
			Reason:    adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAllocationLifecycleRetries() error = %v", err)
	}
	if len(listResp.GetRetries()) != 1 {
		t.Fatalf("retry count = %d, want 1", len(listResp.GetRetries()))
	}
	if got := listResp.GetRetries()[0].GetAllocationID(); got != allocationID {
		t.Fatalf("listed allocation_id = %q, want %q", got, allocationID)
	}
	if listResp.GetRetries()[0].GetDue() {
		t.Fatal("retry is due before force, want false")
	}

	forceResp, err := admin.ForceAllocationLifecycleRetry(context.Background(), &adminv1.ForceAllocationLifecycleRetryRequest{
		AllocationID:   allocationID,
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: "operator verified node is reachable",
	})
	if err != nil {
		t.Fatalf("ForceAllocationLifecycleRetry() error = %v", err)
	}
	if forceResp.GetRetry().GetAllocationID() != allocationID || !forceResp.GetRetry().GetDue() {
		t.Fatalf("forced retry = %+v, want same allocation due", forceResp.GetRetry())
	}
	var auditEvents int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM admin_audit_events
		WHERE operation = 'force_allocation_lifecycle_retry'
		  AND target_id = $1
		  AND operator_reason = 'operator verified node is reachable'
	`, allocationID).Scan(&auditEvents); err != nil {
		t.Fatalf("count admin audit events: %v", err)
	}
	if auditEvents != 1 {
		t.Fatalf("admin audit events = %d, want 1", auditEvents)
	}
	auditResp, err := admin.ListAdminAuditEvents(context.Background(), &adminv1.ListAdminAuditEventsRequest{
		Filter: &adminv1.AdminAuditEventFilter{
			Operation:  adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY,
			TargetType: adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION,
			TargetID:   allocationID,
		},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAdminAuditEvents() error = %v", err)
	}
	if len(auditResp.GetEvents()) != 1 {
		t.Fatalf("admin audit event count = %d, want 1", len(auditResp.GetEvents()))
	}
	if got := auditResp.GetEvents()[0].GetOperatorReason(); got != "operator verified node is reachable" {
		t.Fatalf("admin audit operator reason = %q, want operator verified node is reachable", got)
	}

	lifecycle.CreateErr = nil
	if _, err := app.allocationReconciler.ReconcileAllocationBatch(context.Background(), now); err != nil {
		t.Fatalf("ReconcileAllocationBatch() error = %v", err)
	}
	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = $1
	`, allocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count reconcile queue after forced retry: %v", err)
	}
	if queueItems != 0 {
		t.Fatalf("reconcile queue items after forced retry = %d, want 0", queueItems)
	}
}

func TestPostgresAdminForceAllocationLifecycleRetryRequiresOperatorReason(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	_, err := app.AdminV1Handler().ForceAllocationLifecycleRetry(context.Background(), &adminv1.ForceAllocationLifecycleRetryRequest{
		AllocationID:   "alloc-missing",
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: " ",
	})
	if err == nil {
		t.Fatal("ForceAllocationLifecycleRetry() unexpectedly succeeded")
	}
	if got := grpcstatus.Code(err); got != codes.InvalidArgument {
		t.Fatalf("ForceAllocationLifecycleRetry() code = %v, want InvalidArgument", got)
	}
}

func TestPostgresAdminFailRunCreateLifecycleRetry(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 13, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	admin := app.AdminV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/sleep", "60"}},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	allocationID := runResp.GetRun().GetAllocationID()
	lifecycle.CreateErr = errors.New("node create unavailable")
	app.reconcileV1()

	failResp, err := admin.FailAllocationLifecycleRetry(context.Background(), &adminv1.FailAllocationLifecycleRetryRequest{
		AllocationID:   allocationID,
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: "operator confirmed create cannot recover",
	})
	if err != nil {
		t.Fatalf("FailAllocationLifecycleRetry(run create) error = %v", err)
	}
	if failResp.GetFailedRetry().GetAllocationID() != allocationID {
		t.Fatalf("failed retry allocation_id = %q, want %q", failResp.GetFailedRetry().GetAllocationID(), allocationID)
	}
	gotRun, err := public.GetRun(context.Background(), &runv1.GetRunRequest{RunID: runResp.GetRun().GetID()})
	if err != nil {
		t.Fatalf("GetRun(after admin fail) error = %v", err)
	}
	if gotRun.GetRun().GetStatus() != runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("run status after admin fail = %v, want FAILED", gotRun.GetRun().GetStatus())
	}
	req, ok := allocationkernel.ScheduleCreateRetryRequest(allocationID, 1, "late node failure", now)
	if !ok {
		t.Fatal("expected a stale retry request")
	}
	rescheduled, err := app.runStore.RescheduleReconcile(context.Background(), req, now.Add(time.Second))
	if err != nil {
		t.Fatalf("RescheduleReconcile(after admin fail) error = %v", err)
	}
	if rescheduled {
		t.Fatal("stale run reconciler recreated an operator-failed lifecycle retry")
	}
	lateRun, err := app.runStore.MarkAllocationCreateFailed(context.Background(), allocationID, "late retry exhaustion", now.Add(time.Second))
	if err != nil {
		t.Fatalf("MarkAllocationCreateFailed(after admin fail) error = %v", err)
	}
	if lateRun.GetMessage() != "operator confirmed create cannot recover" {
		t.Fatalf("late reconciliation replaced operator terminal message: %q", lateRun.GetMessage())
	}
	assertAllocationRetryCleanup(t, app, allocationID, "fail_allocation_lifecycle_retry")
}

func TestPostgresAdminFailServiceCreateLifecycleRetry(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 13, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	admin := app.AdminV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
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
	allocationID := admitted.GetAllocationIds()[0]

	_, err = admin.FailAllocationLifecycleRetry(context.Background(), &adminv1.FailAllocationLifecycleRetryRequest{
		AllocationID:   allocationID,
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: "operator removed stuck service replica",
	})
	if err != nil {
		t.Fatalf("FailAllocationLifecycleRetry(service create) error = %v", err)
	}
	gotService, err := public.GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: createResp.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after admin fail) error = %v", err)
	}
	if gotService.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		t.Fatalf("service status after admin fail = %v, want DEGRADED", gotService.GetService().GetStatus())
	}
	if len(gotService.GetService().GetAllocationIds()) != 0 {
		t.Fatalf("service allocation_ids after admin fail = %v, want empty", gotService.GetService().GetAllocationIds())
	}
	assertAllocationRetryCleanup(t, app, allocationID, "fail_allocation_lifecycle_retry")
}

func TestPostgresAdminFailAllocationLifecycleRetryRejectsDeleteReason(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	_, err := app.AdminV1Handler().FailAllocationLifecycleRetry(context.Background(), &adminv1.FailAllocationLifecycleRetryRequest{
		AllocationID:   "alloc-missing",
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE,
		OperatorReason: "operator requested fail",
	})
	if err == nil {
		t.Fatal("FailAllocationLifecycleRetry(delete) unexpectedly succeeded")
	}
	if got := grpcstatus.Code(err); got != codes.InvalidArgument {
		t.Fatalf("FailAllocationLifecycleRetry(delete) code = %v, want InvalidArgument", got)
	}
}

func TestPostgresAdminClearAllocationLifecycleRetryRequiresTerminalCleanup(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 14, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	admin := app.AdminV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/sleep", "60"}},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	allocationID := runResp.GetRun().GetAllocationID()
	lifecycle.CreateErr = errors.New("node create unavailable")
	app.reconcileV1()

	_, err = admin.ClearAllocationLifecycleRetry(context.Background(), &adminv1.ClearAllocationLifecycleRetryRequest{
		AllocationID:   allocationID,
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: "operator attempted unsafe clear",
	})
	if err == nil {
		t.Fatal("ClearAllocationLifecycleRetry(active allocation) unexpectedly succeeded")
	}
	if got := grpcstatus.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("ClearAllocationLifecycleRetry(active allocation) code = %v, want FailedPrecondition", got)
	}
	listResp, err := admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{
			Reason: adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		},
	})
	if err != nil {
		t.Fatalf("ListAllocationLifecycleRetries(active clearability) error = %v", err)
	}
	if len(listResp.GetRetries()) != 1 || listResp.GetRetries()[0].GetClearable() {
		t.Fatalf("active retry clearability = %+v, want one non-clearable retry", listResp.GetRetries())
	}
	if got := listResp.GetRetries()[0].GetClearBlockedReason(); got == "" {
		t.Fatal("active retry clear_blocked_reason is empty")
	}

	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE allocations
		SET status = $2, updated_at = $3
		WHERE allocation_id = $1
	`, allocationID, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), now.UTC()); err != nil {
		t.Fatalf("mark allocation failed for clear precondition: %v", err)
	}
	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE runs
		SET status = $2, updated_at = $3
		WHERE allocation_id = $1
	`, allocationID, runv1.RunStatus_RUN_STATUS_FAILED.String(), now.UTC()); err != nil {
		t.Fatalf("mark run failed for clear precondition: %v", err)
	}
	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE workload_reservations
		SET released_at = $2
		WHERE allocation_id = $1
	`, allocationID, now.UTC()); err != nil {
		t.Fatalf("release reservation for clear precondition: %v", err)
	}
	listResp, err = admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{
			Reason: adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		},
	})
	if err != nil {
		t.Fatalf("ListAllocationLifecycleRetries(clearable) error = %v", err)
	}
	if len(listResp.GetRetries()) != 1 || !listResp.GetRetries()[0].GetClearable() || listResp.GetRetries()[0].GetClearBlockedReason() != "" {
		t.Fatalf("terminal retry clearability = %+v, want one clearable retry", listResp.GetRetries())
	}
	clearResp, err := admin.ClearAllocationLifecycleRetry(context.Background(), &adminv1.ClearAllocationLifecycleRetryRequest{
		AllocationID:   allocationID,
		Reason:         adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		OperatorReason: "operator removed stale terminal retry",
	})
	if err != nil {
		t.Fatalf("ClearAllocationLifecycleRetry(terminal clean allocation) error = %v", err)
	}
	if clearResp.GetClearedRetry().GetAllocationID() != allocationID {
		t.Fatalf("cleared retry allocation_id = %q, want %q", clearResp.GetClearedRetry().GetAllocationID(), allocationID)
	}
	assertAllocationRetryCleanup(t, app, allocationID, "clear_allocation_lifecycle_retry")
}

func TestPostgresAdminListAllocationLifecycleRetriesDueOnly(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 12, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	admin := app.AdminV1Handler()

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

	listResp, err := admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{DueOnly: true},
	})
	if err != nil {
		t.Fatalf("ListAllocationLifecycleRetries(due_only before delay) error = %v", err)
	}
	if len(listResp.GetRetries()) != 0 {
		t.Fatalf("due retry count before delay = %d, want 0", len(listResp.GetRetries()))
	}

	now = now.Add(allocationkernel.CreateRetryDelay(1))
	listResp, err = admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{DueOnly: true},
	})
	if err != nil {
		t.Fatalf("ListAllocationLifecycleRetries(due_only after delay) error = %v", err)
	}
	if len(listResp.GetRetries()) != 1 || listResp.GetRetries()[0].GetAllocationID() != allocationID {
		t.Fatalf("due retries after delay = %+v, want allocation %s", listResp.GetRetries(), allocationID)
	}
}

func assertAllocationRetryCleanup(t *testing.T, app *App, allocationID string, auditOperation string) {
	t.Helper()
	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = $1
	`, allocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count reconcile queue after admin operation: %v", err)
	}
	if queueItems != 0 {
		t.Fatalf("reconcile queue items after admin operation = %d, want 0", queueItems)
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, allocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations after admin operation: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations after admin operation = %d, want 0", activeReservations)
	}
	assertPostgresConsistencyOK(t, app)
	var auditEvents int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM admin_audit_events
		WHERE operation = $2 AND target_id = $1
	`, allocationID, auditOperation).Scan(&auditEvents); err != nil {
		t.Fatalf("count admin audit events after admin operation: %v", err)
	}
	if auditEvents != 1 {
		t.Fatalf("admin audit events for %s = %d, want 1", auditOperation, auditEvents)
	}
}
