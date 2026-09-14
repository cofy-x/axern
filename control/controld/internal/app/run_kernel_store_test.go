package app

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/protobuf/proto"
)

func TestPostgresRunKernelEnvironmentLabelsDoNotChangeSpecIdentity(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	public := app.PublicV1Handler()

	first, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec:   &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
		Labels: map[string]string{"team": "infra"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(first) error = %v", err)
	}
	second, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec:   &environmentv1.EnvironmentSpec{TemplateID: "python311", Namespace: "default"},
		Labels: map[string]string{"team": "runtime"},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment(second) error = %v", err)
	}
	if first.GetEnvironment().GetID() == second.GetEnvironment().GetID() {
		t.Fatalf("independent environment resources reused id %q", first.GetEnvironment().GetID())
	}
	if !proto.Equal(first.GetEnvironment().GetResolvedSpec(), second.GetEnvironment().GetResolvedSpec()) {
		t.Fatal("equivalent environments produced different resolved specifications")
	}
	if first.GetEnvironment().GetLabels()["team"] != "infra" || second.GetEnvironment().GetLabels()["team"] != "runtime" {
		t.Fatalf("environment labels were not independently preserved: first=%v second=%v", first.GetEnvironment().GetLabels(), second.GetEnvironment().GetLabels())
	}
}

func TestPostgresRunStaleNodeHeartbeatFailsRunThenReleasesAllocation(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 5, 9, 13, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	allocationID := runResp.GetRun().GetAllocationID()

	now = now.Add(30 * time.Second)
	app.reconcileV1()

	gotResp, err := public.GetRun(context.Background(), &runv1.GetRunRequest{RunID: runResp.GetRun().GetID()})
	if err != nil {
		t.Fatalf("GetRun(after node unavailable reconcile) error = %v", err)
	}
	if gotResp.GetRun().GetStatus() != runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("run status = %v, want FAILED", gotResp.GetRun().GetStatus())
	}
	if gotResp.GetRun().GetMessage() != allocationkernel.NodeUnavailableMessage {
		t.Fatalf("run message = %q, want %q", gotResp.GetRun().GetMessage(), allocationkernel.NodeUnavailableMessage)
	}
	if gotResp.GetRun().GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("run diagnostic_code = %v, want runtime start error", gotResp.GetRun().GetDiagnosticCode())
	}
	assertAllocationReleasePending(t, app, allocationID)
	app.reconcileV1()
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests for unavailable node allocation = %d, want 1", len(lifecycle.DeleteRequests))
	}
	assertAllocationRetryCleanup(t, app, allocationID, "")
}

func TestPostgresRunStartCreateFailureRetriesBeforeFailing(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 9, 14, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	allocationID := runResp.GetRun().GetAllocationID()

	lifecycle.CreateErr = errors.New("node temporarily unavailable")
	app.reconcileV1()

	gotResp, err := public.GetRun(context.Background(), &runv1.GetRunRequest{RunID: runResp.GetRun().GetID()})
	if err != nil {
		t.Fatalf("GetRun(after first start failure) error = %v", err)
	}
	if gotResp.GetRun().GetStatus() == runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatal("run failed before start retry exhaustion")
	}
	var attempts int
	var lastError string
	var nextRunAt time.Time
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT reconcile_attempts, last_error, next_run_at
		FROM allocation_reconcile_queue
		WHERE allocation_id = $1
	`, allocationID).Scan(&attempts, &lastError, &nextRunAt); err != nil {
		t.Fatalf("load reconcile queue after start failure: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("reconcile attempts = %d, want 1", attempts)
	}
	if lastError != "node temporarily unavailable" {
		t.Fatalf("last error = %q, want node temporarily unavailable", lastError)
	}
	if want := now.Add(allocationkernel.CreateRetryDelay(1)); !nextRunAt.Equal(want) {
		t.Fatalf("next_run_at = %v, want %v", nextRunAt, want)
	}

	lifecycle.CreateErr = nil
	now = nextRunAt
	app.reconcileV1()

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

func TestPostgresRunCreateRetryExhaustionReleasesAllocation(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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
	for i := 1; i <= allocationkernel.CreateRetryMaxAttempts; i++ {
		app.reconcileV1()
		now = now.Add(allocationkernel.CreateRetryDelay(i))
	}

	gotResp, err := public.GetRun(context.Background(), &runv1.GetRunRequest{RunID: runResp.GetRun().GetID()})
	if err != nil {
		t.Fatalf("GetRun(after create retry exhaustion) error = %v", err)
	}
	if gotResp.GetRun().GetStatus() != runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("run status after create retry exhaustion = %v, want FAILED", gotResp.GetRun().GetStatus())
	}
	if gotResp.GetRun().GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR {
		t.Fatalf("run diagnostic_code after create retry exhaustion = %v, want runtime start error", gotResp.GetRun().GetDiagnosticCode())
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests during run create retry exhaustion cleanup = %d, want 1", len(lifecycle.DeleteRequests))
	}
	assertAllocationRetryCleanup(t, app, allocationID, "")
}

func TestPostgresRunCancelAtomicallyReplacesCreateIntentWithDeleteIntent(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 9, 14, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

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

	lifecycle.CreateErr = errors.New("node create temporarily unavailable")
	app.reconcileV1()

	if _, err := public.CancelRun(context.Background(), &runv1.CancelRunRequest{RunID: runResp.GetRun().GetID()}); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	var lifecycleState, lastError string
	var attempts int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT a.lifecycle_state, q.reconcile_attempts, q.last_error
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		WHERE q.allocation_id = $1
	`, allocationID).Scan(&lifecycleState, &attempts, &lastError); err != nil {
		t.Fatalf("load reconcile queue after cancel delete failure: %v", err)
	}
	if lifecycleState != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String() {
		t.Fatalf("allocation lifecycle = %q, want RELEASING", lifecycleState)
	}
	if attempts != 0 {
		t.Fatalf("delete retry inherited start attempts = %d, want 0", attempts)
	}
	if lastError != "" {
		t.Fatalf("new delete intent inherited a create error: %q", lastError)
	}
	if len(lifecycle.DeleteRequests) != 0 {
		t.Fatalf("cancel request called node delete directly: %d calls", len(lifecycle.DeleteRequests))
	}
}

