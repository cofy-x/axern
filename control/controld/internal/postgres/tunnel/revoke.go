package pgtunnel

import (
	"context"
	"strings"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
)

type RevokeActiveForAllocationsRequest struct {
	AllocationIDs []string
	Reason        string
	ReasonCode    tunnelv1.TunnelSessionEventReasonCode
	Now           time.Time
}

func RevokeActiveForAllocationsTx(ctx context.Context, tx pgx.Tx, req RevokeActiveForAllocationsRequest) error {
	if len(req.AllocationIDs) == 0 {
		return nil
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "allocation deleted"
	}
	reasonCode := req.ReasonCode
	if reasonCode == tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED {
		reasonCode = tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED
	}
	now := req.Now.UTC()
	_, err := tx.Exec(ctx, `
		WITH due AS MATERIALIZED (
			SELECT session_id FROM tunnel_sessions
			WHERE allocation_id = ANY($2)
			  AND revoked = FALSE
			  AND status IN (
				'TUNNEL_SESSION_STATUS_PENDING',
				'TUNNEL_SESSION_STATUS_RUNNING',
				'TUNNEL_SESSION_STATUS_DEGRADED'
			  )
			ORDER BY session_id
			FOR UPDATE
		), rev AS (
			UPDATE control_revisions
			SET revision = revision + 1
			WHERE name = 'tunnel_sessions' AND EXISTS (SELECT 1 FROM due)
			RETURNING revision
		), updated AS (
		UPDATE tunnel_sessions
		SET status = $1, revoked = TRUE, reason = $3, updated_at = $4, revision = (SELECT revision FROM rev)
		WHERE session_id IN (SELECT session_id FROM due)
		  AND EXISTS (SELECT 1 FROM rev)
			RETURNING session_id, status, reason, bound_addr
		)
		INSERT INTO tunnel_session_events (session_id, event_type, status, reason_code, reason, bound_addr, created_at)
		SELECT session_id, $5, status, $6, reason, bound_addr, $4 FROM updated
	`, tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_REVOKED.String(), req.AllocationIDs, reason, now, tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_REVOKED.String(), reasonCode.String())
	return err
}
