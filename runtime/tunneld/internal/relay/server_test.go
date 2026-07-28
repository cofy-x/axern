package relay

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type fakeControl struct {
	validate func(context.Context, *tunnelcontrolv1.ValidateTunnelPeerRequest) (*tunnelcontrolv1.ValidateTunnelPeerResponse, error)
	report   func(context.Context, *tunnelrelaycontrolv1.ReportTunnelPeerEventRequest) (*tunnelrelaycontrolv1.ReportTunnelPeerEventResponse, error)
}

func (f fakeControl) ValidateTunnelPeer(ctx context.Context, in *tunnelcontrolv1.ValidateTunnelPeerRequest) (*tunnelcontrolv1.ValidateTunnelPeerResponse, error) {
	if f.validate != nil {
		return f.validate(ctx, in)
	}
	return &tunnelcontrolv1.ValidateTunnelPeerResponse{Session: &tunnelcontrolv1.TunnelSession{
		SessionID: in.GetSessionID(),
		Status:    tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
	}}, nil
}

func (f fakeControl) ReportTunnelPeerEvent(ctx context.Context, in *tunnelrelaycontrolv1.ReportTunnelPeerEventRequest) (*tunnelrelaycontrolv1.ReportTunnelPeerEventResponse, error) {
	if f.report != nil {
		return f.report(ctx, in)
	}
	return &tunnelrelaycontrolv1.ReportTunnelPeerEventResponse{}, nil
}

func TestRelayPairsClientAndNodePeers(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	tunnelv1.RegisterTunnelRelayServer(server, New(fakeControl{}))
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	conn, err := grpcclient.NewReadyClient(context.Background(), grpcclient.PassthroughTarget("bufnet"), grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer conn.Close()
	client := tunnelv1.NewTunnelRelayClient(conn)

	clientPeer, err := client.ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("connect client peer: %v", err)
	}
	if err := clientPeer.Send(peerOpen("session-1", tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)); err != nil {
		t.Fatalf("send client open: %v", err)
	}

	nodePeer, err := client.ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("connect node peer: %v", err)
	}
	if err := nodePeer.Send(peerOpen("session-1", tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)); err != nil {
		t.Fatalf("send node open: %v", err)
	}
	if err := nodePeer.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{StreamOpen: &tunnelv1.StreamOpen{StreamID: 7}}}); err != nil {
		t.Fatalf("send stream open: %v", err)
	}
	got, err := clientPeer.Recv()
	if err != nil {
		t.Fatalf("recv client frame: %v", err)
	}
	if got.GetStreamOpen().GetStreamID() != 7 {
		t.Fatalf("stream id = %d, want 7", got.GetStreamOpen().GetStreamID())
	}
}

func TestOppositePeerCloseDoesNotPanic(t *testing.T) {
	t.Parallel()

	_, client := newRelayTestClient(t, New(fakeControl{}, WithPeerRevalidateInterval(0), WithPingInterval(0)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	clientPeer, err := client.ConnectPeer(ctx)
	if err != nil {
		t.Fatalf("connect client peer: %v", err)
	}
	if err := clientPeer.Send(peerOpen("session-1", tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)); err != nil {
		t.Fatalf("send client open: %v", err)
	}
	nodePeer, err := client.ConnectPeer(ctx)
	if err != nil {
		t.Fatalf("connect node peer: %v", err)
	}
	if err := nodePeer.Send(peerOpen("session-1", tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE)); err != nil {
		t.Fatalf("send node open: %v", err)
	}
	if err := nodePeer.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{StreamOpen: &tunnelv1.StreamOpen{StreamID: 7}}}); err != nil {
		t.Fatalf("send stream open: %v", err)
	}
	if _, err := clientPeer.Recv(); err != nil {
		t.Fatalf("recv paired frame before closing client peer: %v", err)
	}
	if err := clientPeer.CloseSend(); err != nil {
		t.Fatalf("close client send: %v", err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := nodePeer.Recv()
		errCh <- err
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("node peer received a frame after opposite client peer closed")
		}
	case <-ctx.Done():
		t.Fatal("node peer did not close after opposite client peer closed")
	}
}

func TestRegisterReplacingPeerCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	server := New(fakeControl{})
	first := &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}
	second := &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}

	if err := server.register("session-1", first); err != nil {
		t.Fatalf("register first: %v", err)
	}
	if err := server.register("session-1", second); err != nil {
		t.Fatalf("register second: %v", err)
	}
	first.close()
}