func TestPostgresAllocationReconcileClaimHasSingleOwnerAndExpires(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 5, 9, 15, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := app.PublicV1Handler().CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/true"}},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	allocationID := runResp.GetRun().GetAllocationID()
	first, err := app.runStore.ClaimDueReconcileItems(context.Background(), "worker-a", 1, now, allocationkernel.ReconcileClaimTTL)
	if err != nil {
		t.Fatalf("ClaimDueReconcileItems(worker-a) error = %v", err)
	}
	if len(first) != 1 || first[0].AllocationID != allocationID || first[0].ClaimOwner != "worker-a" {
		t.Fatalf("worker-a claim = %+v", first)
	}
	second, err := app.runStore.ClaimDueReconcileItems(context.Background(), "worker-b", 1, now, allocationkernel.ReconcileClaimTTL)
	if err != nil {
		t.Fatalf("ClaimDueReconcileItems(worker-b) error = %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("worker-b claimed live worker-a work: %+v", second)
	}
	if held, err := app.runStore.RenewReconcileClaim(context.Background(), allocationID, "worker-b", now, allocationkernel.ReconcileClaimTTL); err != nil || held {
		t.Fatalf("RenewReconcileClaim(wrong owner) = %v, %v", held, err)
	}
	afterExpiry := now.Add(allocationkernel.ReconcileClaimTTL + time.Nanosecond)
	if held, err := app.runStore.RenewReconcileClaim(context.Background(), allocationID, "worker-a", afterExpiry, allocationkernel.ReconcileClaimTTL); err != nil || held {
		t.Fatalf("RenewReconcileClaim(expired owner) = %v, %v", held, err)
	}
	updated, err := app.runStore.ScheduleClaimedReconcile(context.Background(), allocationkernel.ScheduleReconcileRequest{
		AllocationID: allocationID,
		Intent:       allocationkernel.ReconcileIntentEnsurePresent,
		NextRunAt:    afterExpiry,
	}, "worker-a", afterExpiry)
	if err != nil {
		t.Fatalf("ScheduleClaimedReconcile(expired owner) error = %v", err)
	}
	if updated {
		t.Fatal("expired worker mutated an unclaimed intent")
	}
	if err := app.runStore.CompleteAllocationStart(context.Background(), allocationID, "worker-a", nil, afterExpiry); !errors.Is(err, allocationkernel.ErrReconcileClaimLost) {
		t.Fatalf("CompleteAllocationStart(expired owner) error = %v, want claim lost", err)
	}
	second, err = app.runStore.ClaimDueReconcileItems(context.Background(), "worker-b", 1, afterExpiry, allocationkernel.ReconcileClaimTTL)
	if err != nil {
		t.Fatalf("ClaimDueReconcileItems(worker-b after expiry) error = %v", err)
	}
	if len(second) != 1 || second[0].ClaimOwner != "worker-b" {
		t.Fatalf("worker-b expired-claim takeover = %+v", second)
	}
	updated, err = app.runStore.ScheduleClaimedReconcile(context.Background(), allocationkernel.ScheduleReconcileRequest{
		AllocationID: allocationID,
		Intent:       allocationkernel.ReconcileIntentEnsurePresent,
		NextRunAt:    afterExpiry,
	}, "worker-a", afterExpiry)
	if err != nil {
		t.Fatalf("ScheduleClaimedReconcile(stale owner) error = %v", err)
	}
	if updated {
		t.Fatal("stale worker mutated a reclaimed intent")
	}
}

func TestPostgresRunCancelDeleteRetryEventuallyReleasesReservation(t *testing.T) {
	app, lifecycle := newPostgresTestServiceWithConfig(t, Config{
		HeartbeatFreshnessWindow: time.Hour,
		ReconcileInterval:        time.Hour,
	})
	defer app.Close()
	now := time.Date(2026, 5, 10, 11, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    100,
				MemoryBytes: 64 * 1024 * 1024,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	allocationID := runResp.GetRun().GetAllocationID()

	if _, err := public.CancelRun(context.Background(), &runv1.CancelRunRequest{RunID: runResp.GetRun().GetID()}); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, allocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations after delete failure: %v", err)
	}
	if activeReservations != 1 {
		t.Fatalf("active reservations after delete failure = %d, want 1", activeReservations)
	}

	lifecycle.DeleteErr = nil
	if err := app.runReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests = %d, want one durable-intent delivery", len(lifecycle.DeleteRequests))
	}
	if got := lifecycle.DeleteRequests[0].GetAllocationID(); got != allocationID {
		t.Fatalf("retry delete allocation = %q, want %q", got, allocationID)
	}
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, allocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations after delete retry success: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations after delete retry success = %d, want 0", activeReservations)
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
}

