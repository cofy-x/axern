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
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Store) CompleteAllocationRelease(ctx context.Context, allocationID, claimOwner string, now time.Time) error {
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
		if err := pgallocation.RequireReconcileClaim(ctx, tx, allocationID, claimOwner, allocationkernel.ReconcileIntentEnsureAbsent, now); err != nil {
			return err
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
		// Termination already revoked execution access. Output-only grants issued
		// since then remain valid until their bounded retention deadline.
		tag, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id = $1 AND claim_owner = $2`, allocationID, strings.TrimSpace(claimOwner))
		if err != nil {
			return fmt.Errorf("delete reconcile item: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return allocationkernel.ErrReconcileClaimLost
		}
		return nil
	})
}

func (s *Store) CompleteAllocationStart(ctx context.Context, allocationID, claimOwner string, conditions *capabilityv1.CapabilityConditionSet, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		// All lifecycle transactions lock the Allocation before its durable
		// queue intent. This matches report, cancellation, and release paths and
		// prevents a worker completion racing a terminal report from deadlocking.
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT TRUE FROM allocations WHERE allocation_id = $1 FOR UPDATE
		`, strings.TrimSpace(allocationID)).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		} else if err != nil {
			return fmt.Errorf("lock allocation start completion: %w", err)
		}
		if err := pgallocation.RequireReconcileClaim(ctx, tx, allocationID, claimOwner, allocationkernel.ReconcileIntentEnsurePresent, now); err != nil {
			return err
		}
		if conditions != nil {
			if err := pgallocation.ReplaceCapabilityConditions(ctx, tx, allocationID, conditions, now); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM allocation_reconcile_queue
			WHERE allocation_id = $1 AND claim_owner = $2
		`, strings.TrimSpace(allocationID), strings.TrimSpace(claimOwner))
		if err != nil {
			return fmt.Errorf("delete start reconcile item: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return allocationkernel.ErrReconcileClaimLost
		}
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
		env := &environmentv1.Environment{
			ID:           run.GetEnvironmentID(),
			Namespace:    run.GetNamespace(),
			Spec:         cloneEnvironmentSpec(run.GetEnvironmentSpec()),
			ResolvedSpec: cloneResolvedEnvironmentSpec(run.GetResolvedEnvironmentSpec()),
		}
		alloc, err := s.currentAllocation(ctx, tx, allocationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Errorf(codes.NotFound, "allocation %q not found", allocationID)
		}
		if err != nil {
			return err
		}
		var nodeActive bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM nodes WHERE node_id = $1 AND lifecycle_status = 'active')", alloc.NodeID).Scan(&nodeActive); err != nil {
			return err
		}
		if !nodeActive {
			return grpcstatus.Error(codes.FailedPrecondition, "Node identity is not active")
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

func (s *Store) ClaimDueReconcileItems(ctx context.Context, owner string, limit int, now time.Time, claimTTL time.Duration) ([]allocationkernel.ReconcileItem, error) {
	return pgallocation.ClaimDueReconcileItems(ctx, s.db.Pool(), owner, limit, now, claimTTL)
}

func (s *Store) RenewReconcileClaim(ctx context.Context, allocationID, owner string, now time.Time, claimTTL time.Duration) (bool, error) {
	return pgallocation.RenewReconcileClaim(ctx, s.db.Pool(), allocationID, owner, now, claimTTL)
}

func (s *Store) ScheduleClaimedReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, owner string, now time.Time) (bool, error) {
	updated, err := pgallocation.ScheduleClaimedReconcile(ctx, s.db.Pool(), req, owner, now)
	if err == nil && updated {
		s.signalReconcileWork()
	}
	return updated, err
}
