package relay

import (
	"context"
	"fmt"
	"os"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type ControlClient interface {
	ValidateTunnelPeer(ctx context.Context, in *tunnelrelaycontrolv1.ValidateTunnelPeerRequest) (*tunnelrelaycontrolv1.ValidateTunnelPeerResponse, error)
	ReportTunnelPeerEvent(ctx context.Context, in *tunnelrelaycontrolv1.ReportTunnelPeerEventRequest) (*tunnelrelaycontrolv1.ReportTunnelPeerEventResponse, error)
}

type Server struct {
	tunnelv1.UnimplementedTunnelRelayServer

	control                ControlClient
	relayID                string
	drain                  bool
	peerOpenTimeout        time.Duration
	peerRevalidateInterval time.Duration
	maxSessions            int
	sendQueueSize          int
	maxFrameBytes          int
	pairWaitTimeout        time.Duration
	pingInterval           time.Duration
	pongTimeout            time.Duration
	registry               sessionRegistry
}

func (s *Server) ConnectPeer(stream tunnelv1.TunnelRelay_ConnectPeerServer) error {
	first, err := recvInitialFrame(stream, s.peerOpenTimeout)
	if err != nil {
		s.recordPeerConnect(stream.Context(), tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_UNSPECIFIED, sdkobs.ResultError)
		return err
	}
	open := first.GetPeerOpen()
	if open == nil {
		s.recordPeerConnect(stream.Context(), tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_UNSPECIFIED, sdkobs.ResultError)
		return grpcstatus.Error(codes.InvalidArgument, "initial peer_open frame is required")
	}
	if s.control == nil {
		s.recordPeerConnect(stream.Context(), open.GetPeerKind(), sdkobs.ResultError)
		return grpcstatus.Error(codes.FailedPrecondition, "control validator is not configured")
	}
	if s.drain {
		s.reportPeerEvent(stream.Context(), open.GetSessionID(), open.GetToken(), open.GetPeerKind(), tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_DRAIN_REJECTED, tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_DRAIN_REJECTED, "relay is draining", 0, 0)
		s.recordPeerConnect(stream.Context(), open.GetPeerKind(), sdkobs.ResultError)
		return grpcstatus.Error(codes.Unavailable, "tunnel relay is draining")
	}
	if _, err := s.control.ValidateTunnelPeer(stream.Context(), &tunnelrelaycontrolv1.ValidateTunnelPeerRequest{
		SessionID: open.GetSessionID(),
		PeerKind:  open.GetPeerKind(),
		Token:     open.GetToken(),
	}); err != nil {
		s.recordPeerConnect(stream.Context(), open.GetPeerKind(), sdkobs.ResultError)
		fmt.Fprintf(os.Stderr, "tunneld: validate peer failed session=%s kind=%s err=%v\n", open.GetSessionID(), open.GetPeerKind().String(), err)
		return err
	}
	fmt.Fprintf(os.Stderr, "tunneld: peer connected session=%s kind=%s\n", open.GetSessionID(), open.GetPeerKind().String())

	p := &peer{
		kind:      open.GetPeerKind(),
		sessionID: open.GetSessionID(),
		token:     open.GetToken(),
		send:      make(chan *tunnelv1.TunnelFrame, s.sendQueueSize),
		done:      make(chan struct{}),
	}
	p.lastSeen.Store(time.Now())
	if err := s.register(open.GetSessionID(), p); err != nil {
		s.recordPeerConnect(stream.Context(), open.GetPeerKind(), sdkobs.ResultError)
		s.reportPeerEvent(stream.Context(), open.GetSessionID(), open.GetToken(), open.GetPeerKind(), tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_RESOURCE_LIMITED, tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_RESOURCE_LIMITED, err.Error(), 0, 0)
		return err
	}
	s.recordPeerConnect(stream.Context(), open.GetPeerKind(), sdkobs.ResultOK)
	s.reportPeerEvent(stream.Context(), p.sessionID, p.token, p.kind, connectedEvent(p.kind), connectedReason(p.kind), "connected", 0, 0)
	if s.opposite(p.sessionID, p) != nil {
		s.reportPeerEvent(stream.Context(), p.sessionID, p.token, tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_UNSPECIFIED, tunnelcontrolv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_PAIRED, tunnelcontrolv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_PAIRED, "paired", 0, 0)
	}
	defer s.unregister(open.GetSessionID(), p)

	runCtx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	go p.run(func() error { return writeLoop(runCtx, stream, p) })
	go p.run(func() error { return s.readLoop(runCtx, stream, open.GetSessionID(), p) })
	go p.run(func() error { return s.revalidateLoop(runCtx, p) })
	go p.run(func() error { return s.heartbeatLoop(runCtx, p) })
	<-p.done
	cancel()
	err = p.error()
	outcome := peerCloseOutcomeForError(p.kind, err)
	s.recordPeerDisconnect(stream.Context(), p.kind, outcome.result, outcome.reason)
	s.reportPeerEvent(context.Background(), p.sessionID, p.token, p.kind, disconnectedEvent(p.kind), outcome.reasonCode, outcome.reason, p.bytesIn.Load(), p.bytesOut.Load())
	if outcome.err == nil {
		return nil
	}
	return outcome.err
}
