package pgnodes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Enroll serializes first issuance with Node revocation and retirement. Signing and recording
// its reply happen before commit; no certificate is returned on commit failure.
func (s *PGStore) Enroll(ctx context.Context, request nodekernel.EnrollmentRequest, signer nodekernel.EnrollmentSigner) ([]byte, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var hash, lifecycle string
	var admitted time.Time
	err = tx.QueryRow(ctx, "SELECT enrollment_token_hash,lifecycle_status,admitted_at FROM nodes WHERE node_id=$1 FOR UPDATE", request.NodeID).Scan(&hash, &lifecycle, &admitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.Unauthenticated, "invalid node enrollment")
	}
	if err != nil {
		return nil, fmt.Errorf("lock node enrollment: %w", err)
	}
	var now time.Time
	if err := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return nil, err
	}
	if lifecycle != "active" || !enrollmentTokenHashMatches(hash, request.Token) || now.Before(admitted) || !now.Before(admitted.Add(nodekernel.EnrollmentLifetime)) {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired node enrollment")
	}
	digest := sha256.Sum256(request.CSRDER)
	var existingHash, certificate []byte
	err = tx.QueryRow(ctx, "SELECT csr_sha256,certificate_pem FROM node_enrollment_receipts WHERE node_id=$1", request.NodeID).Scan(&existingHash, &certificate)
	if err == nil {
		if !bytes.Equal(existingHash, digest[:]) {
			return nil, status.Error(codes.PermissionDenied, "node enrollment has already bound another CSR")
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return certificate, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	certificate, err = signer.SignNode(request.CSRDER, request.NodeID, now)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "INSERT INTO node_enrollment_receipts(node_id,csr_sha256,certificate_pem,created_at) VALUES($1,$2,$3,$4)", request.NodeID, digest[:], certificate, now.UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit node enrollment: %w", err)
	}
	return certificate, nil
}

func (s *PGStore) RenewCertificate(ctx context.Context, nodeID string, csrDER []byte, now time.Time, signer nodekernel.EnrollmentSigner) ([]byte, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var lifecycle string
	err = tx.QueryRow(ctx, "SELECT lifecycle_status FROM nodes WHERE node_id=$1 FOR UPDATE", nodeID).Scan(&lifecycle)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.Unauthenticated, "node identity is not admitted")
	}
	if err != nil {
		return nil, err
	}
	if lifecycle != "active" {
		return nil, status.Error(codes.PermissionDenied, "node identity is not active")
	}
	certificate, err := signer.SignNode(csrDER, nodeID, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return certificate, nil
}
