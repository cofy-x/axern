package pgtunnel

import (
	"context"
	"errors"
	"strings"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) Revoke(ctx context.Context, sessionID, reason string, now time.Time) (*tunnelv1.TunnelSession, error) {
	return s.updateStatus(ctx, sessionID, tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED, reason, "", true, now.UTC())
}

func (s *Store) ReportStatus(ctx context.Context, nodeID, sessionID string, status tunnelv1.TunnelSessionStatus, reason, boundAddr string, now time.Time) (*tunnelv1.TunnelSession, error) {
	switch status {
	case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "status must be running, degraded, or failed")
	}
	if strings.TrimSpace(nodeID) == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	return s.reportNodeStatus(ctx, nodeID, sessionID, status, reason, boundAddr, now.UTC())
}

func (s *Store) updateStatus(ctx context.Context, sessionID string, status tunnelv1.TunnelSessionStatus, reason, boundAddr string, revoked bool, now time.Time) (*tunnelv1.TunnelSession, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE session_id = $1 FOR UPDATE`, sessionID)
	current, _, _, _, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "tunnel session not found")
	}
	if err != nil {
		return nil, err
	}

	revision, err := nextRevision(ctx, tx)
	if err != nil {
		return nil, err
	}
	row = tx.QueryRow(ctx, `
		UPDATE tunnel_sessions
		SET status = $2, reason = $3, bound_addr = COALESCE(NULLIF($4, ''), bound_addr),
		    revoked = revoked OR $5, updated_at = $6, revision = $7
		WHERE session_id = $1
		RETURNING `+sessionSelectColumns(), current.GetSessionID(), status.String(), strings.TrimSpace(reason), strings.TrimSpace(boundAddr), revoked, now, revision)
	session, _, _, _, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	eventType := tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_STATUS
	reasonCode := nodeStatusReasonCode(status)
	if status == tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED {
		eventType = tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_REVOKED
		reasonCode = tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_MANUAL_REVOKE
	}
	if err := s.insertEventTx(ctx, tx, eventParams{
		SessionID:  session.GetSessionID(),
		Type:       eventType,
		Status:     session.GetStatus(),
		ReasonCode: reasonCode,
		Reason:     session.GetReason(),
		BoundAddr:  session.GetBoundAddr(),
		Now:        now,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return session, nil
}

func terminal(status tunnelv1.TunnelSessionStatus) bool {
	switch status {
	case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
		return true
	default:
		return false
	}
}
