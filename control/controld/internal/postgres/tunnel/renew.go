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

func (s *Store) Renew(ctx context.Context, sessionID, clientToken string, ttl time.Duration, now time.Time) (*tunnelv1.TunnelSession, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "session_id is required")
	}
	clientToken = strings.TrimSpace(clientToken)
	if clientToken == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "client_token is required")
	}
	now = now.UTC()
	expiresAt := now.Add(normalizeTTL(ttl))

	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := expireDueTx(ctx, tx, now); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE session_id = $1 FOR UPDATE`, sessionID)
	current, clientHash, _, _, err := scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "tunnel session not found")
	}
	if err != nil {
		return nil, err
	}
	if current.GetRevoked() || terminal(current.GetStatus()) {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel session is terminal")
	}
	if hashToken(clientToken) != clientHash {
		return nil, grpcstatus.Error(codes.PermissionDenied, "invalid tunnel client token")
	}

	revision, err := nextRevision(ctx, tx)
	if err != nil {
		return nil, err
	}
	row = tx.QueryRow(ctx, `
		UPDATE tunnel_sessions
		SET expires_at = $2, updated_at = $3, revision = $4
		WHERE session_id = $1
		RETURNING `+sessionSelectColumns(), sessionID, expiresAt, now, revision)
	session, _, _, _, err := scanSession(row)
	if err != nil {
		return nil, err
	}
	if err := s.insertEventTx(ctx, tx, eventParams{
		SessionID:  session.GetSessionID(),
		Type:       tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_RENEWED,
		Status:     session.GetStatus(),
		ReasonCode: tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_SESSION_RENEWED,
		Now:        now,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return session, nil
}
