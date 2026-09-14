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
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func newTestStore(t *testing.T, db *postgres.DB) *Store {
	t.Helper()
	store := NewStore(db, WithRelays([]Relay{{
		ID:           "test",
		ClientTarget: "127.0.0.1:24210",
		NodeTarget:   "tunneld:24210",
		Weight:       1,
	}}))
	t.Cleanup(store.Close)
	return store
}

func TestCreateAllocatesRemotePort(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-auto", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-auto",
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
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-explicit", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-explicit",
		RemotePort:   int32Ptr(8786),
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create(explicit port) error = %v", err)
	}
	if got := result.Session.GetRemotePort(); got != 8786 {
		t.Fatalf("remote port = %d, want 8786", got)
	}
}

func TestRelayBindingIsPrivateAndRecoverable(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-private-relay", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-private-relay",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	target, err := store.ResolveRelayTarget(context.Background(), result.Session.GetSessionID(), now)
	if err != nil {
		t.Fatalf("ResolveRelayTarget() error = %v", err)
	}
	if target != "tunneld:24210" {
		t.Fatalf("relay target = %q, want tunneld:24210", target)
	}
	sessions, _, err := store.loadNodeSessions(context.Background(), "node-test", 0)
	if err != nil {
		t.Fatalf("loadNodeSessions() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].GetNodeEdgeTarget() != target {
		t.Fatalf("node desired sessions = %+v, want private relay target %q", sessions, target)
	}

	if _, err := store.Revoke(context.Background(), result.Session.GetSessionID(), "test revoke", now.Add(time.Second)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := store.ResolveRelayTarget(context.Background(), result.Session.GetSessionID(), now.Add(time.Second)); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ResolveRelayTarget(terminal) code = %s, want %s (err=%v)", grpcstatus.Code(err), codes.FailedPrecondition, err)
	}
}

func TestCreateRejectsExplicitZeroRemotePort(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-zero", now)

	_, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-zero",
		RemotePort:   int32Ptr(0),
		Now:          now,
	})
	if err == nil {
		t.Fatal("Create(explicit zero port) error = nil, want error")
	}
}

func TestRenewExtendsActiveSession(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew",
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
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew-expired", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew-expired",
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
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew-revoked", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew-revoked",
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

func TestRevokeIsIdempotentWithoutRevisionChurn(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-revoke-idempotent", now)
	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{AllocationID: "alloc-revoke-idempotent", Now: now})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := store.Revoke(context.Background(), result.Session.GetSessionID(), "first", now.Add(time.Second)); err != nil {
		t.Fatalf("Revoke(first) error = %v", err)
	}
	revision, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision() error = %v", err)
	}
	second, err := store.Revoke(context.Background(), result.Session.GetSessionID(), "second", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("Revoke(second) error = %v", err)
	}
	after, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision(after) error = %v", err)
	}
	if after != revision || second.GetReason() != "first" {
		t.Fatalf("idempotent revoke = revision %d reason %q, want revision %d reason first", after, second.GetReason(), revision)
	}
}

func TestRenewRequiresClientToken(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-renew-token", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-renew-token",
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

func TestNodeDesiredRevisionIgnoresOperationalUpdates(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-revision", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-revision",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	createdRevision, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision(create) error = %v", err)
	}
	if _, err := store.Renew(context.Background(), result.Session.GetSessionID(), result.ClientToken, 10*time.Minute, now.Add(time.Second)); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if _, err := store.ReportStatus(context.Background(), "node-test", result.Session.GetSessionID(), tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING, "", "0.0.0.0:8080", now.Add(2*time.Second)); err != nil {
		t.Fatalf("ReportStatus(running) error = %v", err)
	}
	if _, err := store.ReportPeerEvent(context.Background(), tunnelkernel.PeerEventParams{
		SessionID: result.Session.GetSessionID(),
		RelayID:   "test",
		PeerKind:  tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		EventType: tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED,
		PeerToken: result.ClientToken,
		BytesIn:   12,
		BytesOut:  34,
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("ReportPeerEvent() error = %v", err)
	}
	afterOperationalUpdates, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision(operational updates) error = %v", err)
	}
	if afterOperationalUpdates != createdRevision {
		t.Fatalf("operational updates advanced node desired revision from %d to %d", createdRevision, afterOperationalUpdates)
	}

	if _, err := store.ReportStatus(context.Background(), "node-test", result.Session.GetSessionID(), tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED, "agent exited", "", now.Add(4*time.Second)); err != nil {
		t.Fatalf("ReportStatus(failed) error = %v", err)
	}
	afterTerminal, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision(terminal) error = %v", err)
	}
	if afterTerminal != createdRevision+1 {
		t.Fatalf("terminal update revision = %d, want %d", afterTerminal, createdRevision+1)
	}
	if err := store.ReconcileExpired(context.Background(), now.Add(24*time.Hour)); err != nil {
		t.Fatalf("ReconcileExpired() error = %v", err)
	}
	afterDeadline, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision(after deadline) error = %v", err)
	}
	if afterDeadline != afterTerminal {
		t.Fatalf("expiry rewrote failed terminal revision from %d to %d", afterTerminal, afterDeadline)
	}
	failed, err := store.Get(context.Background(), result.Session.GetSessionID(), now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("Get(failed after deadline) error = %v", err)
	}
	if failed.GetStatus() != tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED {
		t.Fatalf("failed session status after deadline = %s, want failed", failed.GetStatus())
	}
}

