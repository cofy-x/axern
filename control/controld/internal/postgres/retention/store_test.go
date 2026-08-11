package pgretention

import (
	"context"
	"os"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	retention "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

func TestPGStoreCleanupServiceEventsKeepsRecentPerService(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	insertService(t, db, "svc-events", now)
	for i := 0; i < 5; i++ {
		insertServiceEvent(t, db, "evt-old-"+string(rune('a'+i)), "svc-events", now.Add(-2*time.Hour+time.Duration(i)*time.Minute))
	}

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:           true,
		BatchSize:         10,
		ServiceEventsTTL:  time.Hour,
		ServiceEventsKeep: 2,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ServiceEventsDeleted != 3 {
		t.Fatalf("service events deleted = %d, want 3", result.ServiceEventsDeleted)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM service_events WHERE service_id = 'svc-events'`); got != 2 {
		t.Fatalf("remaining service events = %d, want 2", got)
	}
}

func TestPGStoreCleanupTunnelEventsKeepsRecentPerSession(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	insertAllocation(t, db, allocationRow{ID: "alloc-tunnel-events", OwnerType: allocationkernel.OwnerRun, OwnerID: "run-tunnel-events", Status: "ALLOCATION_STATUS_RUNNING", UpdatedAt: now})
	insertTunnelSession(t, db, "tun-events", "alloc-tunnel-events", "TUNNEL_SESSION_STATUS_RUNNING", now.Add(-2*time.Hour), now.Add(time.Hour))
	for i := 0; i < 5; i++ {
		insertTunnelEvent(t, db, "tun-events", now.Add(-2*time.Hour+time.Duration(i)*time.Minute))
	}

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:          true,
		BatchSize:        10,
		TunnelEventsTTL:  time.Hour,
		TunnelEventsKeep: 2,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.TunnelEventsDeleted != 3 {
		t.Fatalf("tunnel events deleted = %d, want 3", result.TunnelEventsDeleted)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM tunnel_session_events WHERE session_id = 'tun-events'`); got != 2 {
		t.Fatalf("remaining tunnel events = %d, want 2", got)
	}
}

func TestPGStoreCleanupServiceAllocationsKeepsCurrentAndRecentHistory(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	insertService(t, db, "svc-allocs", now)
	setServiceAllocationIDs(t, db, "svc-allocs", `["alloc-current"]`)
	insertAllocation(t, db, allocationRow{ID: "alloc-current", OwnerType: allocationkernel.OwnerService, OwnerID: "svc-allocs", Status: "ALLOCATION_STATUS_RUNNING", UpdatedAt: now.Add(-48 * time.Hour)})
	insertAllocation(t, db, allocationRow{ID: "alloc-old-a", OwnerType: allocationkernel.OwnerService, OwnerID: "svc-allocs", Status: "ALLOCATION_STATUS_EXITED", UpdatedAt: now.Add(-48 * time.Hour)})
	insertAllocation(t, db, allocationRow{ID: "alloc-old-b", OwnerType: allocationkernel.OwnerService, OwnerID: "svc-allocs", Status: "ALLOCATION_STATUS_FAILED", UpdatedAt: now.Add(-47 * time.Hour)})
	insertAllocation(t, db, allocationRow{ID: "alloc-recent", OwnerType: allocationkernel.OwnerService, OwnerID: "svc-allocs", Status: "ALLOCATION_STATUS_EXITED", UpdatedAt: now.Add(-46 * time.Hour)})
	insertAllocationDependents(t, db, "alloc-old-a", now.Add(-48*time.Hour))

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:             true,
		BatchSize:           10,
		ServiceReplicasTTL:  time.Hour,
		ServiceReplicasKeep: 1,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ServiceAllocationsDeleted != 2 {
		t.Fatalf("service allocations deleted = %d, want 2", result.ServiceAllocationsDeleted)
	}
	for _, id := range []string{"alloc-current", "alloc-recent"} {
		if got := countRows(t, db, `SELECT COUNT(*) FROM allocations WHERE allocation_id = $1`, id); got != 1 {
			t.Fatalf("allocation %s count = %d, want 1", id, got)
		}
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM allocations WHERE allocation_id IN ('alloc-old-a', 'alloc-old-b')`); got != 0 {
		t.Fatalf("old service allocations remaining = %d, want 0", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM workload_reservations WHERE allocation_id = 'alloc-old-a'`); got != 0 {
		t.Fatalf("dependent reservations remaining = %d, want 0", got)
	}
}

func TestPGStoreCleanupServiceAllocationsSkipsReconcileQueuedAllocations(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	insertService(t, db, "svc-queued", now)
	insertAllocation(t, db, allocationRow{ID: "alloc-queued", OwnerType: allocationkernel.OwnerService, OwnerID: "svc-queued", Status: "ALLOCATION_STATUS_FAILED", UpdatedAt: now.Add(-48 * time.Hour)})
	insertReconcileQueueItem(t, db, "alloc-queued", allocationkernel.ReconcileReasonCreate, now.Add(-48*time.Hour))

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:             true,
		BatchSize:           10,
		ServiceReplicasTTL:  time.Hour,
		ServiceReplicasKeep: 0,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ServiceAllocationsDeleted != 0 {
		t.Fatalf("service allocations deleted = %d, want 0", result.ServiceAllocationsDeleted)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM allocations WHERE allocation_id = 'alloc-queued'`); got != 1 {
		t.Fatalf("queued allocation count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = 'alloc-queued'`); got != 1 {
		t.Fatalf("queued reconcile item count = %d, want 1", got)
	}
}

func TestPGStoreCleanupServiceAllocationsRevokesActiveTunnelsBeforeDelete(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	insertService(t, db, "svc-tunnel", now)
	insertAllocation(t, db, allocationRow{ID: "alloc-tunnel-old", OwnerType: allocationkernel.OwnerService, OwnerID: "svc-tunnel", Status: "ALLOCATION_STATUS_EXITED", UpdatedAt: now.Add(-48 * time.Hour)})
	insertTunnelSession(t, db, "tun-retention", "alloc-tunnel-old", "TUNNEL_SESSION_STATUS_RUNNING", now.Add(-48*time.Hour), now.Add(time.Hour))

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:             true,
		BatchSize:           10,
		ServiceReplicasTTL:  time.Hour,
		ServiceReplicasKeep: 0,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ServiceAllocationsDeleted != 1 {
		t.Fatalf("service allocations deleted = %d, want 1", result.ServiceAllocationsDeleted)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM allocations WHERE allocation_id = 'alloc-tunnel-old'`); got != 0 {
		t.Fatalf("deleted allocation count = %d, want 0", got)
	}
	if got := countRows(t, db, `
		SELECT COUNT(*)
		FROM tunnel_sessions
		WHERE session_id = 'tun-retention'
		  AND status = 'TUNNEL_SESSION_STATUS_REVOKED'
		  AND revoked = TRUE
		  AND revision > 0
	`); got != 1 {
		t.Fatalf("revoked tunnel count = %d, want 1", got)
	}
}

func TestPGStoreCleanupTerminalRunsAndLeases(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	insertRun(t, db, "run-old", "alloc-run-old", "RUN_STATUS_SUCCEEDED", now.Add(-8*24*time.Hour))
	insertAllocation(t, db, allocationRow{ID: "alloc-run-old", OwnerType: allocationkernel.OwnerRun, OwnerID: "run-old", Status: "ALLOCATION_STATUS_EXITED", UpdatedAt: now.Add(-8 * 24 * time.Hour)})
	insertAllocationDependents(t, db, "alloc-run-old", now.Add(-8*24*time.Hour))
	insertRun(t, db, "run-active", "alloc-run-active", "RUN_STATUS_RUNNING", now.Add(-8*24*time.Hour))
	insertAllocation(t, db, allocationRow{ID: "alloc-run-active", OwnerType: allocationkernel.OwnerRun, OwnerID: "run-active", Status: "ALLOCATION_STATUS_RUNNING", UpdatedAt: now.Add(-8 * 24 * time.Hour)})
	insertLease(t, db, "lease-expired", "alloc-run-active", now.Add(-48*time.Hour), now.Add(-47*time.Hour), false)
	insertLease(t, db, "lease-active", "alloc-run-active", now.Add(-48*time.Hour), now.Add(24*time.Hour), false)
	insertLease(t, db, "lease-revoked", "alloc-run-active", now.Add(-48*time.Hour), now.Add(24*time.Hour), true)

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:         true,
		BatchSize:       10,
		TerminalRunsTTL: 7 * 24 * time.Hour,
		LeasesTTL:       24 * time.Hour,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.TerminalRunsDeleted != 1 {
		t.Fatalf("terminal runs deleted = %d, want 1", result.TerminalRunsDeleted)
	}
	if result.LeasesDeleted != 2 {
		t.Fatalf("leases deleted = %d, want 2", result.LeasesDeleted)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM runs WHERE run_id = 'run-old'`); got != 0 {
		t.Fatalf("old terminal run count = %d, want 0", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM runs WHERE run_id = 'run-active'`); got != 1 {
		t.Fatalf("active run count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM execution_leases WHERE lease_id = 'lease-active'`); got != 1 {
		t.Fatalf("active lease count = %d, want 1", got)
	}
}

func TestPGStoreCleanupTerminalRunsSkipsReconcileQueuedAllocations(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 10, 9, 30, 0, 0, time.UTC)
	insertRun(t, db, "run-queued", "alloc-run-queued", "RUN_STATUS_FAILED", now.Add(-8*24*time.Hour))
	insertAllocation(t, db, allocationRow{ID: "alloc-run-queued", OwnerType: allocationkernel.OwnerRun, OwnerID: "run-queued", Status: "ALLOCATION_STATUS_FAILED", UpdatedAt: now.Add(-8 * 24 * time.Hour)})
	insertReconcileQueueItem(t, db, "alloc-run-queued", allocationkernel.ReconcileReasonDelete, now.Add(-8*24*time.Hour))

	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:         true,
		BatchSize:       10,
		TerminalRunsTTL: 7 * 24 * time.Hour,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.TerminalRunsDeleted != 0 {
		t.Fatalf("terminal runs deleted = %d, want 0", result.TerminalRunsDeleted)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM runs WHERE run_id = 'run-queued'`); got != 1 {
		t.Fatalf("queued run count = %d, want 1", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id = 'alloc-run-queued'`); got != 1 {
		t.Fatalf("queued reconcile item count = %d, want 1", got)
	}
}

func TestPGStoreCleanupHonorsBatchSize(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	insertService(t, db, "svc-batch", now)
	for i := 0; i < 4; i++ {
		insertServiceEvent(t, db, "evt-batch-"+string(rune('a'+i)), "svc-batch", now.Add(-2*time.Hour+time.Duration(i)*time.Minute))
	}
	insertAllocation(t, db, allocationRow{ID: "alloc-tunnel-batch", OwnerType: allocationkernel.OwnerRun, OwnerID: "run-tunnel-batch", Status: "ALLOCATION_STATUS_RUNNING", UpdatedAt: now})
	insertTunnelSession(t, db, "tun-batch", "alloc-tunnel-batch", "TUNNEL_SESSION_STATUS_RUNNING", now.Add(-2*time.Hour), now.Add(time.Hour))
	for i := 0; i < 4; i++ {
		insertTunnelEvent(t, db, "tun-batch", now.Add(-2*time.Hour+time.Duration(i)*time.Minute))
	}
	result, err := store.Cleanup(ctx, retention.Config{
		Enabled:           true,
		BatchSize:         2,
		ServiceEventsTTL:  time.Hour,
		ServiceEventsKeep: 0,
		TunnelEventsTTL:   time.Hour,
		TunnelEventsKeep:  0,
	}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if result.ServiceEventsDeleted != 2 {
		t.Fatalf("service events deleted = %d, want 2", result.ServiceEventsDeleted)
	}
	if result.TunnelEventsDeleted != 2 {
		t.Fatalf("tunnel events deleted = %d, want 2", result.TunnelEventsDeleted)
	}
}

func TestPGStoreCleanupSkipsWhenAdvisoryLockHeld(t *testing.T) {
	db := newTestDB(t)
	store := NewPGStore(db)
	ctx := context.Background()
	now := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin lock tx: %v", err)
	}
	defer tx.Rollback(ctx)
	var locked bool
	if err := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, advisoryLockID).Scan(&locked); err != nil {
		t.Fatalf("acquire advisory lock: %v", err)
	}
	if !locked {
		t.Fatal("test could not acquire advisory lock")
	}
	result, err := store.Cleanup(ctx, retention.Config{Enabled: true, BatchSize: 10}, now)
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !result.Skipped {
		t.Fatal("Cleanup() skipped = false, want true")
	}
}

func newTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}
	truncateRetentionTestTables(t, db)
	return db
}

func truncateRetentionTestTables(t *testing.T, db *postgres.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		TRUNCATE TABLE
			admin_audit_events,
			tunnel_sessions,
			allocation_reconcile_queue,
			execution_leases,
			workload_reservations,
			allocations,
			runs,
			service_events,
			services,
			namespace_resource_quotas,
			namespaces
		CASCADE
	`); err != nil {
		t.Fatalf("truncate retention test tables: %v", err)
	}
}

func insertService(t *testing.T, db *postgres.DB, serviceID string, now time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(), `
		INSERT INTO services (
			service_id, namespace, environment_id, replicas, ready_replicas, unhealthy_replicas,
			status, config, allocation_ids, labels, version, created_at, updated_at, message
		) VALUES ($1, 'default', 'env-test', 1, 0, 0, 'SERVICE_STATUS_READY', '{}'::jsonb, '[]'::jsonb, '{}'::jsonb, 1, $2, $2, '')
	`, serviceID, now.UTC())
	if err != nil {
		t.Fatalf("insert service: %v", err)
	}
}

func setServiceAllocationIDs(t *testing.T, db *postgres.DB, serviceID, allocationIDsJSON string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE services SET allocation_ids = $2::jsonb WHERE service_id = $1
	`, serviceID, allocationIDsJSON); err != nil {
		t.Fatalf("set service allocation ids: %v", err)
	}
}

