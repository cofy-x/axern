package pgtunnel

import (
	"context"
	"os"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func newTestStore(db *postgres.DB) *Store {
	return NewStore(db, "", "", WithRelays([]Relay{{
		ID:           "test",
		ClientTarget: "127.0.0.1:24210",
		NodeTarget:   "tunneld:24210",
		Weight:       1,
	}}))
}

func TestCreateAllocatesRemotePort(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-auto", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-auto",
		LocalTarget:  "127.0.0.1:8080",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create(auto port) error = %v", err)
	}
	if got := result.Session.GetRemotePort(); got < autoPortMin || got > autoPortMax {
		t.Fatalf("auto remote port = %d, want %d..%d", got, autoPortMin, autoPortMax)
	}
}

func TestCreateUsesExplicitRemotePort(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-explicit", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-explicit",
		RemotePort:   int32Ptr(8786),
		LocalTarget:  "127.0.0.1:8080",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create(explicit port) error = %v", err)
	}
	if got := result.Session.GetRemotePort(); got != 8786 {
		t.Fatalf("remote port = %d, want 8786", got)
	}
}

func TestCreateRejectsExplicitZeroRemotePort(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-zero", now)

	_, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-zero",
		RemotePort:   int32Ptr(0),
		LocalTarget:  "127.0.0.1:8080",
		Now:          now,
	})
	if err == nil {
		t.Fatal("Create(explicit zero port) error = nil, want error")
	}
}

func TestRenewExtendsActiveSession(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Minute,
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	renewed, err := store.Renew(context.Background(), result.Session.GetSessionID(), result.ClientToken, 10*time.Minute, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	want := now.Add(30 * time.Second).Add(10 * time.Minute)
	if !renewed.GetExpiresAt().AsTime().Equal(want) {
		t.Fatalf("expires_at = %s, want %s", renewed.GetExpiresAt().AsTime(), want)
	}
}

func TestRenewRejectsExpiredSession(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew-expired", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew-expired",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Minute,
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err = store.Renew(context.Background(), result.Session.GetSessionID(), result.ClientToken, 10*time.Minute, now.Add(2*time.Minute))
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Renew(expired) code = %s, want %s (err=%v)", grpcstatus.Code(err), codes.FailedPrecondition, err)
	}
}

func TestRenewRejectsRevokedSession(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew-revoked", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew-revoked",
		LocalTarget:  "127.0.0.1:8080",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := store.Revoke(context.Background(), result.Session.GetSessionID(), "test", now.Add(time.Second)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	_, err = store.Renew(context.Background(), result.Session.GetSessionID(), result.ClientToken, 10*time.Minute, now.Add(2*time.Second))
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Renew(revoked) code = %s, want %s (err=%v)", grpcstatus.Code(err), codes.FailedPrecondition, err)
	}
}

func TestRenewRequiresClientToken(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew-token", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew-token",
		LocalTarget:  "127.0.0.1:8080",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = store.Renew(context.Background(), result.Session.GetSessionID(), "", 10*time.Minute, now.Add(time.Second))
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("Renew(empty token) code = %s, want %s (err=%v)", grpcstatus.Code(err), codes.InvalidArgument, err)
	}
	_, err = store.Renew(context.Background(), result.Session.GetSessionID(), "wrong-token", 10*time.Minute, now.Add(2*time.Second))
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("Renew(wrong token) code = %s, want %s (err=%v)", grpcstatus.Code(err), codes.PermissionDenied, err)
	}
}

func TestListEventsTracksTunnelLifecycle(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-events", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-events",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Minute,
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := store.Renew(context.Background(), result.Session.GetSessionID(), result.ClientToken, time.Minute, now.Add(10*time.Second)); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if _, err := store.Revoke(context.Background(), result.Session.GetSessionID(), "test revoke", now.Add(20*time.Second)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	events, err := store.ListEvents(context.Background(), result.Session.GetSessionID(), 10, now.Add(30*time.Second))
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	wantTypes := []tunnelv1.TunnelSessionEventType{
		tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_REVOKED,
		tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_RENEWED,
		tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CREATED,
	}
	wantCodes := []tunnelv1.TunnelSessionEventReasonCode{
		tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_MANUAL_REVOKE,
		tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_RENEWED,
		tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_CREATED,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("event count = %d, want %d", len(events), len(wantTypes))
	}
	for i, want := range wantTypes {
		if got := events[i].GetEventType(); got != want {
			t.Fatalf("event[%d] type = %s, want %s", i, got, want)
		}
		if got := events[i].GetReasonCode(); got != wantCodes[i] {
			t.Fatalf("event[%d] reason code = %s, want %s", i, got, wantCodes[i])
		}
	}
	if got := events[0].GetReason(); got != "test revoke" {
		t.Fatalf("revoke event reason = %q, want %q", got, "test revoke")
	}
}

func TestListEventsRecordsExpiry(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-events-expire", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-events-expire",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Minute,
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	events, err := store.ListEvents(context.Background(), result.Session.GetSessionID(), 10, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ListEvents(expired) error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if got := events[0].GetEventType(); got != tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_EXPIRED {
		t.Fatalf("latest event type = %s, want expired", got)
	}
	if got := events[0].GetStatus(); got != tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED {
		t.Fatalf("latest event status = %s, want expired", got)
	}
	if got := events[0].GetReasonCode(); got != tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_EXPIRED {
		t.Fatalf("latest event reason code = %s, want expired", got)
	}
}

func int32Ptr(v int32) *int32 {
	return &v
}

func tunnelTestContext() context.Context {
	return accesskernel.WithActor(context.Background(), accesskernel.Actor{
		Principal: accesskernel.Principal{ID: "prn-tunnel-test", Status: accesskernel.PrincipalStatusActive},
	})
}

func newTunnelTestDB(t *testing.T) *postgres.DB {
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
	if _, err := db.Pool().Exec(context.Background(), `
		TRUNCATE TABLE principals, namespaces, tunnel_sessions, allocations, nodes CASCADE
	`); err != nil {
		t.Fatalf("truncate tunnel test tables: %v", err)
	}
	return db
}

func insertTunnelTestAllocation(t *testing.T, db *postgres.DB, allocationID string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO principals(principal_id,name,display_name,kind,status,version,created_at,updated_at)
		VALUES ('prn-tunnel-test','tunnel-test','Tunnel Test','human','active',1,$1,$1)
		ON CONFLICT (principal_id) DO NOTHING;
		INSERT INTO namespaces(namespace,version,created_at,updated_at)
		VALUES ('default',1,$1,$1)
		ON CONFLICT (namespace) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert tunnel access fixtures: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (
			node_id, node_target, registered_at, updated_at, last_heartbeat_at, last_summary_at, node_auth_token_hash, lifecycle_status
		) VALUES ('node-test', '127.0.0.1:25000', $1, $1, $1, $1, 'hash', 'active')
	`, now.UTC()); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (
			allocation_id, owner_type, owner_id, environment_id, node_id, attempt, status,
			config, version, created_at, updated_at, exit_code, exit_code_known, message
		) VALUES ($1, 'run', 'run-test', 'env-test', 'node-test', 1, 'ALLOCATION_STATUS_RUNNING',
			'{}'::jsonb, 1, $2, $2, 0, false, '')
	`, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert allocation: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO workload_reservations(
			reservation_id,allocation_id,namespace,owner_type,owner_id,node_id,created_at
		) VALUES ('res-' || $1,$1,'default','run','run-test','node-test',$2)
	`, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert workload reservation: %v", err)
	}
}