func TestWatchNodeBlocksUntilDesiredStateChanges(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-watch", now)
	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-watch",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	revision, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision() error = %v", err)
	}

	type watchResult struct {
		sessions []*nodev1.NodeTunnelSession
		revision int64
		err      error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	watched := make(chan watchResult, 1)
	go func() {
		sessions, current, err := store.WatchNode(ctx, "node-test", revision, now)
		watched <- watchResult{sessions: sessions, revision: current, err: err}
	}()

	select {
	case got := <-watched:
		t.Fatalf("WatchNode returned before a desired-state change: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
	if _, err := store.Revoke(context.Background(), result.Session.GetSessionID(), "test revoke", now.Add(time.Second)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	select {
	case got := <-watched:
		if got.err != nil {
			t.Fatalf("WatchNode() error = %v", got.err)
		}
		if got.revision <= revision || len(got.sessions) != 1 {
			t.Fatalf("WatchNode() = revision %d, sessions %d; want advancing revision and one tombstone", got.revision, len(got.sessions))
		}
		if got.sessions[0].GetSession().GetStatus() != tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED {
			t.Fatalf("WatchNode() status = %s, want revoked", got.sessions[0].GetSession().GetStatus())
		}
	case <-ctx.Done():
		t.Fatal("WatchNode did not observe revoke notification")
	}
}

func TestWatchNodeExpiresSessionAtDeadlineWithoutAnotherWrite(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-watch-expiry", now)
	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-watch-expiry",
		Now:          now,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	revision, err := currentRevision(context.Background(), db.Pool())
	if err != nil {
		t.Fatalf("currentRevision() error = %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE tunnel_sessions SET expires_at = $2 WHERE session_id = $1
	`, result.Session.GetSessionID(), now.Add(200*time.Millisecond)); err != nil {
		t.Fatalf("set near expiry: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sessions, current, err := store.WatchNode(ctx, "node-test", revision, now)
	if err != nil {
		t.Fatalf("WatchNode() error = %v", err)
	}
	if current <= revision || len(sessions) != 1 {
		t.Fatalf("WatchNode() = revision %d, sessions %d; want advancing revision and one tombstone", current, len(sessions))
	}
	if sessions[0].GetSession().GetStatus() != tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED {
		t.Fatalf("WatchNode() status = %s, want expired", sessions[0].GetSession().GetStatus())
	}
}

func TestListEventsTracksTunnelLifecycle(t *testing.T) {
	db := newTunnelTestDB(t)
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-events", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-events",
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
	store := newTestStore(t, db)
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	insertTunnelTestAllocation(t, db, "alloc-events-expire", now)

	result, err := store.Create(tunnelTestContext(), tunnelkernel.CreateParams{
		AllocationID: "alloc-events-expire",
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
		TRUNCATE TABLE principals, namespaces, tunnel_sessions, runs, nodes CASCADE
	`); err != nil {
		t.Fatalf("truncate tunnel test tables: %v", err)
	}
	return db
}

func insertTunnelTestAllocation(t *testing.T, db *postgres.DB, allocationID string, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO principals(principal_id,name,display_name,kind,status,created_at,updated_at)
		VALUES ('prn-tunnel-test','tunnel-test','Tunnel Test','human','active',$1,$1)
		ON CONFLICT (principal_id) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert tunnel principal fixture: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO namespaces(namespace,created_at)
		VALUES ('default',$1)
		ON CONFLICT (namespace) DO NOTHING
	`, now.UTC()); err != nil {
		t.Fatalf("insert tunnel namespace fixture: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (
			node_id, node_target, registered_at, last_heartbeat_at, node_auth_token_hash, lifecycle_status
		) VALUES ('node-test', '127.0.0.1:25000', $1, $1, repeat('0', 64), 'active')
	`, now.UTC()); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ('run-test', 'default', 'env-test', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $1, $1)
	`, now.UTC()); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (
			allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at
		) VALUES ($1, 'run-test', 'node-test', 'ALLOCATION_LIFECYCLE_STATE_ACTIVE', 1, $2, $2)
	`, allocationID, now.UTC()); err != nil {
		t.Fatalf("insert allocation: %v", err)
	}
}
