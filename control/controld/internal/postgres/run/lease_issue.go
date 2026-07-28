package pgrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Store) IssueExecutionLease(ctx context.Context, allocationID string, attempt int64, leaseType commonv1.LeaseType, ttl time.Duration, now time.Time) (*commonv1.ExecutionLease, error) {
	if ttl <= 0 {
		ttl = defaultExecutionLeaseTTL
	}
	var lease *commonv1.ExecutionLease
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		alloc, err := s.currentAllocation(ctx, tx, allocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		}
		if err != nil {
			return err
		}
		if alloc.Attempt != attempt {
			return grpcstatus.Error(codes.FailedPrecondition, "allocation attempt is not current")
		}
		token := leasekernel.NewPlaintextToken()
		hash := leasekernel.HashToken(token)
		revision, err := s.nextLeaseRevision(ctx, tx)
		if err != nil {
			return err
		}
		lease = &commonv1.ExecutionLease{
			LeaseID:             "lease-" + uuid.NewString(),
			AllocationID:        alloc.AllocationID,
			NodeID:              alloc.NodeID,
			Attempt:             alloc.Attempt,
			LeaseType:           leaseType,
			PlaintextToken:      token,
			ValidationTokenHash: hash,
			Revision:            revision,
			ExpiresAt:           timestamppb.New(now.Add(ttl)),
			NodeTarget:          alloc.NodeTarget,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO execution_leases (
				lease_id, allocation_id, node_id, node_target, attempt, lease_type,
				expires_at, revision, revoked, token_hash, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, false, $9, $10)
		`, lease.GetLeaseID(), lease.GetAllocationID(), lease.GetNodeID(), lease.GetNodeTarget(), lease.GetAttempt(), lease.GetLeaseType().String(), lease.GetExpiresAt().AsTime().UTC(), revision, hash, now.UTC()); err != nil {
			return fmt.Errorf("insert execution lease: %w", err)
		}
		return nil
	})
	return lease, err
}
