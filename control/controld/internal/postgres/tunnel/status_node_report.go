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

func (s *Store) reportNodeStatus(ctx context.Context, nodeID, sessionID string, status tunnelv1.TunnelSessionStatus, reason, boundAddr string, now time.Time) (*tunnelv1.TunnelSession, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := expireDueTx(ctx, tx, now); err != nil {
		return nil, err
	}
	row := tx.QueryRow(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE session_id = $1 AND node_id = $2 FOR UPDATE`, strings.TrimSpace(sessionID), strings.TrimSpace(nodeID))
	current, _, _, _, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "tunnel session not found")
	}
	if err != nil {
		return nil, err
	}
	if current.GetRevoked() || terminal(current.GetStatus()) {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return current, nil
	}

	revision, err := nextRevision(ctx, tx)
	if err != nil {
		return nil, err
	}
	row = tx.QueryRow(ctx, `
		UPDATE tunnel_sessions
		SET status = $2, reason = $3, bound_addr = COALESCE(NULLIF($4, ''), bound_addr),
		    ready_at = CASE WHEN $2 = 'TUNNEL_SESSION_STATUS_RUNNING' AND ready_at IS NULL THEN $5 ELSE ready_at END,
		    updated_at = $5, revision = $6
		WHERE session_id = $1 AND node_id = $7
		RETURNING `+sessionSelectColumns(), strings.TrimSpace(sessionID), status.String(), strings.TrimSpace(reason), strings.TrimSpace(boundAddr), now, revision, strings.TrimSpace(nodeID))
	session, _, _, _, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	if err := s.insertEventTx(ctx, tx, eventParams{
		SessionID:  session.GetSessionID(),
		Type:       tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_NODE_STATUS,
		Status:     session.GetStatus(),
		ReasonCode: nodeStatusReasonCode(session.GetStatus()),
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
