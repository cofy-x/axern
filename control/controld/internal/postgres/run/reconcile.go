package pgrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) CompleteAllocationRelease(ctx context.Context, allocationID string, now time.Time) error {
	allocationID = strings.TrimSpace(allocationID)
	return s.withTx(ctx, func(tx pgx.Tx) error {
		var stateText string
		if err := tx.QueryRow(ctx, `
			SELECT lifecycle_state FROM allocations WHERE allocation_id = $1 FOR UPDATE
		`, allocationID).Scan(&stateText); errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		} else if err != nil {
			return fmt.Errorf("lock allocation release: %w", err)
		}
		state := allocationkernel.ParseLifecycleState(stateText)
		if state != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING &&
			state != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED {
			return grpcstatus.Errorf(codes.FailedPrecondition, "allocation %q cannot be released from lifecycle state %s", allocationID, stateText)
		}
		if state == commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING {
			if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET lifecycle_state = $2, updated_at = $3
			WHERE allocation_id = $1
			`, allocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(), now.UTC()); err != nil {
				return fmt.Errorf("complete allocation release: %w", err)
			}
		}
		if err := s.revokeAllocationLeases(ctx, tx, allocationID, now); err != nil {
			return err
		}
		if err := pgreservation.ReleaseAllocation(ctx, tx, allocationID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id = $1`, allocationID); err != nil {
			return fmt.Errorf("delete reconcile item: %w", err)
		}
		return nil
	})
}

func (s *Store) CompleteAllocationStart(ctx context.Context, allocationID string, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			DELETE FROM allocation_reconcile_queue
			WHERE allocation_id = $1 AND reason = $2
		`, strings.TrimSpace(allocationID), allocationkernel.ReconcileReasonCreate); err != nil {
			return fmt.Errorf("delete start reconcile item: %w", err)
		}
		_ = now
		return nil
	})
}

func (s *Store) LoadStartAllocation(ctx context.Context, allocationID string) (*runkernel.StartAllocation, error) {
	var out *runkernel.StartAllocation
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		run, err := s.runByAllocation(ctx, tx, allocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "run allocation %q not found", allocationID)
		}
		if err != nil {
			return err
		}
		env, err := scanEnvironment(tx.QueryRow(ctx, environmentSelectSQL()+` WHERE environment_id = $1`, run.GetEnvironmentID()))
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "environment %q not found", run.GetEnvironmentID())
		}
		if err != nil {
			return err
		}
		alloc, err := s.currentAllocation(ctx, tx, allocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		}
		if err != nil {
			return err
		}
		out = &runkernel.StartAllocation{
			Run:         run,
			Environment: env,
			Allocation:  alloc,
		}
		return nil
	})
	return out, err
}

func (s *Store) nextLeaseRevision(ctx context.Context, tx pgx.Tx) (int64, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE control_revisions
		SET revision = revision + 1
		WHERE name = $1
		RETURNING revision
	`, leaseRevisionName).Scan(&revision); err != nil {
		return 0, fmt.Errorf("next lease revision: %w", err)
	}
	return revision, nil
}

func (s *Store) revokeAllocationLeases(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) error {
	rows, err := tx.Query(ctx, `
		SELECT lease_id
		FROM execution_leases
		WHERE allocation_id = $1 AND revoked = false
		FOR UPDATE
	`, strings.TrimSpace(allocationID))
	if err != nil {
		return fmt.Errorf("query leases for revoke: %w", err)
	}
	defer rows.Close()
	leaseIDs := make([]string, 0)
	for rows.Next() {
		var leaseID string
		if err := rows.Scan(&leaseID); err != nil {
			return err
		}
		leaseIDs = append(leaseIDs, leaseID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, leaseID := range leaseIDs {
		revision, err := s.nextLeaseRevision(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE execution_leases
			SET revoked = true, revision = $2
			WHERE lease_id = $1
		`, leaseID, revision); err != nil {
			return fmt.Errorf("revoke lease %s: %w", leaseID, err)
		}
	}
	_ = now
	return nil
}

func (s *Store) DueReconcileItems(ctx context.Context, limit int, now time.Time) ([]allocationkernel.ReconcileItem, error) {
	return pgallocation.DueReconcileItems(ctx, s.db.Pool(), limit, now)
}

func (s *Store) ScheduleReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, now time.Time) error {
	return pgallocation.ScheduleReconcile(ctx, s.db.Pool(), req, now)
}

func (s *Store) RescheduleReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, now time.Time) (bool, error) {
	return pgallocation.RescheduleReconcile(ctx, s.db.Pool(), req, now)
}
