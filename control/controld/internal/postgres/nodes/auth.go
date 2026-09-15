package pgnodes

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *PGStore) RequireActive(ctx context.Context, nodeID string) error {
	if nodeID == "" {
		return grpcstatus.Error(codes.PermissionDenied, "Node identity is required")
	}
	var lifecycle string
	err := s.db.Pool().QueryRow(ctx, "SELECT lifecycle_status FROM nodes WHERE node_id=$1", nodeID).Scan(&lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return grpcstatus.Error(codes.PermissionDenied, "Node identity is unknown")
	}
	if err != nil {
		return fmt.Errorf("load Node admission: %w", err)
	}
	if lifecycle != "active" {
		return grpcstatus.Error(codes.FailedPrecondition, "Node identity is not active")
	}
	return nil
}

func hashEnrollmentToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func enrollmentTokenHashMatches(hash, credential string) bool {
	want := hashEnrollmentToken(credential)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(want)) == 1
}
