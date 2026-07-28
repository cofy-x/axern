package publicv1

import (
	"context"
	"fmt"
	"strings"
	"time"

	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) CreateTunnelSession(ctx context.Context, req *tunnelv1.CreateTunnelSessionRequest) (*tunnelv1.CreateTunnelSessionResponse, error) {
	allocationID := strings.TrimSpace(req.GetAllocationID())
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelCreate, publicActionCreate, withAllocationID(allocationID))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	var ttl time.Duration
	if req.GetTtl() != nil {
		ttl = req.GetTtl().AsDuration()
	}
	result, err := s.deps.Tunnels.Create(ctx, tunnelkernel.CreateParams{
		AllocationID: allocationID,
		RemotePort:   req.RemotePort,
		LocalTarget:  req.GetLocalTarget(),
		TTL:          ttl,
		Now:          s.deps.Now(),
	})
	if err != nil {
		opErr = err
		return nil, err
	}
	if req.GetWaitReady() {
		readyTimeout := 30 * time.Second
		if req.GetReadyTimeout() != nil && req.GetReadyTimeout().AsDuration() > 0 {
			readyTimeout = req.GetReadyTimeout().AsDuration()
		}
		session, err := s.waitTunnelReady(ctx, result.Session.GetSessionID(), readyTimeout)
		if err != nil {
			s.revokeTunnelAfterReadyWaitFailure(result.Session.GetSessionID(), err)
			opErr = err
			return nil, err
		}
		result.Session = session
	}
	return &tunnelv1.CreateTunnelSessionResponse{Session: result.Session, ClientToken: result.ClientToken}, nil
}

func (s *Server) revokeTunnelAfterReadyWaitFailure(sessionID string, cause error) {
	if strings.TrimSpace(sessionID) == "" || s.deps.Tunnels == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reason := "ready wait failed"
	if cause != nil {
		reason = fmt.Sprintf("ready wait failed: %v", cause)
	}
	_, _ = s.deps.Tunnels.Revoke(ctx, sessionID, reason, s.deps.Now())
}

func (s *Server) waitTunnelReady(ctx context.Context, sessionID string, timeout time.Duration) (*tunnelv1.TunnelSession, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		session, err := s.deps.Tunnels.Get(ctx, sessionID, s.deps.Now())
		if err != nil {
			return nil, err
		}
		if session.GetStatus() == tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING && strings.TrimSpace(session.GetBoundAddr()) != "" {
			return session, nil
		}
		if session.GetRevoked() || terminalTunnelStatus(session.GetStatus()) {
			return nil, grpcstatus.Errorf(codes.FailedPrecondition, "tunnel session became terminal while waiting for ready: %s %s", session.GetStatus().String(), session.GetReason())
		}
		select {
		case <-ctx.Done():
			return nil, grpcstatus.Error(codes.DeadlineExceeded, fmt.Sprintf("tunnel session did not become ready within %s", timeout))
		case <-ticker.C:
		}
	}
}

func terminalTunnelStatus(status tunnelv1.TunnelSessionStatus) bool {
	switch status {
	case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
		return true
	default:
		return false
	}
}

func (s *Server) GetTunnelSession(ctx context.Context, req *tunnelv1.GetTunnelSessionRequest) (*tunnelv1.GetTunnelSessionResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelGet, publicActionGet)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	session, err := s.deps.Tunnels.Get(ctx, req.GetSessionID(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.GetTunnelSessionResponse{Session: session}, nil
}

func (s *Server) ListTunnelSessions(ctx context.Context, req *tunnelv1.ListTunnelSessionsRequest) (*tunnelv1.ListTunnelSessionsResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelList, publicActionList, withAllocationID(req.GetAllocationID()))
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	sessions, err := s.deps.Tunnels.List(ctx, req.GetAllocationID(), req.GetNodeID(), req.GetIncludeTerminal(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.ListTunnelSessionsResponse{Sessions: sessions}, nil
}

func (s *Server) ListTunnelSessionEvents(ctx context.Context, req *tunnelv1.ListTunnelSessionEventsRequest) (*tunnelv1.ListTunnelSessionEventsResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelListEvents, publicActionListEvents)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	events, err := s.deps.Tunnels.ListEvents(ctx, req.GetSessionID(), req.GetLimit(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.ListTunnelSessionEventsResponse{Events: events}, nil
}

func (s *Server) InspectTunnelSession(ctx context.Context, req *tunnelv1.InspectTunnelSessionRequest) (*tunnelv1.InspectTunnelSessionResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelInspect, publicActionInspect)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	now := s.deps.Now()
	session, err := s.deps.Tunnels.Get(ctx, req.GetSessionID(), now)
	if err != nil {
		opErr = err
		return nil, err
	}
	events, err := s.deps.Tunnels.ListEvents(ctx, req.GetSessionID(), req.GetEventLimit(), now)
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.InspectTunnelSessionResponse{Session: session, Events: events}, nil
}

func (s *Server) RevokeTunnelSession(ctx context.Context, req *tunnelv1.RevokeTunnelSessionRequest) (*tunnelv1.RevokeTunnelSessionResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelRevoke, publicActionRevoke)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	session, err := s.deps.Tunnels.Revoke(ctx, req.GetSessionID(), req.GetReason(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.RevokeTunnelSessionResponse{Session: session}, nil
}

func (s *Server) RenewTunnelSession(ctx context.Context, req *tunnelv1.RenewTunnelSessionRequest) (*tunnelv1.RenewTunnelSessionResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelRenew, publicActionRenew)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	var ttl time.Duration
	if req.GetTtl() != nil {
		ttl = req.GetTtl().AsDuration()
	}
	session, err := s.deps.Tunnels.Renew(ctx, req.GetSessionID(), req.GetClientToken(), ttl, s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.RenewTunnelSessionResponse{Session: session}, nil
}

func (s *Server) ValidateTunnelPeer(ctx context.Context, req *tunnelv1.ValidateTunnelPeerRequest) (*tunnelv1.ValidateTunnelPeerResponse, error) {
	ctx, op := publicOps.Tunnel(ctx, ctrlobs.SpanTunnelValidatePeer, publicActionValidatePeer)
	var opErr error
	defer func() { op.End(opErr) }()
	if s.deps.Tunnels == nil {
		opErr = grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
		return nil, opErr
	}
	session, err := s.deps.Tunnels.ValidatePeer(ctx, req.GetSessionID(), req.GetPeerKind(), req.GetToken(), s.deps.Now())
	if err != nil {
		opErr = err
		return nil, err
	}
	return &tunnelv1.ValidateTunnelPeerResponse{Session: session}, nil
}
