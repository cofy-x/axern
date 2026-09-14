package pgrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) IssueExecutionLease(ctx context.Context, allocationID string, ttl time.Duration, now time.Time) (*leasekernel.IssuedGrant, error) {
	if ttl <= 0 {
		ttl = defaultExecutionLeaseTTL
	}
	var lease *leasekernel.IssuedGrant
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		alloc, err := s.currentAllocation(ctx, tx, allocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		}
		if err != nil {
			return err
		}
		token := leasekernel.NewPlaintextToken()
		hash := leasekernel.HashToken(token)
		revision, err := s.nextLeaseRevision(ctx, tx)
		if err != nil {
			return err
		}
		lease = &leasekernel.IssuedGrant{
			Record: leasekernel.Record{
				LeaseID:             "lease-" + uuid.NewString(),
				AllocationID:        alloc.AllocationID,
				NodeID:              alloc.NodeID,
				ValidationTokenHash: hash,
				Revision:            revision,
				ExpiresAt:           now.Add(ttl).UTC(),
			},
			PlaintextToken: token,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO execution_leases (
				lease_id, allocation_id, node_id, expires_at, revision,
				revoked, token_hash, created_at
			) VALUES ($1, $2, $3, $4, $5, false, $6, $7)
		`, lease.LeaseID, lease.AllocationID, lease.NodeID, lease.ExpiresAt, revision, hash, now.UTC()); err != nil {
			return fmt.Errorf("insert execution lease: %w", err)
		}
		return nil
	})
	return lease, err
}
