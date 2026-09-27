package pgrun

import (
	"context"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestCancelRunRevokesTunnelAccessInSameTransaction(t *testing.T) {
	db := newEnvironmentTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO principals(principal_id, name, display_name, kind, status, created_at, updated_at)
		VALUES ('prn-cancel-tunnel', 'cancel-tunnel', 'Cancel Tunnel', 'human', 'active', $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes(node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-cancel-tunnel', '127.0.0.1:24010', repeat('0', 64), $1, $1, 'active')
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs(run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ('run-cancel-tunnel', 'default', 'env-cancel-tunnel', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations(allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ('alloc-cancel-tunnel', 'run-cancel-tunnel', 'node-cancel-tunnel', 'ALLOCATION_LIFECYCLE_STATE_ACTIVE', 1, $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}

	tunnels := pgtunnel.NewStore(db, pgtunnel.WithRelays([]pgtunnel.Relay{{
		ID: "test", ClientTarget: "127.0.0.1:24210", NodeTarget: "127.0.0.1:24210", Weight: 1,
	}}))
	defer tunnels.Close()
	actorCtx := accesskernel.WithActor(ctx, accesskernel.Actor{
		Principal: accesskernel.Principal{ID: "prn-cancel-tunnel", Status: accesskernel.PrincipalStatusActive},
	})
	created, err := tunnels.Create(actorCtx, tunnelkernel.CreateParams{AllocationID: "alloc-cancel-tunnel", TTL: time.Hour, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := created.Session.GetSessionID()
	if _, err := tunnels.ValidatePeer(ctx, sessionID, tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT, created.ClientToken, now); err != nil {
		t.Fatalf("peer before cancellation: %v", err)
	}

	runs := NewStore(db)
	defer runs.Close()
	cancelled, err := runs.CancelRun(ctx, "run-cancel-tunnel", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.GetStatus() != runv1.RunStatus_RUN_STATUS_CANCELLED {
		t.Fatalf("cancelled Run status = %v", cancelled.GetStatus())
	}
	var allocationState string
	if err := db.Pool().QueryRow(ctx, "SELECT lifecycle_state FROM allocations WHERE allocation_id = 'alloc-cancel-tunnel'").Scan(&allocationState); err != nil {
		t.Fatal(err)
	}
	if allocationState != "ALLOCATION_LIFECYCLE_STATE_RELEASING" {
		t.Fatalf("cancelled Allocation state = %s", allocationState)
	}
	session, err := tunnels.Get(ctx, sessionID, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if session.GetStatus() != tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED {
		t.Fatalf("cancelled Run left TunnelSession active: %v", session.GetStatus())
	}
	if _, err := tunnels.ValidatePeer(ctx, sessionID, tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT, created.ClientToken, now.Add(time.Second)); grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("peer after cancellation: %v, want PermissionDenied", err)
	}
	if _, err := tunnels.Renew(ctx, sessionID, created.ClientToken, time.Hour, now.Add(time.Second)); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("renew after cancellation: %v, want FailedPrecondition", err)
	}
	if _, err := runs.CancelRun(ctx, "run-cancel-tunnel", now.Add(2*time.Second)); err != nil {
		t.Fatalf("repeat cancellation: %v", err)
	}
	var revision int64
	if err := db.Pool().QueryRow(ctx, "SELECT revision FROM tunnel_sessions WHERE session_id = $1", sessionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if revision != 2 {
		t.Fatalf("TunnelSession revision after repeated cancellation = %d, want 2", revision)
	}
}