func insertServiceEvent(t *testing.T, db *postgres.DB, eventID, serviceID string, createdAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO service_events (
			event_id, service_id, replica_id, event_type, phase, diagnostic_code, message, created_at
		) VALUES ($1, $2, '', 'SERVICE_EVENT_TYPE_SERVICE_DEGRADED', 'SERVICE_ROLLOUT_PHASE_UNSPECIFIED', 'WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED', '', $3)
	`, eventID, serviceID, createdAt.UTC()); err != nil {
		t.Fatalf("insert service event: %v", err)
	}
}

type allocationRow struct {
	ID        string
	OwnerType string
	OwnerID   string
	Status    string
	UpdatedAt time.Time
}

func insertAllocation(t *testing.T, db *postgres.DB, row allocationRow) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (
			allocation_id, owner_type, owner_id, environment_id, node_id, attempt, status,
			config, version, created_at, updated_at, exit_code, exit_code_known, message
		) VALUES ($1, $2, $3, 'env-test', 'node-test', 1, $4, '{}'::jsonb, 1, $5, $5, 0, false, '')
	`, row.ID, row.OwnerType, row.OwnerID, row.Status, row.UpdatedAt.UTC()); err != nil {
		t.Fatalf("insert allocation: %v", err)
	}
}

func insertAllocationDependents(t *testing.T, db *postgres.DB, allocationID string, now time.Time) {
	t.Helper()
	insertLease(t, db, "lease-"+allocationID, allocationID, now, now.Add(-time.Hour), true)
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO workload_reservations (
			reservation_id, allocation_id, namespace, owner_type, owner_id, node_id,
			cpu_milli, sandbox_memory_request_bytes, created_at, released_at
		) VALUES ($1, $2, 'default', 'run', 'run-test', 'node-test', 500, 4294967296, $3, $3)
	`, "resv-"+allocationID, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert node reservation: %v", err)
	}
}

func insertReconcileQueueItem(t *testing.T, db *postgres.DB, allocationID, reason string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocation_reconcile_queue (
			allocation_id, reason, next_run_at, created_at, updated_at
		) VALUES ($1, $2, $3, $3, $3)
	`, allocationID, reason, now.UTC()); err != nil {
		t.Fatalf("insert reconcile queue item: %v", err)
	}
}

