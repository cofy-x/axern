package pgconsistency

import (
	"context"
	"os"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

func TestSnapshotReportsActiveDependentsOnEndedAllocation(t *testing.T) {
	db := openConsistencyTestDB(t)
	defer db.Close()

	now := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	insertConsistencyAllocation(t, db, "alloc-ended", allocationkernel.OwnerService, "svc-ended", commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(), now)
	insertConsistencyReservation(t, db, "resv-ended", "alloc-ended", allocationkernel.OwnerService, "svc-ended", now)
	insertConsistencyLease(t, db, "lease-ended", "alloc-ended", now, now.Add(time.Hour))
	insertConsistencyTunnel(t, db, "tun-ended", "alloc-ended", tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(), now, now.Add(time.Hour))
	insertConsistencyService(t, db, "svc-ended", "alloc-ended", now)

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
		"active_reservation_on_ended_allocation": false,
		"active_lease_on_ended_allocation":       false,
		"active_tunnel_on_ended_allocation":      false,
		"service_reference_ended_allocation":     false,
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
	insertConsistencyAllocation(t, db, "alloc-ok", allocationkernel.OwnerRun, "run-ok", commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(), now)
	insertReleasedConsistencyReservation(t, db, "resv-ok", "alloc-ok", allocationkernel.OwnerRun, "run-ok", now)
	insertConsistencyLeaseRevoked(t, db, "lease-ok", "alloc-ok", now, now.Add(time.Hour))
	insertConsistencyTunnelRevoked(t, db, "tun-ok", "alloc-ok", tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(), now, now.Add(time.Hour))
	insertConsistencyServiceWithStatus(t, db, "svc-deleted", "alloc-ok", servicev1.ServiceStatus_SERVICE_STATUS_DELETED.String(), now)

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

func insertConsistencyAllocation(t *testing.T, db *postgres.DB, allocationID, ownerType, ownerID, status string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (
			allocation_id, owner_type, owner_id, environment_id, node_id, attempt, status,
			config, version, created_at, updated_at, exit_code, exit_code_known, message
		) VALUES ($1, $2, $3, 'env-test', 'node-test', 1, $4, '{}'::jsonb, 1, $5, $5, 0, false, '')
	`, allocationID, ownerType, ownerID, status, now.UTC()); err != nil {
		t.Fatalf("insert allocation: %v", err)
	}
}

func insertConsistencyReservation(t *testing.T, db *postgres.DB, reservationID, allocationID, ownerType, ownerID string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO workload_reservations (
			reservation_id, allocation_id, namespace, owner_type, owner_id, node_id,
			cpu_milli, memory_bytes, created_at, released_at
		) VALUES ($1, $2, 'default', $3, $4, 'node-test', 500, 536870912, $5, NULL)
	`, reservationID, allocationID, ownerType, ownerID, now.UTC()); err != nil {
		t.Fatalf("insert reservation: %v", err)
	}
}

func insertReleasedConsistencyReservation(t *testing.T, db *postgres.DB, reservationID, allocationID, ownerType, ownerID string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO workload_reservations (
			reservation_id, allocation_id, namespace, owner_type, owner_id, node_id,
			cpu_milli, memory_bytes, created_at, released_at
		) VALUES ($1, $2, 'default', $3, $4, 'node-test', 500, 536870912, $5, $5)
	`, reservationID, allocationID, ownerType, ownerID, now.UTC()); err != nil {
		t.Fatalf("insert released reservation: %v", err)
	}
}

func insertConsistencyLease(t *testing.T, db *postgres.DB, leaseID, allocationID string, createdAt, expiresAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, attempt, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ($1, $2, 'node-test', '127.0.0.1:24010', 1, 'LEASE_TYPE_RUN', $3, 1, FALSE, 'hash', $4)
	`, leaseID, allocationID, expiresAt.UTC(), createdAt.UTC()); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
}

func insertConsistencyLeaseRevoked(t *testing.T, db *postgres.DB, leaseID, allocationID string, createdAt, expiresAt time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, attempt, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ($1, $2, 'node-test', '127.0.0.1:24010', 1, 'LEASE_TYPE_RUN', $3, 1, TRUE, 'hash', $4)
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
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO tunnel_sessions (
			session_id, allocation_id, node_id, node_target, attempt, remote_port,
			local_target, edge_target, node_edge_target, status, reason, bound_addr, revoked,
			client_token_hash, node_token_encrypted, node_token_hash, revision, created_at, updated_at, expires_at
		) VALUES ($1, $2, 'node-test', '127.0.0.1:24010', 1, 30001,
			'127.0.0.1:8080', '127.0.0.1:24210', '127.0.0.1:24210', $3, '', '', $4,
			'client-hash', $5, 'node-hash', 0, $6, $6, $7)
	`, sessionID, allocationID, status, revoked, []byte("node-token"), createdAt.UTC(), expiresAt.UTC()); err != nil {
		t.Fatalf("insert tunnel session: %v", err)
	}
}

func insertConsistencyService(t *testing.T, db *postgres.DB, serviceID, allocationID string, now time.Time) {
	t.Helper()
	insertConsistencyServiceWithStatus(t, db, serviceID, allocationID, servicev1.ServiceStatus_SERVICE_STATUS_READY.String(), now)
}

func insertConsistencyServiceWithStatus(t *testing.T, db *postgres.DB, serviceID, allocationID, status string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO services (
			service_id, namespace, environment_id, replicas, status, config, allocation_ids,
			labels, created_at, updated_at
		) VALUES ($1, 'default', 'env-test', 1, $2, '{}'::jsonb, jsonb_build_array($3::text), '{}'::jsonb, $4, $4)
	`, serviceID, status, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert service: %v", err)
	}
}
