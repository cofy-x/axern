package publicv1

import (
	"context"
	"strings"
	"testing"
	"time"

	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
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

func assertTunnelSetupError(t *testing.T, err error, code codes.Code, reason tunnelkernel.SetupErrorReason) {
	t.Helper()
	st, ok := grpcstatus.FromError(err)
	if !ok || st.Code() != code {
		t.Fatalf("tunnel setup error = %v, want gRPC %s", err, code)
	}
	if len(st.Details()) != 1 {
		t.Fatalf("tunnel setup details = %v, want one ErrorInfo", st.Details())
	}
	info, ok := st.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.GetDomain() != tunnelkernel.SetupErrorDomain || info.GetReason() != string(reason) {
		t.Fatalf("tunnel setup detail = %v, want %s/%s", st.Details()[0], tunnelkernel.SetupErrorDomain, reason)
	}
	if len(info.GetMetadata()) != 0 {
		t.Fatalf("tunnel setup metadata = %v, want none", info.GetMetadata())
	}
	if strings.Contains(st.Message(), "client-token") || strings.Contains(st.Message(), "session-wait-timeout") {
		t.Fatalf("tunnel setup message exposes session identity or token: %q", st.Message())
	}
}

func TestCreateTunnelSessionControlUnavailableHasPublicReason(t *testing.T) {
	server := New(Dependencies{})
	response, err := server.CreateTunnelSession(context.Background(), &tunnelv1.CreateTunnelSessionRequest{AllocationID: "alloc-1"})
	if response != nil {
		t.Fatalf("CreateTunnelSession() response = %v, want nil", response)
	}
	assertTunnelSetupError(t, err, codes.FailedPrecondition, tunnelkernel.SetupControlUnavailable)
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
	assertTunnelSetupError(t, err, codes.DeadlineExceeded, tunnelkernel.SetupReadyTimeout)
	if tunnels.revokedSessionID != tunnels.createdSessionID {
		t.Fatalf("revoked session = %q, want %q", tunnels.revokedSessionID, tunnels.createdSessionID)
	}
	if tunnels.revokeReason == "" {
		t.Fatal("revoke reason = empty, want ready-wait failure reason")
	}
}

func TestCreateTunnelSessionWaitReadyPreservesCallerCancellation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func() context.Context
		code codes.Code
	}{
		{"canceled", func() context.Context {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}, codes.Canceled},
		{"deadline exceeded", func() context.Context {
			ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
			defer cancel()
			return ctx
		}, codes.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tunnels := &fakeTunnelControl{}
			server := New(Dependencies{
				Now:     func() time.Time { return time.Unix(100, 0).UTC() },
				Tunnels: tunnels,
			})
			response, err := server.CreateTunnelSession(tc.ctx(), &tunnelv1.CreateTunnelSessionRequest{
				AllocationID: "alloc-1",
				WaitReady:    true,
				ReadyTimeout: durationpb.New(time.Minute),
			})
			if response != nil || grpcstatus.Code(err) != tc.code {
				t.Fatalf("CreateTunnelSession() = %v, %v; want nil, %s", response, err, tc.code)
			}
			if len(grpcstatus.Convert(err).Details()) != 0 {
				t.Fatalf("caller cancellation gained tunnel setup reason: %v", grpcstatus.Convert(err).Details())
			}
		})
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
	assertTunnelSetupError(t, err, codes.FailedPrecondition, tunnelkernel.SetupSessionFailed)
	if !strings.Contains(err.Error(), tunnels.getReason) {
		t.Fatalf("CreateTunnelSession() error = %v, want terminal node reason", err)
	}
	if tunnels.getCount != 1 {
		t.Fatalf("Get calls = %d, want one terminal observation", tunnels.getCount)
	}
	if tunnels.revokedSessionID != tunnels.createdSessionID {
		t.Fatalf("revoke session = %q, want %q", tunnels.revokedSessionID, tunnels.createdSessionID)
	}
}

func TestCreateTunnelSessionWaitReadyTerminalReasonCodes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status tunnelv1.TunnelSessionStatus
		reason tunnelkernel.SetupErrorReason
	}{
		{"revoked", tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED, tunnelkernel.SetupSessionRevoked},
		{"expired", tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED, tunnelkernel.SetupSessionExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tunnels := &fakeTunnelControl{getStatus: tc.status}
			server := New(Dependencies{
				Now:     func() time.Time { return time.Unix(100, 0).UTC() },
				Tunnels: tunnels,
			})
			response, err := server.CreateTunnelSession(context.Background(), &tunnelv1.CreateTunnelSessionRequest{
				AllocationID: "alloc-1",
				WaitReady:    true,
			})
			if response != nil {
				t.Fatalf("CreateTunnelSession() response = %v, want nil", response)
			}
			assertTunnelSetupError(t, err, codes.FailedPrecondition, tc.reason)
		})
	}
}
