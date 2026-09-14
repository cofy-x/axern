package pgrun

import (
	"context"
	"errors"
	"fmt"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) IssueAllocationAccessGrant(ctx context.Context, allocationID string, ttl time.Duration, now time.Time) (*accessgrantkernel.IssuedGrant, error) {
	if ttl <= 0 {
		ttl = defaultAllocationAccessGrantTTL
	}
	var grant *accessgrantkernel.IssuedGrant
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		alloc, err := s.currentAllocation(ctx, tx, allocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		}
		if err != nil {
			return err
		}
		token := accessgrantkernel.NewPlaintextToken()
		hash := accessgrantkernel.HashToken(token)
		revision, err := s.nextAccessGrantRevision(ctx, tx)
		if err != nil {
			return err
		}
		grant = &accessgrantkernel.IssuedGrant{
			Record: accessgrantkernel.Record{
				GrantID: "grant-" + uuid.NewString(), AllocationID: alloc.AllocationID,
				NodeID: alloc.NodeID, ValidationTokenHash: hash, Revision: revision,
				ExpiresAt: now.Add(ttl).UTC(),
			},
			PlaintextToken: token,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocation_access_grants (
				grant_id, allocation_id, node_id, expires_at, revision,
				revoked, token_hash, created_at
			) VALUES ($1, $2, $3, $4, $5, false, $6, $7)
		`, grant.GrantID, grant.AllocationID, grant.NodeID, grant.ExpiresAt, revision, hash, now.UTC()); err != nil {
			return fmt.Errorf("insert allocation access grant: %w", err)
		}
		return nil
	})
	return grant, err
}
