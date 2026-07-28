package pgtunnel

import (
	"context"
	"errors"
	"strings"
	"time"

	tunnelkernel "github.com/cofy-x/axern/control/controld/internal/kernel/tunnel"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) ReportPeerEvent(ctx context.Context, params tunnelkernel.PeerEventParams, now time.Time) (*tunnelv1.TunnelSession, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	sessionID := strings.TrimSpace(params.SessionID)
	if sessionID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "session_id is required")
	}
	now = now.UTC()
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := expireDueTx(ctx, tx, now); err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE session_id = $1 FOR UPDATE`, sessionID)
	current, clientHash, _, nodeHash, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "tunnel session not found")
	}
	if err != nil {
		return nil, err
	}
	if params.RelayID != "" && current.GetRelayID() != "" && params.RelayID != current.GetRelayID() {
		return nil, grpcstatus.Error(codes.PermissionDenied, "tunnel peer event relay_id does not match session")
	}
	gotTokenHash := hashToken(params.PeerToken)
	switch params.PeerKind {
	case tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT:
		if gotTokenHash != clientHash {
			return nil, grpcstatus.Error(codes.PermissionDenied, "invalid tunnel client token")
		}
	case tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE:
		if gotTokenHash != nodeHash {
			return nil, grpcstatus.Error(codes.PermissionDenied, "invalid tunnel node token")
		}
	default:
		if gotTokenHash != clientHash && gotTokenHash != nodeHash {
			return nil, grpcstatus.Error(codes.PermissionDenied, "invalid tunnel peer token")
		}
	}
	revision, err := nextRevision(ctx, tx)
	if err != nil {
		return nil, err
	}
	row = tx.QueryRow(ctx, `
		UPDATE tunnel_sessions
		SET last_peer_event_at = $2,
		    bytes_in = bytes_in + $3,
		    bytes_out = bytes_out + $4,
		    updated_at = $2,
		    revision = $5
		WHERE session_id = $1
		RETURNING `+sessionSelectColumns(), sessionID, now, params.BytesIn, params.BytesOut, revision)
	session, _, _, _, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	if err := s.insertEventTx(ctx, tx, eventParams{
		SessionID:  sessionID,
		Type:       params.EventType,
		Status:     session.GetStatus(),
		ReasonCode: params.ReasonCode,
		Reason:     params.Reason,
		BoundAddr:  session.GetBoundAddr(),
		RelayID:    firstNonEmpty(params.RelayID, session.GetRelayID()),
		PeerKind:   params.PeerKind,
		BytesIn:    params.BytesIn,
		BytesOut:   params.BytesOut,
		Now:        now,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return session, nil
}
