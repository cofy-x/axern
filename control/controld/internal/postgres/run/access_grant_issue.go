package pgrun

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) IssueAllocationAccessGrant(ctx context.Context, allocationID string, purpose gatewayv1.AllocationAccessPurpose, ttl time.Duration, now time.Time) (*accessgrantkernel.IssuedGrant, error) {
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
		var state string
		var outputExpiry sql.NullTime
		if err := tx.QueryRow(ctx, "SELECT lifecycle_state, output_expires_at FROM allocations WHERE allocation_id = $1 FOR UPDATE", allocationID).Scan(&state, &outputExpiry); err != nil {
			return err
		}
		switch purpose {
		case gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE:
			if state != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String() {
				return grpcstatus.Error(codes.FailedPrecondition, "allocation is not active")
			}
		case gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT:
			if state != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String() {
				if !outputExpiry.Valid || !now.Before(outputExpiry.Time) {
					return grpcstatus.Error(codes.FailedPrecondition, "allocation output is unavailable or expired")
				}
				if now.Add(ttl).After(outputExpiry.Time) {
					ttl = outputExpiry.Time.Sub(now)
				}
			}
		default:
			return grpcstatus.Error(codes.InvalidArgument, "allocation access purpose is required")
		}
		token := accessgrantkernel.NewPlaintextToken()
		hash := accessgrantkernel.HashToken(token)
		revision, err := pgallocation.NextAccessGrantRevision(ctx, tx, alloc.NodeID)
		if err != nil {
			return err
		}
		grant = &accessgrantkernel.IssuedGrant{
			Record: accessgrantkernel.Record{
				Purpose: purpose,
				GrantID: "grant-" + uuid.NewString(), AllocationID: alloc.AllocationID,
				NodeID: alloc.NodeID, ValidationTokenHash: hash, Revision: revision,
				ExpiresAt: now.Add(ttl).UTC(),
			},
			PlaintextToken: token,
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocation_access_grants (
				grant_id, allocation_id, node_id, expires_at, revision,
				revoked, token_hash, created_at, purpose
			) VALUES ($1, $2, $3, $4, $5, false, $6, $7, $8)
		`, grant.GrantID, grant.AllocationID, grant.NodeID, grant.ExpiresAt, revision, hash, now.UTC(), purpose.String()); err != nil {
			return fmt.Errorf("insert allocation access grant: %w", err)
		}
		return nil
	})
	return grant, err
}
