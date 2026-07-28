package pgnodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *PGStore) Authenticate(ctx context.Context, nodeID, nodeAuthToken string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || strings.TrimSpace(nodeAuthToken) == "" {
		return grpcstatus.Error(codes.PermissionDenied, "node auth token is required")
	}
	var hash, lifecycle string
	err := s.db.Pool().QueryRow(ctx, `SELECT node_auth_token_hash, lifecycle_status FROM nodes WHERE node_id = $1`, nodeID).Scan(&hash, &lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return grpcstatus.Error(codes.PermissionDenied, "node is not registered")
	}
	if err != nil {
		return fmt.Errorf("load node auth token: %w", err)
	}
	if lifecycle != "active" {
		return grpcstatus.Error(codes.FailedPrecondition, "node is retired")
	}
	if strings.TrimSpace(hash) == "" {
		return grpcstatus.Error(codes.PermissionDenied, "node auth token is not registered")
	}
	if hash != hashNodeAuthToken(nodeAuthToken) {
		return grpcstatus.Error(codes.PermissionDenied, "invalid node auth token")
	}
	return nil
}

func hashNodeAuthToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}
