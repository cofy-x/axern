package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) DueReconcileItems(ctx context.Context, limit int, now time.Time) ([]allocationkernel.ReconcileItem, error) {
	return pgallocation.DueReconcileItems(ctx, s.db.Pool(), allocationOwnerService, limit, now)
}

func (s *PGStore) ClaimDueReconcileItems(ctx context.Context, owner string, limit int, now time.Time, leaseTTL time.Duration) ([]allocationkernel.ReconcileItem, error) {
	return pgallocation.ClaimDueReconcileItems(ctx, s.db.Pool(), allocationOwnerService, owner, limit, now, leaseTTL)
}

func (s *PGStore) RenewReconcileClaim(ctx context.Context, allocationID, owner string, now time.Time, leaseTTL time.Duration) (bool, error) {
	return pgallocation.RenewReconcileClaim(ctx, s.db.Pool(), allocationID, owner, now, leaseTTL)
}

func (s *PGStore) ScheduleReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, now time.Time) error {
	return pgallocation.ScheduleReconcile(ctx, s.db.Pool(), req, now)
}

func (s *PGStore) CompleteAllocationCreate(ctx context.Context, allocationID string, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			DELETE FROM allocation_reconcile_queue
			WHERE allocation_id = $1 AND reason = $2
		`, strings.TrimSpace(allocationID), allocationkernel.ReconcileReasonCreate); err != nil {
			return fmt.Errorf("delete service allocation create reconcile item: %w", err)
		}
		_ = now
		return nil
	})
}

func (s *PGStore) ScheduleClaimedReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, owner string, now time.Time) (bool, error) {
	return pgallocation.ScheduleClaimedReconcile(ctx, s.db.Pool(), req, owner, now)
}

func (s *PGStore) CompleteClaimedAllocationCreate(ctx context.Context, allocationID, owner string, now time.Time) (bool, error) {
	tag, err := s.db.Pool().Exec(ctx, `
		DELETE FROM allocation_reconcile_queue
		WHERE allocation_id = $1 AND reason = $2 AND lease_owner = $3
	`, strings.TrimSpace(allocationID), allocationkernel.ReconcileReasonCreate, strings.TrimSpace(owner))
	if err != nil {
		return false, fmt.Errorf("complete claimed service allocation create: %w", err)
	}
	_ = now
	return tag.RowsAffected() == 1, nil
}
