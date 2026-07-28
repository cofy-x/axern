package pgadmin

import (
	"context"
	"fmt"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Store struct {
	db *postgres.DB
}

const leaseRevisionName = "execution_leases"

func NewStore(db *postgres.DB) *Store {
	return &Store{db: db}
}

func (s *Store) ListAllocationLifecycleRetries(ctx context.Context, filter allocationkernel.LifecycleRetryFilter, now time.Time) ([]allocationkernel.LifecycleRetryItem, error) {
	return pgallocation.ListLifecycleRetries(ctx, s.db.Pool(), filter, now)
}

func (s *Store) ForceAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ForceLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeForceLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateForceLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	runAt := req.RequestedRunAt
	if runAt.IsZero() {
		runAt = now
	}
	var out *allocationkernel.LifecycleRetryItem
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := lockLifecycleRetry(ctx, tx, req.AllocationID, req.Reason, now); err != nil {
			return err
		}
		if err := insertAdminAuditEvent(ctx, tx, adminAuditEvent{
			EventID:        "admaudit-" + uuid.NewString(),
			Operation:      adminkernel.AuditOperationForceAllocationLifecycleRetry,
			TargetType:     adminkernel.AuditTargetAllocation,
			TargetID:       req.AllocationID,
			OperatorReason: req.OperatorReason,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE allocation_reconcile_queue
			SET next_run_at = $3, updated_at = $4
			WHERE allocation_id = $1 AND reason = $2
		`, req.AllocationID, req.Reason, runAt.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("force allocation lifecycle retry: %w", err)
		}
		item, err := loadLifecycleRetry(ctx, tx, req.AllocationID, req.Reason, now)
		if err != nil {
			return err
		}
		out = item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) FailAllocationLifecycleRetry(ctx context.Context, req allocationkernel.FailLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeFailLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateFailLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	var out *allocationkernel.LifecycleRetryItem
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		locked, err := lockLifecycleRetry(ctx, tx, req.AllocationID, req.Reason, now)
		if err != nil {
			return err
		}
		if allocationkernel.IsEnded(allocationkernel.ParseStatus(locked.AllocationStatus)) {
			return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q is already terminal; clear the retry instead", req.AllocationID)
		}
		if err := insertAdminAuditEvent(ctx, tx, adminAuditEvent{
			EventID:        "admaudit-" + uuid.NewString(),
			Operation:      adminkernel.AuditOperationFailAllocationLifecycleRetry,
			TargetType:     adminkernel.AuditTargetAllocation,
			TargetID:       req.AllocationID,
			OperatorReason: req.OperatorReason,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		if err := failLifecycleRetryOwner(ctx, tx, locked.Item, req.OperatorReason, now); err != nil {
			return err
		}
		if err := deleteLifecycleRetry(ctx, tx, req.AllocationID, req.Reason); err != nil {
			return err
		}
		out = &locked.Item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ClearAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ClearLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeClearLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateClearLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	var out *allocationkernel.LifecycleRetryItem
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		locked, err := lockLifecycleRetry(ctx, tx, req.AllocationID, req.Reason, now)
		if err != nil {
			return err
		}
		if !allocationkernel.IsEnded(allocationkernel.ParseStatus(locked.AllocationStatus)) {
			return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q cannot be cleared while allocation status is %s", req.AllocationID, locked.AllocationStatus)
		}
		if err := requireNoActiveAllocationCleanupState(ctx, tx, req.AllocationID, now); err != nil {
			return err
		}
		if err := requireOwnerConvergedForClear(ctx, tx, locked.Item); err != nil {
			return err
		}
		if err := insertAdminAuditEvent(ctx, tx, adminAuditEvent{
			EventID:        "admaudit-" + uuid.NewString(),
			Operation:      adminkernel.AuditOperationClearAllocationLifecycleRetry,
			TargetType:     adminkernel.AuditTargetAllocation,
			TargetID:       req.AllocationID,
			OperatorReason: req.OperatorReason,
			CreatedAt:      now,
		}); err != nil {
			return err
		}
		if err := deleteLifecycleRetry(ctx, tx, req.AllocationID, req.Reason); err != nil {
			return err
		}
		out = &locked.Item
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