func TestRegisterReplacementClosesOppositePeer(t *testing.T) {
	t.Parallel()

	server := New(fakeControl{})
	firstClient := &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}
	firstNode := &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}
	nextClient := &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}

	if err := server.register("session-1", firstClient); err != nil {
		t.Fatalf("register first client: %v", err)
	}
	if err := server.register("session-1", firstNode); err != nil {
		t.Fatalf("register first node: %v", err)
	}
	if err := server.register("session-1", nextClient); err != nil {
		t.Fatalf("register next client: %v", err)
	}

	select {
	case <-firstNode.done:
	case <-time.After(time.Second):
		t.Fatal("replacing client did not close the opposite node peer")
	}
	if got := server.opposite("session-1", nextClient); got != nil {
		t.Fatalf("replacement opposite = %#v, want nil until a new node peer connects", got)
	}
}

func TestRegisterEnforcesMaxSessions(t *testing.T) {
	t.Parallel()

	server := New(fakeControl{}, WithMaxSessions(1))
	if err := server.register("session-1", &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}); err != nil {
		t.Fatalf("register first session: %v", err)
	}
	if err := server.register("session-2", &peer{
		kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		send: make(chan *tunnelv1.TunnelFrame, 1),
		done: make(chan struct{}),
	}); err == nil {
		t.Fatal("register second session error = nil, want limit error")
	}
}

func TestRegisterAllowsUnlimitedSessions(t *testing.T) {
	t.Parallel()

	server := New(fakeControl{}, WithMaxSessions(0))
	for _, sessionID := range []string{"session-1", "session-2"} {
		if err := server.register(sessionID, &peer{
			kind: tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
			send: make(chan *tunnelv1.TunnelFrame, 1),
			done: make(chan struct{}),
		}); err != nil {
			t.Fatalf("register %s: %v", sessionID, err)
		}
	}
}

func TestPeerRevalidateIntervalCanBeDisabled(t *testing.T) {
	t.Parallel()

	server := New(fakeControl{}, WithPeerRevalidateInterval(0))
	if server.peerRevalidateInterval != 0 {
		t.Fatalf("peerRevalidateInterval = %s, want 0", server.peerRevalidateInterval)
	}
}

func TestConnectPeerTimesOutWithoutInitialOpen(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	relay := New(fakeControl{})
	relay.peerOpenTimeout = 20 * time.Millisecond
	tunnelv1.RegisterTunnelRelayServer(server, relay)
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	conn, err := grpcclient.NewReadyClient(context.Background(), grpcclient.PassthroughTarget("bufnet"), grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	defer conn.Close()
	peer, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("connect peer: %v", err)
	}
	_, err = peer.Recv()
	if err == nil || !strings.Contains(err.Error(), "initial peer_open frame timed out") {
		t.Fatalf("Recv() error = %v, want initial open timeout", err)
	}
}

func TestRevalidationClosesTerminalSession(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	control := fakeControl{validate: func(_ context.Context, in *tunnelcontrolv1.ValidateTunnelPeerRequest) (*tunnelcontrolv1.ValidateTunnelPeerResponse, error) {
		if calls.Add(1) > 1 {
			return nil, grpcstatus.Error(codes.PermissionDenied, "tunnel session is not active")
		}
		return &tunnelcontrolv1.ValidateTunnelPeerResponse{Session: &tunnelcontrolv1.TunnelSession{
			SessionID: in.GetSessionID(),
			Status:    tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		}}, nil
	}}
	_, client := newRelayTestClient(t, New(control, WithPeerRevalidateInterval(10*time.Millisecond)))
	peer, err := client.ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("connect peer: %v", err)
	}
	if err := peer.Send(peerOpen("session-1", tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)); err != nil {
		t.Fatalf("send peer open: %v", err)
	}
	_, err = peer.Recv()
	if err == nil || grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("Recv() error = %v, want PermissionDenied", err)
	}
}

