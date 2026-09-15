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
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) MarkAllocationCreateFailed(ctx context.Context, allocationID, claimOwner string, message string, now time.Time) (*runv1.Run, error) {
	var run *runv1.Run
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT TRUE
			FROM allocations a
			JOIN runs r ON r.run_id = a.run_id
			WHERE a.allocation_id = $1
			FOR UPDATE OF a, r
		`, strings.TrimSpace(allocationID)).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		} else if err != nil {
			return fmt.Errorf("lock allocation after create failure: %w", err)
		}
		if err := pgallocation.RequireReconcileClaim(ctx, tx, allocationID, claimOwner, allocationkernel.ReconcileIntentEnsurePresent, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET lifecycle_state = $2, updated_at = $3
			WHERE allocation_id = $1 AND lifecycle_state NOT IN ($2, $4)
		`, allocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(), now.UTC(), commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()); err != nil {
			return fmt.Errorf("mark allocation releasing after create failure: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE runs
			SET status = $2, diagnostic_code = $3, message = $4, version = version + 1, updated_at = $5
			WHERE run_id = (SELECT run_id FROM allocations WHERE allocation_id = $1)
			  AND status NOT IN ($6, $7, $8)
		`, allocationID, runv1.RunStatus_RUN_STATUS_FAILED.String(), commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR.String(), message, now.UTC(), runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(), runv1.RunStatus_RUN_STATUS_FAILED.String(), runv1.RunStatus_RUN_STATUS_CANCELLED.String()); err != nil {
			return fmt.Errorf("mark run failed: %w", err)
		}
		if err := deleteAllocationRunSecretReferences(ctx, tx, allocationID); err != nil {
			return err
		}
		updated, err := pgallocation.ScheduleClaimedReconcile(ctx, tx, allocationkernel.ScheduleReconcileRequest{
			AllocationID: allocationID,
			Intent:       allocationkernel.ReconcileIntentEnsureAbsent,
			NextRunAt:    now,
		}, claimOwner, now)
		if err != nil {
			return err
		}
		if !updated {
			return allocationkernel.ErrReconcileClaimLost
		}
		run, err = s.runByAllocation(ctx, tx, allocationID)
		return err
	})
	if err == nil {
		s.signalReconcileWork()
	}
	return run, err
}

func (s *Store) CancelRun(ctx context.Context, runID string, now time.Time) (*runv1.Run, error) {
	var run *runv1.Run
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		run, err = scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE r.run_id = $1 FOR UPDATE OF r, a`, strings.TrimSpace(runID)))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return grpcstatus.Errorf(codes.NotFound, "run %q not found", runID)
			}
			return err
		}
		if !runkernel.IsTerminal(run.GetStatus()) {
			if _, err := tx.Exec(ctx, `
				UPDATE runs
				SET status = $2, version = version + 1, updated_at = $3
				WHERE run_id = $1
			`, run.GetID(), runv1.RunStatus_RUN_STATUS_CANCELLED.String(), now.UTC()); err != nil {
				return fmt.Errorf("cancel run: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				UPDATE allocations
				SET lifecycle_state = $2, updated_at = $3
				WHERE allocation_id = $1
			`, run.GetAllocationID(), commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(), now.UTC()); err != nil {
				return fmt.Errorf("mark allocation releasing: %w", err)
			}
			if err := pgallocation.RevokeAccessGrants(ctx, tx, run.GetAllocationID()); err != nil {
				return err
			}
			if err := deleteRunSecretReferences(ctx, tx, run.GetID()); err != nil {
				return err
			}
			if err := pgallocation.ScheduleReconcile(ctx, tx, allocationkernel.ScheduleDeleteRequest(run.GetAllocationID(), now), now); err != nil {
				return err
			}
		}
		run, err = scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE r.run_id = $1`, strings.TrimSpace(runID)))
		return err
	})
	if err == nil {
		s.signalReconcileWork()
	}
	return run, err
}
