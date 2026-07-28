package tunnel

import (
	"context"
	"testing"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeTunnelClient struct {
	created     *tunnelv1.CreateTunnelSessionRequest
	revoked     *tunnelv1.RevokeTunnelSessionRequest
	getResp     *tunnelv1.GetTunnelSessionResponse
	listResp    *tunnelv1.ListTunnelSessionsResponse
	eventsResp  *tunnelv1.ListTunnelSessionEventsResponse
	inspectResp *tunnelv1.InspectTunnelSessionResponse
}

func (f *fakeTunnelClient) CreateTunnelSession(_ context.Context, req *tunnelv1.CreateTunnelSessionRequest, _ ...grpc.CallOption) (*tunnelv1.CreateTunnelSessionResponse, error) {
	f.created = req
	now := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	return &tunnelv1.CreateTunnelSessionResponse{
		Session: &tunnelv1.TunnelSession{
			SessionID:        "tun-1",
			AllocationID:     req.GetAllocationID(),
			RemotePort:       12345,
			ClientEdgeTarget: "127.0.0.1:25000",
			CreatedAt:        timestamppb.New(now),
			ExpiresAt:        timestamppb.New(now.Add(time.Hour)),
		},
		ClientToken: "client-token",
	}, nil
}

func (f *fakeTunnelClient) GetTunnelSession(context.Context, *tunnelv1.GetTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.GetTunnelSessionResponse, error) {
	if f.getResp != nil {
		return f.getResp, nil
	}
	return &tunnelv1.GetTunnelSessionResponse{Session: &tunnelv1.TunnelSession{SessionID: "tun-1", Status: tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING}}, nil
}

func (f *fakeTunnelClient) ListTunnelSessions(context.Context, *tunnelv1.ListTunnelSessionsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionsResponse, error) {
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &tunnelv1.ListTunnelSessionsResponse{}, nil
}

func TestForwardDoesNotRevokeExpiredSessionOnExit(t *testing.T) {
	client := &fakeTunnelClient{
		getResp: &tunnelv1.GetTunnelSessionResponse{Session: &tunnelv1.TunnelSession{
			SessionID: "tun-1",
			Status:    tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED,
		}},
	}
	err := New(client).Forward(context.Background(), ForwardParams{
		AllocationID: "alloc-1",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Hour,
		WaitReady:    true,
		ReadyTimeout: 30 * time.Second,
		DisableRenew: true,
		Connector:    ConnectorConfig{MaxStreams: 32},
		ConnectorRunner: func(context.Context, *tunnelv1.TunnelSession, string, string, RelayDialConfig, ConnectorConfig) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Forward returned error: %v", err)
	}
	if client.revoked != nil {
		t.Fatalf("expired session was revoked: %+v", client.revoked)
	}
}

func (f *fakeTunnelClient) RenewTunnelSession(context.Context, *tunnelv1.RenewTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RenewTunnelSessionResponse, error) {
	return &tunnelv1.RenewTunnelSessionResponse{}, nil
}

func (f *fakeTunnelClient) RevokeTunnelSession(_ context.Context, req *tunnelv1.RevokeTunnelSessionRequest, _ ...grpc.CallOption) (*tunnelv1.RevokeTunnelSessionResponse, error) {
	f.revoked = req
	return &tunnelv1.RevokeTunnelSessionResponse{}, nil
}

func (f *fakeTunnelClient) ListTunnelSessionEvents(context.Context, *tunnelv1.ListTunnelSessionEventsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionEventsResponse, error) {
	if f.eventsResp != nil {
		return f.eventsResp, nil
	}
	return &tunnelv1.ListTunnelSessionEventsResponse{}, nil
}

func (f *fakeTunnelClient) InspectTunnelSession(context.Context, *tunnelv1.InspectTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.InspectTunnelSessionResponse, error) {
	if f.inspectResp != nil {
		return f.inspectResp, nil
	}
	return &tunnelv1.InspectTunnelSessionResponse{}, nil
}

func TestForwardCreatesConnectsAndRevokes(t *testing.T) {
	client := &fakeTunnelClient{}
	var callbackSessionID string
	var connectorLocalTarget string
	err := New(client).Forward(context.Background(), ForwardParams{
		AllocationID: "alloc-1",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Hour,
		WaitReady:    true,
		ReadyTimeout: 30 * time.Second,
		DisableRenew: true,
		Connector:    ConnectorConfig{MaxStreams: 32},
		OnSessionCreated: func(session ForwardSession) error {
			callbackSessionID = session.Session.GetSessionID()
			return nil
		},
		ConnectorRunner: func(_ context.Context, session *tunnelv1.TunnelSession, token, localTarget string, _ RelayDialConfig, cfg ConnectorConfig) error {
			if session.GetSessionID() != "tun-1" || token != "client-token" || cfg.MaxStreams != 32 {
				t.Fatalf("unexpected connector inputs session=%s token=%s cfg=%+v", session.GetSessionID(), token, cfg)
			}
			connectorLocalTarget = localTarget
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Forward returned error: %v", err)
	}
	if client.created == nil || client.created.GetAllocationID() != "alloc-1" || !client.created.GetWaitReady() {
		t.Fatalf("unexpected create request: %+v", client.created)
	}
	if callbackSessionID != "tun-1" {
		t.Fatalf("callback session id = %s, want tun-1", callbackSessionID)
	}
	if connectorLocalTarget != "127.0.0.1:8080" {
		t.Fatalf("connector local target = %s", connectorLocalTarget)
	}
	if client.revoked == nil || client.revoked.GetSessionID() != "tun-1" || client.revoked.GetReason() != "client disconnected" {
		t.Fatalf("unexpected revoke request: %+v", client.revoked)
	}
}

func TestForwardWaitsForClientPeerBeforeConnectorStart(t *testing.T) {
	client := &fakeTunnelClient{
		eventsResp: &tunnelv1.ListTunnelSessionEventsResponse{Events: []*tunnelv1.TunnelSessionEvent{{
			EventType: tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED,
		}}},
	}
	started := false
	err := New(client).Forward(context.Background(), ForwardParams{
		AllocationID: "alloc-1",
		LocalTarget:  "127.0.0.1:8080",
		TTL:          time.Hour,
		WaitReady:    true,
		ReadyTimeout: 30 * time.Second,
		DisableRenew: true,
		Connector:    ConnectorConfig{MaxStreams: 32},
		ConnectorRunner: func(ctx context.Context, _ *tunnelv1.TunnelSession, _, _ string, _ RelayDialConfig, _ ConnectorConfig) error {
			<-ctx.Done()
			return nil
		},
		OnConnectorStart: func(ForwardSession) error {
			started = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Forward returned error: %v", err)
	}
	if !started {
		t.Fatalf("OnConnectorStart was not called")
	}
}

func TestForwardDoesNotStartForegroundTaskWhenClientPeerMissing(t *testing.T) {
	client := &fakeTunnelClient{}
	started := false
	err := New(client).Forward(context.Background(), ForwardParams{
		AllocationID:          "alloc-1",
		LocalTarget:           "127.0.0.1:8080",
		TTL:                   time.Hour,
		WaitReady:             true,
		ReadyTimeout:          30 * time.Second,
		DisableRenew:          true,
		ConnectorReadyTimeout: time.Millisecond,
		Connector:             ConnectorConfig{MaxStreams: 32},
		ConnectorRunner: func(ctx context.Context, _ *tunnelv1.TunnelSession, _, _ string, _ RelayDialConfig, _ ConnectorConfig) error {
			<-ctx.Done()
			return nil
		},
		OnConnectorStart: func(ForwardSession) error {
			started = true
			return nil
		},
	})
	if err == nil {
		t.Fatalf("Forward returned nil error")
	}
	if started {
		t.Fatalf("OnConnectorStart was called before client peer connected")
	}
	if client.revoked == nil || client.revoked.GetSessionID() != "tun-1" {
		t.Fatalf("missing revoke after connector ready failure: %+v", client.revoked)
	}
}
