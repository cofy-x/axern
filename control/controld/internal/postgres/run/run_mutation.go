package pgrun

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) MarkAllocationCreateFailed(ctx context.Context, allocationID string, message string, now time.Time) (*runv1.Run, error) {
	var run *runv1.Run
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE allocations
			SET status = $2, message = $3, version = version + 1, updated_at = $4
			WHERE allocation_id = $1 AND status NOT IN ($5, $6, $7)
		`, allocationID, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), message, now.UTC(), commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String()); err != nil {
			return fmt.Errorf("mark allocation failed: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE runs
			SET status = $2, message = $3, version = version + 1, updated_at = $4
			WHERE allocation_id = $1 AND status NOT IN ($5, $6, $7)
		`, allocationID, runv1.RunStatus_RUN_STATUS_FAILED.String(), message, now.UTC(), runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(), runv1.RunStatus_RUN_STATUS_FAILED.String(), runv1.RunStatus_RUN_STATUS_CANCELLED.String()); err != nil {
			return fmt.Errorf("mark run failed: %w", err)
		}
		if err := pgreservation.ReleaseAllocation(ctx, tx, allocationID, now); err != nil {
			return err
		}
		var err error
		run, err = s.runByAllocation(ctx, tx, allocationID)
		return err
	})
	return run, err
}

func (s *Store) CancelRun(ctx context.Context, runID string, now time.Time) (*runv1.Run, *runkernel.AllocationRecord, error) {
	var (
		run   *runv1.Run
		alloc *runkernel.AllocationRecord
	)
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var err error
		run, err = scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE run_id = $1 FOR UPDATE`, strings.TrimSpace(runID)))
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
				SET status = $2, version = version + 1, updated_at = $3
				WHERE allocation_id = $1 AND attempt = $4
			`, run.GetAllocationID(), commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING.String(), now.UTC(), run.GetAttempt()); err != nil {
				return fmt.Errorf("mark allocation releasing: %w", err)
			}
			if err := s.revokeAllocationLeases(ctx, tx, run.GetAllocationID(), now); err != nil {
				return err
			}
		}
		alloc, err = s.currentAllocation(ctx, tx, run.GetAllocationID())
		if err != nil {
			return err
		}
		run, err = scanRun(tx.QueryRow(ctx, runSelectSQL()+` WHERE run_id = $1`, strings.TrimSpace(runID)))
		return err
	})
	return run, alloc, err
}
