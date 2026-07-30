package relayv1

import (
	"context"

	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) ValidateTunnelPeer(ctx context.Context, req *tunnelrelaycontrolv1.ValidateTunnelPeerRequest) (*tunnelrelaycontrolv1.ValidateTunnelPeerResponse, error) {
	if s.deps.Tunnels == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel relay control is not configured")
	}
	session, err := s.deps.Tunnels.ValidatePeer(ctx, req.GetSessionID(), req.GetPeerKind(), req.GetToken(), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &tunnelrelaycontrolv1.ValidateTunnelPeerResponse{Session: session}, nil
}

func (s *Server) ReportTunnelPeerEvent(ctx context.Context, req *tunnelrelaycontrolv1.ReportTunnelPeerEventRequest) (*tunnelrelaycontrolv1.ReportTunnelPeerEventResponse, error) {
	if s.deps.Tunnels == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel relay control is not configured")
	}
	now := s.deps.Now()
	session, err := s.deps.Tunnels.ReportPeerEvent(ctx, tunnelkernel.PeerEventParams{
		SessionID:  req.GetSessionID(),
		RelayID:    req.GetRelayID(),
		PeerKind:   req.GetPeerKind(),
		EventType:  req.GetEventType(),
		ReasonCode: req.GetReasonCode(),
		Reason:     req.GetReason(),
		BytesIn:    req.GetBytesIn(),
		BytesOut:   req.GetBytesOut(),
		PeerToken:  req.GetPeerToken(),
	}, now)
	if err != nil {
		return nil, err
	}
	return &tunnelrelaycontrolv1.ReportTunnelPeerEventResponse{Session: session}, nil
}
