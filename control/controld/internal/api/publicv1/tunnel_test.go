package publicv1

import (
	"context"
	"strings"
	"testing"
	"time"

	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

type fakeTunnelControl struct {
	createdSessionID string
	revokedSessionID string
	revokeReason     string
	getStatus        tunnelv1.TunnelSessionStatus
	getReason        string
	getCount         int
}

func (f *fakeTunnelControl) Create(context.Context, tunnelkernel.CreateParams) (*tunnelkernel.CreateResult, error) {
	f.createdSessionID = "session-wait-timeout"
	return &tunnelkernel.CreateResult{
		Session: &tunnelv1.TunnelSession{
			SessionID: f.createdSessionID,
			Status:    tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING,
		},
		ClientToken: "client-token",
	}, nil
}

func (f *fakeTunnelControl) Get(context.Context, string, time.Time) (*tunnelv1.TunnelSession, error) {
	f.getCount++
	status := f.getStatus
	if status == tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_UNSPECIFIED {
		status = tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING
	}
	return &tunnelv1.TunnelSession{
		SessionID: f.createdSessionID,
		Status:    status,
		Reason:    f.getReason,
	}, nil
}

func (*fakeTunnelControl) List(context.Context, string, string, bool, time.Time) ([]*tunnelv1.TunnelSession, error) {
	return nil, nil
}

func (*fakeTunnelControl) ListEvents(context.Context, string, int32, time.Time) ([]*tunnelv1.TunnelSessionEvent, error) {
	return nil, nil
}

func (f *fakeTunnelControl) Revoke(_ context.Context, sessionID, reason string, _ time.Time) (*tunnelv1.TunnelSession, error) {
	f.revokedSessionID = sessionID
	f.revokeReason = reason
	return &tunnelv1.TunnelSession{
		SessionID: sessionID,
		Status:    tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED,
		Reason:    reason,
	}, nil
}

func (*fakeTunnelControl) Renew(context.Context, string, string, time.Duration, time.Time) (*tunnelv1.TunnelSession, error) {
	return nil, grpcstatus.Error(codes.Unimplemented, "not implemented")
}

func (*fakeTunnelControl) ValidatePeer(context.Context, string, tunnelv1.TunnelPeerKind, string, time.Time) (*tunnelv1.TunnelSession, error) {
	return nil, grpcstatus.Error(codes.Unimplemented, "not implemented")
}

func TestCreateTunnelSessionWaitReadyRevokesOnTimeout(t *testing.T) {
	tunnels := &fakeTunnelControl{}
	server := New(Dependencies{
		Now:     func() time.Time { return time.Unix(100, 0).UTC() },
		Tunnels: tunnels,
	})
	_, err := server.CreateTunnelSession(context.Background(), &tunnelv1.CreateTunnelSessionRequest{
		AllocationID: "alloc-1",
		WaitReady:    true,
		ReadyTimeout: durationpb.New(time.Millisecond),
	})
	if grpcstatus.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("CreateTunnelSession() code = %v, want %v (err=%v)", grpcstatus.Code(err), codes.DeadlineExceeded, err)
	}
	if tunnels.revokedSessionID != tunnels.createdSessionID {
		t.Fatalf("revoked session = %q, want %q", tunnels.revokedSessionID, tunnels.createdSessionID)
	}
	if tunnels.revokeReason == "" {
		t.Fatal("revoke reason = empty, want ready-wait failure reason")
	}
}

func TestCreateTunnelSessionWaitReadyReturnsNodeFailure(t *testing.T) {
	tunnels := &fakeTunnelControl{
		getStatus: tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED,
		getReason: "sandbox tunnel agent failed before readiness",
	}
	server := New(Dependencies{
		Now:     func() time.Time { return time.Unix(100, 0).UTC() },
		Tunnels: tunnels,
	})
	_, err := server.CreateTunnelSession(context.Background(), &tunnelv1.CreateTunnelSessionRequest{
		AllocationID: "alloc-1",
		WaitReady:    true,
		ReadyTimeout: durationpb.New(time.Minute),
	})
	if grpcstatus.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), tunnels.getReason) {
		t.Fatalf("CreateTunnelSession() error = %v, want terminal node reason", err)
	}
	if tunnels.getCount != 1 {
		t.Fatalf("Get calls = %d, want one terminal observation", tunnels.getCount)
	}
	if tunnels.revokedSessionID != tunnels.createdSessionID {
		t.Fatalf("revoke session = %q, want %q", tunnels.revokedSessionID, tunnels.createdSessionID)
	}
}
