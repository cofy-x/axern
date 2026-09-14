package pgtunnel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) Get(ctx context.Context, sessionID string, now time.Time) (*tunnelv1.TunnelSession, error) {
	session, _, err := s.getWithTokens(ctx, strings.TrimSpace(sessionID), now)
	return session, err
}

func (s *Store) ResolveRelayTarget(ctx context.Context, sessionID string, now time.Time) (string, error) {
	session, internal, err := s.getWithTokens(ctx, strings.TrimSpace(sessionID), now)
	if err != nil {
		return "", err
	}
	if terminal(session.GetStatus()) {
		return "", grpcstatus.Error(codes.FailedPrecondition, "tunnel session is terminal")
	}
	if strings.TrimSpace(internal.nodeEdgeTarget) == "" {
		return "", grpcstatus.Error(codes.Unavailable, "tunnel relay target is not ready")
	}
	return internal.nodeEdgeTarget, nil
}

func (s *Store) List(ctx context.Context, namespace, allocationID, nodeID string, includeTerminal bool, now time.Time) ([]*tunnelv1.TunnelSession, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	if err := s.expireDue(ctx, now.UTC()); err != nil {
		return nil, fmt.Errorf("expire tunnel sessions before list: %w", err)
	}
	conds := []string{"TRUE"}
	args := []any{}
	if v := strings.TrimSpace(namespace); v != "" {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("namespace = $%d", len(args)))
	}
	if v := strings.TrimSpace(allocationID); v != "" {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("allocation_id = $%d", len(args)))
	}
	if v := strings.TrimSpace(nodeID); v != "" {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if !includeTerminal {
		conds = append(conds, "status NOT IN ('TUNNEL_SESSION_STATUS_REVOKED','TUNNEL_SESSION_STATUS_EXPIRED','TUNNEL_SESSION_STATUS_FAILED')")
	}
	rows, err := s.db.Pool().Query(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE `+strings.Join(conds, " AND ")+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query tunnel sessions: %w", err)
	}
	defer rows.Close()
	var out []*tunnelv1.TunnelSession
	for rows.Next() {
		session, _, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, session)
	}
	return out, rows.Err()
}

func (s *Store) WatchNode(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*nodev1.NodeTunnelSession, int64, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, 0, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	if s.watches == nil {
		return nil, 0, grpcstatus.Error(codes.FailedPrecondition, "tunnel session watch is not configured")
	}
	nodeID = strings.TrimSpace(nodeID)
	subscription, err := s.watches.subscribe(ctx, nodeID)
	if err != nil {
		return nil, afterRevision, fmt.Errorf("subscribe tunnel session changes: %w", err)
	}
	defer subscription.close()
	clockStartedAt := time.Now()
	baseTime := now.UTC()
	for {
		currentTime := baseTime.Add(time.Since(clockStartedAt))
		if err := s.expireDue(ctx, currentTime); err != nil {
			return nil, afterRevision, err
		}
		sessions, revision, err := s.loadNodeSessions(ctx, nodeID, afterRevision)
		if err != nil {
			return nil, afterRevision, err
		}
		if revision > afterRevision {
			return sessions, revision, nil
		}
		nextExpiry, hasExpiry, err := s.nextNodeExpiry(ctx, nodeID)
		if err != nil {
			return nil, afterRevision, err
		}
		if !hasExpiry {
			err = subscription.wait(ctx)
		} else {
			_, err = subscription.waitFor(ctx, nextExpiry.Sub(currentTime))
		}
		if err != nil {
			return nil, afterRevision, err
		}
	}
}

func (s *Store) nextNodeExpiry(ctx context.Context, nodeID string) (time.Time, bool, error) {
	var expiresAt time.Time
	err := s.db.Pool().QueryRow(ctx, `
		SELECT expires_at
		FROM tunnel_sessions
		WHERE node_id = $1
		  AND status IN (
			'TUNNEL_SESSION_STATUS_PENDING',
			'TUNNEL_SESSION_STATUS_RUNNING',
			'TUNNEL_SESSION_STATUS_DEGRADED'
		  )
		ORDER BY expires_at ASC
		LIMIT 1
	`, nodeID).Scan(&expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("query next node tunnel expiry: %w", err)
	}
	return expiresAt.UTC(), true, nil
}

func (s *Store) loadNodeSessions(ctx context.Context, nodeID string, afterRevision int64) ([]*nodev1.NodeTunnelSession, int64, error) {
	// Fix the high-water mark before reading rows. A session committed after
	// this query is intentionally delivered by the next response.
	revision, err := currentRevision(ctx, s.db.Pool())
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT `+sessionSelectColumns()+`
		FROM tunnel_sessions
		WHERE node_id = $1
		  AND revision > $2
		  AND revision <= $3
		ORDER BY revision ASC
	`, nodeID, afterRevision, revision)
	if err != nil {
		return nil, 0, fmt.Errorf("query node tunnel sessions: %w", err)
	}
	defer rows.Close()
	var out []*nodev1.NodeTunnelSession
	for rows.Next() {
		session, internal, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		nodeToken, err := s.decryptNodeToken(internal.nodeTokenCipher)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, &nodev1.NodeTunnelSession{Session: session, NodeToken: nodeToken, NodeEdgeTarget: internal.nodeEdgeTarget})
	}
	return out, revision, rows.Err()
}

func (s *Store) getWithTokens(ctx context.Context, sessionID string, now time.Time) (*tunnelv1.TunnelSession, sessionInternal, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, sessionInternal{}, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	if sessionID == "" {
		return nil, sessionInternal{}, grpcstatus.Error(codes.InvalidArgument, "session_id is required")
	}
	if err := s.expireDue(ctx, now.UTC()); err != nil {
		return nil, sessionInternal{}, fmt.Errorf("expire tunnel sessions before get: %w", err)
	}
	session, internal, err := scanSession(s.db.Pool().QueryRow(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE session_id = $1`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, sessionInternal{}, grpcstatus.Error(codes.NotFound, "tunnel session not found")
	}
	return session, internal, err
}
