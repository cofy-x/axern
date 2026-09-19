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
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) CompleteAllocationRelease(ctx context.Context, allocationID, claimOwner string, snapshot *allocationkernel.RootfsSnapshotResult, now time.Time) error {
	allocationID = strings.TrimSpace(allocationID)
	return s.withTx(ctx, func(tx pgx.Tx) error {
		var stateText, runID, namespace, runStatus string
		var configJSON, resolvedEnvironmentJSON, currentSnapshotJSON []byte
		if err := tx.QueryRow(ctx, `
			SELECT a.lifecycle_state, r.run_id, r.namespace, r.status, r.config, r.resolved_environment_spec, r.rootfs_snapshot_result
			FROM allocations a JOIN runs r ON r.run_id = a.run_id
			WHERE a.allocation_id = $1 FOR UPDATE OF a, r
		`, allocationID).Scan(&stateText, &runID, &namespace, &runStatus, &configJSON, &resolvedEnvironmentJSON, &currentSnapshotJSON); errors.Is(err, pgx.ErrNoRows) {
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
		config := &commonv1.ExecutionConfig{}
		if err := protojson.Unmarshal(configJSON, config); err != nil {
			return fmt.Errorf("decode released run config: %w", err)
		}
		keepForAcknowledgement := false
		currentSnapshot := &runv1.RootfsSnapshotResult{}
		if err := protojson.Unmarshal(currentSnapshotJSON, currentSnapshot); err != nil {
			return fmt.Errorf("decode current rootfs snapshot result: %w", err)
		}
		if snapshot != nil {
			if config.GetRootfsSnapshot() == nil || runStatus != runv1.RunStatus_RUN_STATUS_SUCCEEDED.String() {
				return grpcstatus.Error(codes.FailedPrecondition, "rootfs snapshot result does not match a successful snapshot request")
			}
			if currentSnapshot.GetStatus() != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING {
				return grpcstatus.Error(codes.FailedPrecondition, "rootfs snapshot result is already final")
			}
			if strings.TrimSpace(snapshot.ImageRef) == "" || snapshot.ImageDescriptor == nil {
				return grpcstatus.Error(codes.FailedPrecondition, "rootfs snapshot result is incomplete")
			}
			environmentID := "env-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(allocationID)).String()
			spec := &environmentv1.EnvironmentSpec{Namespace: namespace, Image: &environmentv1.EnvironmentImageSource{Ref: snapshot.ImageRef}}
			resolved := &environmentv1.ResolvedEnvironmentSpec{}
			if err := protojson.Unmarshal(resolvedEnvironmentJSON, resolved); err != nil {
				return fmt.Errorf("decode snapshot environment: %w", err)
			}
			resolved.RootfsReadonly = false
			resolved.ImageDescriptor = snapshot.ImageDescriptor
			specJSON, err := marshalProtoJSON(spec)
			if err != nil {
				return err
			}
			resolvedJSON, err := marshalProtoJSON(resolved)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO environments (environment_id, namespace, spec, resolved_spec, labels, created_at)
				VALUES ($1,$2,$3::jsonb,$4::jsonb,'{}'::jsonb,$5)`, environmentID, namespace, specJSON, resolvedJSON, now.UTC()); err != nil {
				return fmt.Errorf("publish snapshot environment: %w", err)
			}
			result := &runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY, EnvironmentID: environmentID,
				ImageRef: snapshot.ImageRef, ImageDescriptor: snapshot.ImageDescriptor, PlatformOS: snapshot.PlatformOS, PlatformArch: snapshot.PlatformArch, PlatformVariant: snapshot.PlatformVariant}
			resultJSON, err := marshalProtoJSON(result)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE runs SET rootfs_snapshot_result=$2::jsonb, version=version+1, updated_at=$3 WHERE run_id=$1`, runID, resultJSON, now.UTC()); err != nil {
				return fmt.Errorf("publish run rootfs snapshot result: %w", err)
			}
			keepForAcknowledgement = true
		} else if config.GetRootfsSnapshot() != nil && runStatus == runv1.RunStatus_RUN_STATUS_SUCCEEDED.String() && currentSnapshot.GetStatus() == runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING {
			return grpcstatus.Error(codes.FailedPrecondition, "successful rootfs snapshot request has no sealing result")
		} else if config.GetRootfsSnapshot() != nil && runStatus != runv1.RunStatus_RUN_STATUS_SUCCEEDED.String() {
			resultJSON, err := marshalProtoJSON(&runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED, Message: "workload did not succeed"})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE runs SET rootfs_snapshot_result=$2::jsonb, version=version+1, updated_at=$3 WHERE run_id=$1`, runID, resultJSON, now.UTC()); err != nil {
				return err
			}
		}
		// Termination already revoked execution access. Output-only grants issued
		// since then remain valid until their bounded retention deadline.
		if keepForAcknowledgement {
			tag, err := tx.Exec(ctx, `UPDATE allocation_reconcile_queue SET claim_owner='', claim_expires_at=NULL, next_run_at=$3, updated_at=$3 WHERE allocation_id=$1 AND claim_owner=$2`, allocationID, strings.TrimSpace(claimOwner), now.UTC())
			if err != nil {
				return fmt.Errorf("schedule snapshot release acknowledgement: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return allocationkernel.ErrReconcileClaimLost
			}
			return nil
		}
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

func (s *Store) CompleteAllocationReleaseAcknowledgement(ctx context.Context, allocationID, claimOwner string, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if err := pgallocation.RequireReconcileClaim(ctx, tx, allocationID, claimOwner, allocationkernel.ReconcileIntentEnsureAbsent, now); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM allocation_reconcile_queue WHERE allocation_id=$1 AND claim_owner=$2`, strings.TrimSpace(allocationID), strings.TrimSpace(claimOwner))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return allocationkernel.ErrReconcileClaimLost
		}
		return nil
	})
}

func (s *Store) MarkRootfsSnapshotFailed(ctx context.Context, allocationID, claimOwner, message string, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if err := pgallocation.RequireReconcileClaim(ctx, tx, allocationID, claimOwner, allocationkernel.ReconcileIntentEnsureAbsent, now); err != nil {
			return err
		}
		resultJSON, err := marshalProtoJSON(&runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED, Message: strings.TrimSpace(message)})
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE runs r
			SET rootfs_snapshot_result=$2::jsonb, version=version+1, updated_at=$3
			FROM allocations a
			WHERE a.allocation_id=$1 AND r.run_id=a.run_id
			  AND r.rootfs_snapshot_result->>'status'='ROOTFS_SNAPSHOT_STATUS_PENDING'`, strings.TrimSpace(allocationID), resultJSON, now.UTC())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return grpcstatus.Error(codes.FailedPrecondition, "rootfs snapshot result is already final or missing")
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
