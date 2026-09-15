package pgadmin

import (
	"context"
	"fmt"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Store struct {
	db *postgres.DB
}

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
		if _, err := lockLifecycleRetry(ctx, tx, req.AllocationID, now); err != nil {
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
			SET next_run_at = $2, updated_at = $3,
				claim_owner = '', claim_expires_at = NULL
			WHERE allocation_id = $1
		`, req.AllocationID, runAt.UTC(), now.UTC()); err != nil {
			return fmt.Errorf("force allocation lifecycle retry: %w", err)
		}
		item, err := loadLifecycleRetry(ctx, tx, req.AllocationID, now)
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
		locked, err := lockLifecycleRetry(ctx, tx, req.AllocationID, now)
		if err != nil {
			return err
		}
		if allocationkernel.ReconcileIntentForLifecycle(allocationkernel.ParseLifecycleState(locked.AllocationState)) != allocationkernel.ReconcileIntentEnsurePresent {
			return grpcstatus.Errorf(codes.FailedPrecondition, "allocation delete retries cannot be abandoned before cleanup succeeds")
		}
		if allocationkernel.IsCleanupState(allocationkernel.ParseLifecycleState(locked.AllocationState)) {
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
		if err := failRunLifecycleRetry(ctx, tx, locked.Item, req.OperatorReason, now); err != nil {
			return err
		}
		item, err := loadLifecycleRetry(ctx, tx, req.AllocationID, now)
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

func (s *Store) ClearAllocationLifecycleRetry(ctx context.Context, req allocationkernel.ClearLifecycleRetryRequest, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	req = allocationkernel.NormalizeClearLifecycleRetryRequest(req)
	if err := allocationkernel.ValidateClearLifecycleRetryRequest(req); err != nil {
		return nil, err
	}
	var out *allocationkernel.LifecycleRetryItem
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		locked, err := lockLifecycleRetry(ctx, tx, req.AllocationID, now)
		if err != nil {
			return err
		}
		if allocationkernel.ParseLifecycleState(locked.AllocationState) != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED {
			return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q cannot be cleared while allocation lifecycle state is %s", req.AllocationID, locked.AllocationState)
		}
		if err := requireNoActiveAllocationCleanupState(ctx, tx, req.AllocationID, now); err != nil {
			return err
		}
		if err := requireRunConvergedForClear(ctx, tx, locked.Item); err != nil {
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
		if err := deleteLifecycleRetry(ctx, tx, req.AllocationID); err != nil {
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
