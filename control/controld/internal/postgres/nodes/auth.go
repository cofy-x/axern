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

func (s *PGStore) Authenticate(ctx context.Context, nodeID, nodeCredential string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" || strings.TrimSpace(nodeCredential) == "" {
		return grpcstatus.Error(codes.PermissionDenied, "node credential is required")
	}
	var hash, lifecycle string
	err := s.db.Pool().QueryRow(ctx, `SELECT node_credential_hash, lifecycle_status FROM nodes WHERE node_id = $1`, nodeID).Scan(&hash, &lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return grpcstatus.Error(codes.PermissionDenied, "node identity is unknown")
	}
	if err != nil {
		return fmt.Errorf("load node credential: %w", err)
	}
	if lifecycle != "active" {
		return grpcstatus.Error(codes.FailedPrecondition, "node is retired")
	}
	if strings.TrimSpace(hash) == "" {
		return grpcstatus.Error(codes.PermissionDenied, "node authentication credential is unavailable")
	}
	if !nodeCredentialHashMatches(hash, nodeCredential) {
		return grpcstatus.Error(codes.PermissionDenied, "invalid node credential")
	}
	return nil
}

func hashNodeCredential(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func nodeCredentialHashMatches(hash, credential string) bool {
	want := hashNodeCredential(credential)
	return subtle.ConstantTimeCompare([]byte(hash), []byte(want)) == 1
}