func TestRevalidationKeepsPeerOnTransientFailure(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	control := fakeControl{validate: func(_ context.Context, in *tunnelcontrolv1.ValidateTunnelPeerRequest) (*tunnelcontrolv1.ValidateTunnelPeerResponse, error) {
		if calls.Add(1) > 1 {
			return nil, grpcstatus.Error(codes.Unavailable, "control unavailable")
		}
		return &tunnelcontrolv1.ValidateTunnelPeerResponse{Session: &tunnelcontrolv1.TunnelSession{
			SessionID: in.GetSessionID(),
			Status:    tunnelcontrolv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		}}, nil
	}}
	_, client := newRelayTestClient(t, New(control, WithPeerRevalidateInterval(10*time.Millisecond)))
	peer, err := client.ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("connect peer: %v", err)
	}
	if err := peer.Send(peerOpen("session-1", tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT)); err != nil {
		t.Fatalf("send peer open: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := peer.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Ping{Ping: &tunnelv1.Ping{ID: "still-open"}}}); err != nil {
		t.Fatalf("send ping after transient revalidation failure: %v", err)
	}
	got, err := peer.Recv()
	if err != nil {
		t.Fatalf("recv pong after transient revalidation failure: %v", err)
	}
	if got.GetPong().GetID() != "still-open" {
		t.Fatalf("pong id = %q, want still-open", got.GetPong().GetID())
	}
}

func TestReasonForErrorMapsPongTimeout(t *testing.T) {
	t.Parallel()

	if got := reasonForError(errPeerPongTimeout); got != tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_PONG_TIMEOUT {
		t.Fatalf("reasonForError(pong timeout) = %s, want RELAY_PONG_TIMEOUT", got)
	}
}

func TestPeerCloseOutcomeClassifiesNormalAndRelayErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantResult string
		wantReason string
		wantCode   tunnelcontrolv1.TunnelSessionEventReasonCode
		wantErr    bool
	}{
		{
			name:       "nil",
			wantResult: "ok",
			wantReason: peerCloseReasonNormal,
			wantCode:   tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_DISCONNECTED,
		},
		{
			name:       "canceled",
			err:        grpcstatus.Error(codes.Canceled, "client canceled"),
			wantResult: "ok",
			wantReason: peerCloseReasonContextCancel,
			wantCode:   tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_CLIENT_DISCONNECTED,
		},
		{
			name:       "frame too large",
			err:        grpcstatus.Error(codes.ResourceExhausted, "tunnel frame too large"),
			wantResult: "error",
			wantReason: peerCloseReasonFrameTooLarge,
			wantCode:   tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_FRAME_TOO_LARGE,
			wantErr:    true,
		},
		{
			name:       "queue full",
			err:        grpcstatus.Error(codes.ResourceExhausted, "tunnel peer send queue full"),
			wantResult: "error",
			wantReason: peerCloseReasonQueueFull,
			wantCode:   tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_QUEUE_FULL,
			wantErr:    true,
		},
		{
			name:       "opposite missing",
			err:        grpcstatus.Error(codes.Unavailable, "opposite tunnel peer is not connected"),
			wantResult: "error",
			wantReason: peerCloseReasonOppositeMissed,
			wantCode:   tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_OPPOSITE_MISSING,
			wantErr:    true,
		},
		{
			name:       "pong timeout",
			err:        errPeerPongTimeout,
			wantResult: "error",
			wantReason: peerCloseReasonPongTimeout,
			wantCode:   tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RELAY_PONG_TIMEOUT,
			wantErr:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := peerCloseOutcomeForError(tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT, tc.err)
			if got.result != tc.wantResult || got.reason != tc.wantReason || got.reasonCode != tc.wantCode {
				t.Fatalf("outcome = {result:%q reason:%q code:%s}, want {result:%q reason:%q code:%s}", got.result, got.reason, got.reasonCode, tc.wantResult, tc.wantReason, tc.wantCode)
			}
			if (got.err != nil) != tc.wantErr {
				t.Fatalf("outcome err present = %t, want %t", got.err != nil, tc.wantErr)
			}
		})
	}
}

func newRelayTestClient(t *testing.T, relay *Server) (*grpc.Server, tunnelv1.TunnelRelayClient) {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	tunnelv1.RegisterTunnelRelayServer(server, relay)
	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)
	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	conn, err := grpcclient.NewReadyClient(context.Background(), grpcclient.PassthroughTarget("bufnet"), grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial relay: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return server, tunnelv1.NewTunnelRelayClient(conn)
}

func peerOpen(sessionID string, kind tunnelcontrolv1.TunnelPeerKind) *tunnelv1.TunnelFrame {
	return &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: sessionID,
		PeerKind:  kind,
		Token:     "token",
	}}}
}
