package pgtunnel

import (
	"context"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) expireDue(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil
	}
	_, err := s.db.Pool().Exec(ctx, `
		WITH due AS MATERIALIZED (
			SELECT session_id FROM tunnel_sessions
			WHERE revoked = FALSE AND expires_at <= $2
			ORDER BY session_id
			FOR UPDATE
		), rev AS (
			UPDATE control_revisions
			SET revision = revision + 1
			WHERE name = 'tunnel_sessions' AND EXISTS (SELECT 1 FROM due)
			RETURNING revision
		), updated AS (
		UPDATE tunnel_sessions
		SET status = $1, revoked = TRUE, reason = 'expired', updated_at = $2, revision = (SELECT revision FROM rev)
		WHERE session_id IN (SELECT session_id FROM due) AND EXISTS (SELECT 1 FROM rev)
			RETURNING session_id, status, reason, bound_addr
		)
		INSERT INTO tunnel_session_events (session_id, event_type, status, reason_code, reason, bound_addr, created_at)
		SELECT session_id, $3, status, $4, reason, bound_addr, $2 FROM updated
	`, tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED.String(), now.UTC(), tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_EXPIRED.String(), tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_EXPIRED.String())
	return err
}

func (s *Store) ReconcileExpired(ctx context.Context, now time.Time) error {
	return s.expireDue(ctx, now)
}

func expireDueTx(ctx context.Context, tx pgx.Tx, now time.Time) error {
	_, err := tx.Exec(ctx, `
		WITH due AS MATERIALIZED (
			SELECT session_id FROM tunnel_sessions
			WHERE revoked = FALSE AND expires_at <= $2
			ORDER BY session_id
			FOR UPDATE
		), rev AS (
			UPDATE control_revisions
			SET revision = revision + 1
			WHERE name = 'tunnel_sessions' AND EXISTS (SELECT 1 FROM due)
			RETURNING revision
		), updated AS (
		UPDATE tunnel_sessions
		SET status = $1, revoked = TRUE, reason = 'expired', updated_at = $2, revision = (SELECT revision FROM rev)
		WHERE session_id IN (SELECT session_id FROM due) AND EXISTS (SELECT 1 FROM rev)
			RETURNING session_id, status, reason, bound_addr
		)
		INSERT INTO tunnel_session_events (session_id, event_type, status, reason_code, reason, bound_addr, created_at)
		SELECT session_id, $3, status, $4, reason, bound_addr, $2 FROM updated
	`, tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_EXPIRED.String(), now.UTC(), tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_EXPIRED.String(), tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_EXPIRED.String())
	return err
}