func insertRun(t *testing.T, db *postgres.DB, runID, allocationID, status string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (
			run_id, namespace, environment_id, allocation_id, attempt, status,
			config, labels, version, created_at, updated_at, exit_code, exit_code_known, message
		) VALUES ($1, 'default', 'env-test', $2, 1, $3, '{}'::jsonb, '{}'::jsonb, 1, $4, $4, 0, false, '')
	`, runID, allocationID, status, now.UTC()); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func insertLease(t *testing.T, db *postgres.DB, leaseID, allocationID string, createdAt, expiresAt time.Time, revoked bool) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, attempt, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ($1, $2, 'node-test', '127.0.0.1:24010', 1, 'LEASE_TYPE_RUN', $3, 1, $4, 'hash', $5)
	`, leaseID, allocationID, expiresAt.UTC(), revoked, createdAt.UTC()); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
}

func insertTunnelSession(t *testing.T, db *postgres.DB, sessionID, allocationID, status string, createdAt, expiresAt time.Time) {
	t.Helper()
	ensureRetentionTunnelIdentity(t, db, createdAt)
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO tunnel_sessions (
			session_id, allocation_id, namespace, creator_principal_id, node_id, node_target, attempt, remote_port,
			local_target, edge_target, node_edge_target, status, reason, bound_addr, revoked,
			client_token_hash, node_token_encrypted, node_token_hash, revision, created_at, updated_at, expires_at
		) VALUES ($1, $2, 'default', 'prn-retention-test', 'node-test', '127.0.0.1:24010', 1, 30001,
			'127.0.0.1:8080', '127.0.0.1:24210', '127.0.0.1:24210', $3, '', '', FALSE,
			'client-hash', decode('00', 'hex'), 'node-hash', 0, $4, $4, $5)
	`, sessionID, allocationID, status, createdAt.UTC(), expiresAt.UTC()); err != nil {
		t.Fatalf("insert tunnel session: %v", err)
	}
}

func ensureRetentionTunnelIdentity(t *testing.T, db *postgres.DB, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO namespaces(namespace, version, created_at, updated_at)
		VALUES ('default', 1, $1, $1)
		ON CONFLICT (namespace) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert tunnel namespace fixture: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO principals(principal_id, name, display_name, kind, status, version, created_at, updated_at)
		VALUES ('prn-retention-test', 'retention-test', 'Retention Test', 'human', 'active', 1, $1, $1)
		ON CONFLICT (principal_id) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert tunnel principal fixture: %v", err)
	}
}

func insertTunnelEvent(t *testing.T, db *postgres.DB, sessionID string, createdAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO tunnel_session_events (
			session_id, event_type, status, reason, bound_addr, created_at
		) VALUES ($1, 'TUNNEL_SESSION_EVENT_TYPE_NODE_STATUS', 'TUNNEL_SESSION_STATUS_RUNNING', '', '', $2)
	`, sessionID, createdAt.UTC()); err != nil {
		t.Fatalf("insert tunnel event: %v", err)
	}
}

func countRows(t *testing.T, db *postgres.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.Pool().QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}
