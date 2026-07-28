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
	session, _, _, err := s.getWithTokens(ctx, strings.TrimSpace(sessionID), now)
	return session, err
}

func (s *Store) List(ctx context.Context, allocationID, nodeID string, includeTerminal bool, now time.Time) ([]*tunnelv1.TunnelSession, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	_ = s.expireDue(ctx, now.UTC())
	conds := []string{"TRUE"}
	args := []any{}
	if v := strings.TrimSpace(allocationID); v != "" {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("allocation_id = $%d", len(args)))
	}
	if v := strings.TrimSpace(nodeID); v != "" {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf("node_id = $%d", len(args)))
	}
	if !includeTerminal {
		conds = append(conds, "revoked = FALSE")
		conds = append(conds, "status NOT IN ('TUNNEL_SESSION_STATUS_REVOKED','TUNNEL_SESSION_STATUS_EXPIRED','TUNNEL_SESSION_STATUS_FAILED')")
	}
	rows, err := s.db.Pool().Query(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE `+strings.Join(conds, " AND ")+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("query tunnel sessions: %w", err)
	}
	defer rows.Close()
	var out []*tunnelv1.TunnelSession
	for rows.Next() {
		session, _, _, _, err := scanSession(rows)
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
	_ = s.expireDue(ctx, now.UTC())
	revision, err := currentRevision(ctx, s.db.Pool())
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT `+sessionSelectColumns()+`
		FROM tunnel_sessions
		WHERE node_id = $1
		  AND revision > $2
		ORDER BY revision ASC
	`, strings.TrimSpace(nodeID), afterRevision)
	if err != nil {
		return nil, 0, fmt.Errorf("query node tunnel sessions: %w", err)
	}
	defer rows.Close()
	var out []*nodev1.NodeTunnelSession
	for rows.Next() {
		session, _, nodeCipher, _, err := scanSession(rows)
		if err != nil {
			return nil, 0, err
		}
		nodeToken, err := s.decryptNodeToken(nodeCipher)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, &nodev1.NodeTunnelSession{Session: session, NodeToken: nodeToken})
	}
	return out, revision, rows.Err()
}

func (s *Store) getWithTokens(ctx context.Context, sessionID string, now time.Time) (*tunnelv1.TunnelSession, string, string, error) {
	if s == nil || s.db == nil || s.db.Pool() == nil {
		return nil, "", "", grpcstatus.Error(codes.FailedPrecondition, "tunnel store is not configured")
	}
	if sessionID == "" {
		return nil, "", "", grpcstatus.Error(codes.InvalidArgument, "session_id is required")
	}
	_ = s.expireDue(ctx, now.UTC())
	session, clientHash, _, nodeHash, err := scanSession(s.db.Pool().QueryRow(ctx, `SELECT `+sessionSelectColumns()+` FROM tunnel_sessions WHERE session_id = $1`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", grpcstatus.Error(codes.NotFound, "tunnel session not found")
	}
	return session, clientHash, nodeHash, err
}
