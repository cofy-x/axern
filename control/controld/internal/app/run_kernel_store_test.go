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
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestPostgresRunKernelEnvironmentLabelsDoNotAffectDedupe(t *testing.T) {
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
	if first.GetEnvironment().GetID() != second.GetEnvironment().GetID() {
		t.Fatalf("environment ids differ: %q != %q", first.GetEnvironment().GetID(), second.GetEnvironment().GetID())
	}
	if first.GetEnvironment().GetSpecHash() != second.GetEnvironment().GetSpecHash() {
		t.Fatalf("spec hashes differ: %q != %q", first.GetEnvironment().GetSpecHash(), second.GetEnvironment().GetSpecHash())
	}
}

func TestPostgresRunStaleNodeHeartbeatFailsRunAndReleasesReservation(t *testing.T) {
	app, _ := newPostgresTestService(t)
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
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, allocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations for unavailable node run = %d, want 0", activeReservations)
	}
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
		WHERE allocation_id = $1 AND reason = $2
	`, allocationID, allocationkernel.ReconcileReasonCreate).Scan(&attempts, &lastError, &nextRunAt); err != nil {
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

func TestPostgresRunCreateRetryExhaustionReleasesReservation(t *testing.T) {
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
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, allocationID).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations for failed run: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations for exhausted run create retry = %d, want 0", activeReservations)
	}
	var queueItems int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = $1
	`, allocationID).Scan(&queueItems); err != nil {
		t.Fatalf("count reconcile queue after run create retry exhaustion: %v", err)
	}
	if queueItems != 0 {
		t.Fatalf("reconcile queue items after run create retry exhaustion = %d, want 0", queueItems)
	}
}

func TestPostgresRunCancelDeleteRetryResetsStartAttemptsAndRecordsError(t *testing.T) {
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

	lifecycle.DeleteErr = errors.New("node delete temporarily unavailable")
	if _, err := public.CancelRun(context.Background(), &runv1.CancelRunRequest{RunID: runResp.GetRun().GetID()}); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	var reason, lastError string
	var attempts int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT reason, reconcile_attempts, last_error
		FROM allocation_reconcile_queue
		WHERE allocation_id = $1
	`, allocationID).Scan(&reason, &attempts, &lastError); err != nil {
		t.Fatalf("load reconcile queue after cancel delete failure: %v", err)
	}
	if reason != allocationkernel.ReconcileReasonDelete {
		t.Fatalf("reconcile reason = %q, want %q", reason, allocationkernel.ReconcileReasonDelete)
	}
	if attempts != 0 {
		t.Fatalf("delete retry inherited start attempts = %d, want 0", attempts)
	}
	if lastError != "node delete temporarily unavailable" {
		t.Fatalf("last error = %q, want node delete temporarily unavailable", lastError)
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

	lifecycle.DeleteErr = errors.New("node delete temporarily unavailable")
	if _, err := public.CancelRun(context.Background(), &runv1.CancelRunRequest{RunID: runResp.GetRun().GetID()}); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
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
	if len(lifecycle.DeleteRequests) != 2 {
		t.Fatalf("delete requests = %d, want initial request plus retry", len(lifecycle.DeleteRequests))
	}
	if got := lifecycle.DeleteRequests[1].GetAllocationID(); got != allocationID {
		t.Fatalf("retry delete allocation = %q, want %q", got, allocationID)
	}
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
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
	leaseResp, err := app.runStore.IssueExecutionLease(context.Background(), runResp.GetRun().GetAllocationID(), runResp.GetRun().GetAttempt(), commonv1.LeaseType_LEASE_TYPE_RUN, 30*time.Second, now)
	if err != nil {
		t.Fatalf("AcquireRunLease() error = %v", err)
	}
	if leaseResp.GetPlaintextToken() == "" {
		t.Fatal("AcquireRunLease() returned empty plaintext token")
	}

	if _, err := public.CancelRun(context.Background(), &runv1.CancelRunRequest{RunID: runResp.GetRun().GetID()}); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	if len(lifecycle.DeleteRequests) != 1 {
		t.Fatalf("delete requests = %d, want 1", len(lifecycle.DeleteRequests))
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
		if lease.GetLeaseID() == leaseResp.GetLeaseID() {
			revoked = lease.GetRevoked()
			if lease.GetPlaintextToken() != "" {
				t.Fatal("watch path leaked plaintext token")
			}
			if lease.GetValidationTokenHash() == "" {
				t.Fatal("watch path did not return validation token hash")
			}
		}
	}
	if !revoked {
		t.Fatal("cancelled run lease was not revoked")
	}
	var activeReservations int
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = $1 AND released_at IS NULL
	`, runResp.GetRun().GetAllocationID()).Scan(&activeReservations); err != nil {
		t.Fatalf("count active reservations: %v", err)
	}
	if activeReservations != 0 {
		t.Fatalf("active reservations = %d, want 0", activeReservations)
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
		Runtimes:      []string{"runsc"},
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
