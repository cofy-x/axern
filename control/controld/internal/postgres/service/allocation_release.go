package pgservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) CompleteAllocationRelease(ctx context.Context, allocationID string, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		return s.completeAllocationReleaseTx(ctx, tx, allocationID, now)
	})
}

func (s *PGStore) CompleteClaimedAllocationRelease(ctx context.Context, allocationID, owner string, now time.Time) (bool, error) {
	completed := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var lockedAllocationID string
		err := tx.QueryRow(ctx, `
			SELECT allocation_id FROM allocations
			WHERE allocation_id = $1 AND owner_type = $2
			FOR UPDATE
		`, strings.TrimSpace(allocationID), allocationOwnerService).Scan(&lockedAllocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock service allocation release: %w", err)
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM allocation_reconcile_queue
			WHERE allocation_id = $1 AND reason = $2 AND lease_owner = $3
		`, strings.TrimSpace(allocationID), allocationkernel.ReconcileReasonDelete, strings.TrimSpace(owner))
		if err != nil {
			return fmt.Errorf("claim service allocation release: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return nil
		}
		completed = true
		return s.completeAllocationReleaseTx(ctx, tx, allocationID, now)
	})
	return completed, err
}

func (s *PGStore) completeAllocationReleaseTx(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET status = $2, version = version + 1, updated_at = $3
			WHERE allocation_id = $1 AND owner_type = $4
		`, strings.TrimSpace(allocationID), commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(), now.UTC(), allocationOwnerService); err != nil {
		return fmt.Errorf("complete service allocation release: %w", err)
	}
	if err := s.revokeAllocationLeases(ctx, tx, []string{allocationID}); err != nil {
		return err
	}
	if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
		AllocationIDs: []string{strings.TrimSpace(allocationID)},
		Reason:        "service allocation released",
		ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
		Now:           now,
	}); err != nil {
		return err
	}
	if err := pgreservation.ReleaseAllocation(ctx, tx, allocationID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id = $1`, strings.TrimSpace(allocationID)); err != nil {
		return fmt.Errorf("delete service allocation reconcile item: %w", err)
	}
	return nil
}
