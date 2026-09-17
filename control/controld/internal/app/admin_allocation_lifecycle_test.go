package app

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	adminv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPostgresAdminForceAllocationLifecycleRetryRequiresOperatorReason(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	_, err := app.AdminV1Handler().ForceAllocationLifecycleRetry(context.Background(), &adminv1.ForceAllocationLifecycleRetryRequest{
		AllocationID:   "alloc-missing",
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
	if gotRun.GetRun().GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("run diagnostic_code after admin fail = %v, want runtime start error", gotRun.GetRun().GetDiagnosticCode())
	}
	req, ok := allocationkernel.ScheduleCreateRetryRequest(allocationID, 1, "late node failure", now)
	if !ok {
		t.Fatal("expected a stale retry request")
	}
	rescheduled, err := app.runStore.ScheduleClaimedReconcile(context.Background(), req, "stale-worker", now.Add(time.Second))
	if err != nil {
		t.Fatalf("ScheduleClaimedReconcile(after admin fail) error = %v", err)
	}
	if rescheduled {
		t.Fatal("stale run reconciler recreated an operator-failed lifecycle retry")
	}
	if _, err := app.runStore.MarkAllocationCreateFailed(context.Background(), allocationID, "stale-worker", "late retry exhaustion", now.Add(time.Second)); !errors.Is(err, allocationkernel.ErrReconcileClaimLost) {
		t.Fatalf("MarkAllocationCreateFailed(after admin fail) error = %v, want claim lost", err)
	}
	assertAllocationReleasePending(t, app, allocationID)
	now = now.Add(time.Second)
	app.reconcileV1()
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests after admin fail = %d, want 1", len(lifecycle.DeleteRequests))
	}
	assertAllocationRetryCleanup(t, app, allocationID, "fail_allocation_lifecycle_retry")
}

func TestPostgresAdminFailAllocationLifecycleRetryRejectsMissingRetry(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	_, err := app.AdminV1Handler().FailAllocationLifecycleRetry(context.Background(), &adminv1.FailAllocationLifecycleRetryRequest{
		AllocationID:   "alloc-missing",
		OperatorReason: "operator requested fail",
	})
	if err == nil {
		t.Fatal("FailAllocationLifecycleRetry(missing) unexpectedly succeeded")
	}
	if got := grpcstatus.Code(err); got != codes.NotFound {
		t.Fatalf("FailAllocationLifecycleRetry(missing) code = %v, want NotFound", got)
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
		OperatorReason: "operator attempted unsafe clear",
	})
	if err == nil {
		t.Fatal("ClearAllocationLifecycleRetry(active allocation) unexpectedly succeeded")
	}
	if got := grpcstatus.Code(err); got != codes.FailedPrecondition {
		t.Fatalf("ClearAllocationLifecycleRetry(active allocation) code = %v, want FailedPrecondition", got)
	}
	listResp, err := admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{},
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
		SET lifecycle_state = $2, updated_at = $3
		WHERE allocation_id = $1
	`, allocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(), now.UTC()); err != nil {
		t.Fatalf("mark allocation released for clear precondition: %v", err)
	}
	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE runs
		SET status = $2, updated_at = $3
		WHERE run_id = (SELECT run_id FROM allocations WHERE allocation_id = $1)
	`, allocationID, runv1.RunStatus_RUN_STATUS_FAILED.String(), now.UTC()); err != nil {
		t.Fatalf("mark run failed for clear precondition: %v", err)
	}
	listResp, err = admin.ListAllocationLifecycleRetries(context.Background(), &adminv1.ListAllocationLifecycleRetriesRequest{
		Filter: &adminv1.AllocationLifecycleRetryFilter{},
	})
	if err != nil {
		t.Fatalf("ListAllocationLifecycleRetries(clearable) error = %v", err)
	}
	if len(listResp.GetRetries()) != 1 || !listResp.GetRetries()[0].GetClearable() || listResp.GetRetries()[0].GetClearBlockedReason() != "" {
		t.Fatalf("terminal retry clearability = %+v, want one clearable retry", listResp.GetRetries())
	}
	clearResp, err := admin.ClearAllocationLifecycleRetry(context.Background(), &adminv1.ClearAllocationLifecycleRetryRequest{
		AllocationID:   allocationID,
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
	var chargedAllocations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocations WHERE allocation_id = $1 AND lifecycle_state <> $2
	`, allocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()).Scan(&chargedAllocations); err != nil {
		t.Fatalf("count charged allocations after admin operation: %v", err)
	}
	if chargedAllocations != 0 {
		t.Fatalf("charged allocations after admin operation = %d, want 0", chargedAllocations)
	}
	assertPostgresConsistencyOK(t, app)
	if auditOperation == "" {
		return
	}
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

func assertAllocationReleasePending(t *testing.T, app *App, allocationID string) {
	t.Helper()
	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM allocation_reconcile_queue
		WHERE allocation_id = $1
	`, allocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count pending allocation delete: %v", err)
	}
	if queueItems != 1 {
		t.Fatalf("pending allocation deletes = %d, want 1", queueItems)
	}
	var chargedAllocations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocations WHERE allocation_id = $1 AND lifecycle_state <> $2
	`, allocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()).Scan(&chargedAllocations); err != nil {
		t.Fatalf("count charged allocations pending release: %v", err)
	}
	if chargedAllocations != 1 {
		t.Fatalf("charged allocations pending release = %d, want 1", chargedAllocations)
	}
}
