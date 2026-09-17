package pgtunnel

import (
	"context"
	"strings"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) ValidatePeer(ctx context.Context, sessionID string, kind tunnelv1.TunnelPeerKind, token string, now time.Time) (*tunnelv1.TunnelSession, error) {
	session, internal, err := s.getWithTokens(ctx, strings.TrimSpace(sessionID), now.UTC())
	if err != nil {
		return nil, err
	}
	var active bool
	if err := s.db.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM nodes WHERE node_id = $1 AND lifecycle_status = 'active')", internal.nodeID).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, grpcstatus.Error(codes.PermissionDenied, "Node identity is not active")
	}
	if terminal(session.GetStatus()) {
		return nil, grpcstatus.Error(codes.PermissionDenied, "tunnel session is not active")
	}
	if !now.UTC().Before(session.GetExpiresAt().AsTime()) {
		return nil, grpcstatus.Error(codes.PermissionDenied, "tunnel session is expired")
	}
	got := hashToken(strings.TrimSpace(token))
	switch kind {
	case tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT:
		if got != internal.clientTokenHash {
			return nil, grpcstatus.Error(codes.PermissionDenied, "invalid tunnel client token")
		}
	case tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE:
		if got != internal.nodeTokenHash {
			return nil, grpcstatus.Error(codes.PermissionDenied, "invalid tunnel node token")
		}
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "peer_kind is required")
	}
	return session, nil
}