func TestPostgresRunKernelCancelRevokesLeaseAndReleasesReservation(t *testing.T) {
	app, lifecycle := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 24, 9, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    100,
				MemoryBytes: 64 * 1024 * 1024,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	leaseResp, err := app.runStore.IssueExecutionLease(context.Background(), runResp.GetRun().GetAllocationID(), 30*time.Second, now)
	if err != nil {
		t.Fatalf("AcquireRunLease() error = %v", err)
	}
	if leaseResp.PlaintextToken == "" {
		t.Fatal("AcquireRunLease() returned empty plaintext token")
	}

	if _, err := public.CancelRun(context.Background(), &runv1.CancelRunRequest{RunID: runResp.GetRun().GetID()}); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != 0 {
		t.Fatalf("cancel request called node delete directly: %d calls", len(lifecycle.DeleteRequests))
	}
	leases, revision, err := app.runStore.WatchExecutionLeases(context.Background(), "node-a", 0, now)
	if err != nil {
		t.Fatalf("WatchExecutionLeases() error = %v", err)
	}
	if revision < 2 {
		t.Fatalf("lease revision = %d, want at least 2 after acquire+revoke", revision)
	}
	var revoked bool
	for _, lease := range leases {
		if lease.LeaseID == leaseResp.LeaseID {
			revoked = lease.Revoked
			if lease.ValidationTokenHash == "" {
				t.Fatal("watch path did not return validation token hash")
			}
		}
	}
	if !revoked {
		t.Fatal("cancelled run lease was not revoked")
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, runResp.GetRun().GetAllocationID()).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations: %v", err)
	}
	if activeReservations != 1 {
		t.Fatalf("active reservations before durable delete delivery = %d, want 1", activeReservations)
	}
	if err := app.runReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending(delete intent) error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests after reconcile = %d, want 1", len(lifecycle.DeleteRequests))
	}
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, runResp.GetRun().GetAllocationID()).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations after reconcile: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations after durable delete delivery = %d, want 0", activeReservations)
	}
	assertPostgresConsistencyOK(t, app)
}

func TestPostgresRunStartingAllocationInActiveInventoryDoesNotFail(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 4, 26, 11, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	node := app.NodeV1Handler()

	registerReadyNode(t, app, "node-a", now)
	env := createDefaultEnvironment(t, app)
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	now = now.Add(10 * time.Second)
	summary := controldtest.ReadySummary(now)
	summary.Components.Axnoded.RunningContainers = 0
	summary.Components.Axnoded.RunningAllocationIds = nil
	summary.Components.Axnoded.ActiveAllocationIds = []string{runResp.GetRun().GetAllocationID()}
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{
		NodeID:        "node-a",
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
		Summary:       summary,
	}); err != nil {
		t.Fatalf("ReportNode(starting inventory) error = %v", err)
	}

	gotRun, err := public.GetRun(context.Background(), &runv1.GetRunRequest{RunID: runResp.GetRun().GetID()})
	if err != nil {
		t.Fatalf("GetRun(after starting inventory) error = %v", err)
	}
	if gotRun.GetRun().GetStatus() == runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("run status after starting inventory = %v, want not FAILED", gotRun.GetRun().GetStatus())
	}
	if gotRun.GetRun().GetMessage() == "allocation missing from node inventory" {
		t.Fatalf("run message after starting inventory = %q, want no inventory-missing failure", gotRun.GetRun().GetMessage())
	}
}
