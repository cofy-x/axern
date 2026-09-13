package pgconsistency

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

func TestSnapshotReportsActiveDependentsOnEndedAllocation(t *testing.T) {
	db := openConsistencyTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	insertConsistencyAllocation(t, db, "alloc-ended", "run-ended", commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(), now)
	insertConsistencyReservation(t, db, "alloc-ended", now)
	insertConsistencyLease(t, db, "lease-ended", "alloc-ended", now, now.Add(time.Hour))
	insertConsistencyTunnel(t, db, "tun-ended", "alloc-ended", tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(), now, now.Add(time.Hour))

	snapshot, err := Snapshot(context.Background(), db.Pool(), now)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Status != "inconsistent" {
		t.Fatalf("status = %q, want inconsistent", snapshot.Status)
	}
	if snapshot.Counts.ActiveReservations != 1 || snapshot.Counts.ActiveLeases != 1 || snapshot.Counts.ActiveTunnels != 1 {
		t.Fatalf("unexpected counts: %+v", snapshot.Counts)
	}
	wantCodes := map[string]bool{
		"active_reservation_on_released_allocation": false,
		"active_lease_on_ended_allocation":          false,
		"active_tunnel_on_ended_allocation":         false,
	}
	for _, issue := range snapshot.Issues {
		if _, ok := wantCodes[string(issue.Code)]; ok {
			wantCodes[string(issue.Code)] = true
		}
		if issue.Code == "active_tunnel_on_ended_allocation" && issue.DependentID != "tun-ended" {
			t.Fatalf("tunnel issue dependent id = %q, want tun-ended", issue.DependentID)
		}
	}
	for code, found := range wantCodes {
		if !found {
			t.Fatalf("missing issue code %q in %+v", code, snapshot.Issues)
		}
	}
}

func TestSnapshotReportsOKForReleasedDependents(t *testing.T) {
	db := openConsistencyTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	insertConsistencyAllocation(t, db, "alloc-ok", "run-ok", commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(), now)
	insertReleasedConsistencyReservation(t, db, "alloc-ok", now)
	insertConsistencyLeaseRevoked(t, db, "lease-ok", "alloc-ok", now, now.Add(time.Hour))
	insertConsistencyTunnelRevoked(t, db, "tun-ok", "alloc-ok", tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(), now, now.Add(time.Hour))

	snapshot, err := Snapshot(context.Background(), db.Pool(), now)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Status != "ok" {
		t.Fatalf("status = %q, want ok; issues=%+v", snapshot.Status, snapshot.Issues)
	}
	if snapshot.Counts.Issues != 0 {
		t.Fatalf("issues count = %d, want 0", snapshot.Counts.Issues)
	}
}

func TestSnapshotAllowsReservationWhileAllocationIsReleasing(t *testing.T) {
	db := openConsistencyTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 9, 30, 0, 0, time.UTC)
	insertConsistencyAllocation(t, db, "alloc-releasing", "run-releasing", commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(), now)
	insertConsistencyReservation(t, db, "alloc-releasing", now)

	snapshot, err := Snapshot(context.Background(), db.Pool(), now)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Status != "ok" || snapshot.Counts.Issues != 0 {
		t.Fatalf("releasing allocation snapshot = %+v, want active reservation without consistency issue", snapshot)
	}
	if snapshot.Counts.ActiveReservations != 1 {
		t.Fatalf("active reservations = %d, want 1", snapshot.Counts.ActiveReservations)
	}
}

func openConsistencyTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres test db: %v", err)
	}
	return db
}

func insertConsistencyAllocation(t *testing.T, db *postgres.DB, allocationID, runID, status string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (node_id, node_target, registered_at, updated_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-test', '127.0.0.1:24010', $1, $1, $1, 'active')
		ON CONFLICT (node_id) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, runID, now.UTC()); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (
			allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at
		) VALUES ($1, $2, 'node-test', $3, $4, $4)
	`, allocationID, runID, status, now.UTC()); err != nil {
		t.Fatalf("insert allocation: %v", err)
	}
}

func insertConsistencyReservation(t *testing.T, db *postgres.DB, allocationID string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO reservations (
			allocation_id, node_id,
			cpu_milli, sandbox_memory_request_bytes, created_at, released_at
		) VALUES ($1, 'node-test', 500, 536870912, $2, NULL)
	`, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert reservation: %v", err)
	}
}

func insertReleasedConsistencyReservation(t *testing.T, db *postgres.DB, allocationID string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO reservations (
			allocation_id, node_id,
			cpu_milli, sandbox_memory_request_bytes, created_at, released_at
		) VALUES ($1, 'node-test', 500, 536870912, $2, $2)
	`, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert released reservation: %v", err)
	}
}

func insertConsistencyLease(t *testing.T, db *postgres.DB, leaseID, allocationID string, createdAt, expiresAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ($1, $2, 'node-test', '127.0.0.1:24010', 'LEASE_TYPE_RUN', $3, 1, FALSE, 'hash', $4)
	`, leaseID, allocationID, expiresAt.UTC(), createdAt.UTC()); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
}

func insertConsistencyLeaseRevoked(t *testing.T, db *postgres.DB, leaseID, allocationID string, createdAt, expiresAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ($1, $2, 'node-test', '127.0.0.1:24010', 'LEASE_TYPE_RUN', $3, 1, TRUE, 'hash', $4)
	`, leaseID, allocationID, expiresAt.UTC(), createdAt.UTC()); err != nil {
		t.Fatalf("insert revoked lease: %v", err)
	}
}

func insertConsistencyTunnel(t *testing.T, db *postgres.DB, sessionID, allocationID, status string, createdAt, expiresAt time.Time) {
	t.Helper()
	insertConsistencyTunnelWithRevoked(t, db, sessionID, allocationID, status, false, createdAt, expiresAt)
}

func insertConsistencyTunnelRevoked(t *testing.T, db *postgres.DB, sessionID, allocationID, status string, createdAt, expiresAt time.Time) {
	t.Helper()
	insertConsistencyTunnelWithRevoked(t, db, sessionID, allocationID, status, true, createdAt, expiresAt)
}

func insertConsistencyTunnelWithRevoked(t *testing.T, db *postgres.DB, sessionID, allocationID, status string, revoked bool, createdAt, expiresAt time.Time) {
	t.Helper()
	ensureConsistencyTunnelIdentity(t, db, createdAt)
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO tunnel_sessions (
			session_id, allocation_id, namespace, creator_principal_id, node_id, node_target, remote_port,
			local_target, edge_target, node_edge_target, status, reason, bound_addr, revoked,
			client_token_hash, node_token_encrypted, node_token_hash, revision, created_at, updated_at, expires_at
		) VALUES ($1, $2, 'default', 'prn-consistency-test', 'node-test', '127.0.0.1:24010', 30001,
			'127.0.0.1:8080', '127.0.0.1:24210', '127.0.0.1:24210', $3, '', '', $4,
			'client-hash', $5, 'node-hash', 0, $6, $6, $7)
	`, sessionID, allocationID, status, revoked, []byte("node-token"), createdAt.UTC(), expiresAt.UTC()); err != nil {
		t.Fatalf("insert tunnel session: %v", err)
	}
}

func ensureConsistencyTunnelIdentity(t *testing.T, db *postgres.DB, now time.Time) {
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
		VALUES ('prn-consistency-test', 'consistency-test', 'Consistency Test', 'human', 'active', 1, $1, $1)
		ON CONFLICT (principal_id) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert tunnel principal fixture: %v", err)
	}
}
