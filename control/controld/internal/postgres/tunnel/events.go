package pgtunnel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultEventLimit = 50
const maxEventLimit = 500

type eventParams struct {
	SessionID  string
	Type       tunnelv1.TunnelSessionEventType
	Status     tunnelv1.TunnelSessionStatus
	ReasonCode tunnelv1.TunnelSessionEventReasonCode
	Reason     string
	BoundAddr  string
	RelayID    string
	PeerKind   tunnelv1.TunnelPeerKind
	BytesIn    int64
	BytesOut   int64
	Now        time.Time
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, limit int32, now time.Time) ([]*tunnelv1.TunnelSessionEvent, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := s.expireDue(ctx, now.UTC()); err != nil {
		return nil, err
	}
	var exists int
	if err := s.db.Pool().QueryRow(ctx, `SELECT 1 FROM tunnel_sessions WHERE session_id = $1`, sessionID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "tunnel session not found")
	} else if err != nil {
		return nil, fmt.Errorf("check tunnel session before listing events: %w", err)
	}
	normalizedLimit := normalizeEventLimit(limit)
	rows, err := s.db.Pool().Query(ctx, `
		SELECT event_id, session_id, event_type, status, reason_code, reason, bound_addr, relay_id, peer_kind, bytes_in, bytes_out, created_at
		FROM tunnel_session_events
		WHERE session_id = $1
		ORDER BY event_id DESC
		LIMIT $2
	`, sessionID, normalizedLimit)
	if err != nil {
		return nil, fmt.Errorf("query tunnel session events: %w", err)
	}
	defer rows.Close()
	var events []*tunnelv1.TunnelSessionEvent
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func normalizeEventLimit(limit int32) int32 {
	if limit <= 0 {
		return defaultEventLimit
	}
	if limit > maxEventLimit {
		return maxEventLimit
	}
	return limit
}

func (s *Store) insertEventTx(ctx context.Context, tx pgx.Tx, params eventParams) error {
	sessionID := strings.TrimSpace(params.SessionID)
	if sessionID == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO tunnel_session_events (session_id, event_type, status, reason_code, reason, bound_addr, relay_id, peer_kind, bytes_in, bytes_out, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, sessionID, params.Type.String(), params.Status.String(), params.ReasonCode.String(), strings.TrimSpace(params.Reason), strings.TrimSpace(params.BoundAddr), strings.TrimSpace(params.RelayID), params.PeerKind.String(), params.BytesIn, params.BytesOut, params.Now.UTC())
	return err
}

func scanEvent(row rowScanner) (*tunnelv1.TunnelSessionEvent, error) {
	var (
		event      tunnelv1.TunnelSessionEvent
		eventText  string
		status     string
		reasonCode string
		peerKind   string
		createdAt  time.Time
	)
	err := row.Scan(&event.EventID, &event.SessionID, &eventText, &status, &reasonCode, &event.Reason, &event.BoundAddr, &event.RelayID, &peerKind, &event.BytesIn, &event.BytesOut, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "tunnel session event not found")
	}
	if err != nil {
		return nil, err
	}
	event.EventType = parseEventType(eventText)
	event.Status = parseStatus(status)
	event.ReasonCode = parseReasonCode(reasonCode)
	event.PeerKind = parsePeerKind(peerKind)
	event.CreatedAt = timestamppb.New(createdAt)
	return &event, nil
}

func parseEventType(value string) tunnelv1.TunnelSessionEventType {
	if n, ok := tunnelv1.TunnelSessionEventType_value[value]; ok {
		return tunnelv1.TunnelSessionEventType(n)
	}
	return tunnelv1.TunnelSessionEventType_TUNNEL_SESSION_EVENT_TYPE_UNSPECIFIED
}

func parseReasonCode(value string) tunnelv1.TunnelSessionEventReasonCode {
	if n, ok := tunnelv1.TunnelSessionEventReasonCode_value[value]; ok {
		return tunnelv1.TunnelSessionEventReasonCode(n)
	}
	return tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED
}

func parsePeerKind(value string) tunnelv1.TunnelPeerKind {
	if n, ok := tunnelv1.TunnelPeerKind_value[value]; ok {
		return tunnelv1.TunnelPeerKind(n)
	}
	return tunnelv1.TunnelPeerKind_TUNNEL_PEER_KIND_UNSPECIFIED
}

func nodeStatusReasonCode(status tunnelv1.TunnelSessionStatus) tunnelv1.TunnelSessionEventReasonCode {
	switch status {
	case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING:
		return tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_NODE_RUNNING
	case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED:
		return tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_NODE_DEGRADED
	case tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED:
		return tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_NODE_FAILED
	default:
		return tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_UNSPECIFIED
	}
}
