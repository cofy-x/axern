package pgadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type lockedLifecycleRetry struct {
	Item             allocationkernel.LifecycleRetryItem
	AllocationStatus string
}

func lockLifecycleRetry(ctx context.Context, tx pgx.Tx, allocationID string, reason string, now time.Time) (*lockedLifecycleRetry, error) {
	var out lockedLifecycleRetry
	clearanceInput := allocationkernel.LifecycleRetryClearanceInput{}
	err := tx.QueryRow(ctx, `
		SELECT q.allocation_id, a.owner_id, a.owner_type, a.environment_id, q.reason, a.node_id, n.node_target, a.attempt,
			q.reconcile_attempts, q.last_error, q.next_run_at, q.created_at, q.updated_at, a.status,
			EXISTS (
				SELECT 1 FROM workload_reservations wr
				WHERE wr.allocation_id = q.allocation_id AND wr.released_at IS NULL
			),
			EXISTS (
				SELECT 1 FROM execution_leases el
				WHERE el.allocation_id = q.allocation_id AND el.revoked = FALSE AND el.expires_at > $3
			),
			EXISTS (
				SELECT 1 FROM tunnel_sessions ts
				WHERE ts.allocation_id = q.allocation_id
				  AND ts.revoked = FALSE
				  AND ts.status IN ($4, $5, $6)
			),
			COALESCE((
				SELECT r.status FROM runs r
				WHERE r.allocation_id = q.allocation_id
				LIMIT 1
			), ''),
			EXISTS (
				SELECT 1
				FROM services s, jsonb_array_elements_text(s.allocation_ids) AS existing(allocation_id)
				WHERE s.service_id = a.owner_id AND existing.allocation_id = q.allocation_id
			)
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		JOIN nodes n ON n.node_id = a.node_id
		WHERE q.allocation_id = $1 AND q.reason = $2
		FOR UPDATE OF q, a
	`, strings.TrimSpace(allocationID), strings.TrimSpace(reason), now.UTC(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String()).Scan(
		&out.Item.AllocationID,
		&out.Item.OwnerID,
		&out.Item.OwnerType,
		&out.Item.EnvironmentID,
		&out.Item.Reason,
		&out.Item.NodeID,
		&out.Item.NodeTarget,
		&out.Item.Attempt,
		&out.Item.ReconcileAttempts,
		&out.Item.LastReconcileError,
		&out.Item.NextRunAt,
		&out.Item.CreatedAt,
		&out.Item.UpdatedAt,
		&out.AllocationStatus,
		&clearanceInput.HasActiveReservation,
		&clearanceInput.HasActiveLease,
		&clearanceInput.HasActiveTunnelSession,
		&clearanceInput.OwnerRunStatus,
		&clearanceInput.OwnerServiceReferencesAllocation,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q with reason %q not found", allocationID, reason)
	}
	if err != nil {
		return nil, fmt.Errorf("lock allocation lifecycle retry: %w", err)
	}
	if strings.TrimSpace(out.Item.AllocationID) == "" {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q not found", allocationID)
	}
	now = now.UTC()
	out.Item.AgeSeconds = int64(now.Sub(out.Item.CreatedAt).Seconds())
	out.Item.Due = !out.Item.NextRunAt.After(now)
	clearanceInput.AllocationID = out.Item.AllocationID
	clearanceInput.AllocationStatus = out.AllocationStatus
	clearanceInput.OwnerType = out.Item.OwnerType
	clearance := allocationkernel.EvaluateLifecycleRetryClearance(clearanceInput)
	out.Item.Clearable = clearance.Clearable
	out.Item.ClearBlockedReason = clearance.BlockedReason
	return &out, nil
}

type adminAuditEvent struct {
	EventID          string
	Operation        string
	TargetType       string
	TargetID         string
	OperatorReason   string
	ActorPrincipalID string
	CreatedAt        time.Time
}

func loadLifecycleRetry(ctx context.Context, tx pgx.Tx, allocationID string, reason string, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	item, ok, err := pgallocation.LoadLifecycleRetry(ctx, tx, allocationID, reason, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q not found", allocationID)
	}
	return item, nil
}

func failLifecycleRetryOwner(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem, message string, now time.Time) error {
	switch item.OwnerType {
	case allocationkernel.OwnerRun:
		return failRunLifecycleRetry(ctx, tx, item, message, now)
	case allocationkernel.OwnerService:
		return failServiceLifecycleRetry(ctx, tx, item, message, now)
	default:
		return grpcstatus.Errorf(codes.FailedPrecondition, "unsupported allocation lifecycle retry owner_type %q", item.OwnerType)
	}
}

func failRunLifecycleRetry(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem, message string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE allocations
		SET status = $2, message = $3, version = version + 1, updated_at = $4
		WHERE allocation_id = $1 AND owner_type = $5 AND owner_id = $6
	`, item.AllocationID, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), strings.TrimSpace(message), now.UTC(), allocationkernel.OwnerRun, item.OwnerID); err != nil {
		return fmt.Errorf("fail run allocation: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE runs
		SET status = $2, message = $3, version = version + 1, updated_at = $4
		WHERE allocation_id = $1 AND status NOT IN ($5, $6, $7)
	`, item.AllocationID, runv1.RunStatus_RUN_STATUS_FAILED.String(), strings.TrimSpace(message), now.UTC(), runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(), runv1.RunStatus_RUN_STATUS_FAILED.String(), runv1.RunStatus_RUN_STATUS_CANCELLED.String())
	if err != nil {
		return fmt.Errorf("fail run lifecycle retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "run allocation lifecycle retry %q cannot be failed because the run is already terminal", item.AllocationID)
	}
	if err := revokeActiveAllocationLeases(ctx, tx, item.AllocationID); err != nil {
		return err
	}
	if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
		AllocationIDs: []string{item.AllocationID},
		Reason:        "admin failed run allocation lifecycle retry",
		ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
		Now:           now,
	}); err != nil {
		return err
	}
	return pgreservation.ReleaseAllocation(ctx, tx, item.AllocationID, now)
}

func failServiceLifecycleRetry(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem, message string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE allocations
		SET status = $2, message = $3, version = version + 1, updated_at = $4
		WHERE allocation_id = $1 AND owner_type = $5 AND owner_id = $6
	`, item.AllocationID, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), strings.TrimSpace(message), now.UTC(), allocationkernel.OwnerService, item.OwnerID); err != nil {
		return fmt.Errorf("fail service allocation: %w", err)
	}
	if err := pgtunnel.RevokeActiveForAllocationsTx(ctx, tx, pgtunnel.RevokeActiveForAllocationsRequest{
		AllocationIDs: []string{item.AllocationID},
		Reason:        "admin failed service allocation lifecycle retry",
		ReasonCode:    tunnelv1.TunnelSessionEventReasonCode_TUNNEL_SESSION_EVENT_REASON_CODE_ALLOCATION_ENDED,
		Now:           now,
	}); err != nil {
		return err
	}
	if err := revokeActiveAllocationLeases(ctx, tx, item.AllocationID); err != nil {
		return err
	}
	if err := pgreservation.ReleaseAllocation(ctx, tx, item.AllocationID, now); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE services
		SET allocation_ids = COALESCE((
				SELECT jsonb_agg(allocation_id ORDER BY ord)
				FROM jsonb_array_elements_text(services.allocation_ids) WITH ORDINALITY AS existing(allocation_id, ord)
				WHERE existing.allocation_id <> $2
			), '[]'::jsonb),
			status = $3,
			message = $4,
			version = version + 1,
			updated_at = $5
		WHERE service_id = $1
	`, item.OwnerID, item.AllocationID, servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED.String(), strings.TrimSpace(message), now.UTC())
	if err != nil {
		return fmt.Errorf("fail service lifecycle retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "service allocation lifecycle retry %q has no owner service", item.AllocationID)
	}
	return nil
}

func revokeActiveAllocationLeases(ctx context.Context, tx pgx.Tx, allocationID string) error {
	rows, err := tx.Query(ctx, `
		SELECT lease_id
		FROM execution_leases
		WHERE allocation_id = $1 AND revoked = FALSE
		FOR UPDATE
	`, strings.TrimSpace(allocationID))
	if err != nil {
		return fmt.Errorf("query active allocation leases: %w", err)
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
		revision, err := nextLeaseRevision(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE execution_leases
			SET revoked = TRUE, revision = $2
			WHERE lease_id = $1
		`, leaseID, revision); err != nil {
			return fmt.Errorf("revoke allocation lease %s: %w", leaseID, err)
		}
	}
	return nil
}

func nextLeaseRevision(ctx context.Context, tx pgx.Tx) (int64, error) {
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

func deleteLifecycleRetry(ctx context.Context, tx pgx.Tx, allocationID string, reason string) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM allocation_reconcile_queue
		WHERE allocation_id = $1 AND reason = $2
	`, strings.TrimSpace(allocationID), strings.TrimSpace(reason))
	if err != nil {
		return fmt.Errorf("delete allocation lifecycle retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q with reason %q not found", allocationID, reason)
	}
	return nil
}

func requireNoActiveAllocationCleanupState(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) error {
	var activeReservations int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM workload_reservations
		WHERE allocation_id = $1 AND released_at IS NULL
	`, strings.TrimSpace(allocationID)).Scan(&activeReservations); err != nil {
		return fmt.Errorf("count active allocation reservations: %w", err)
	}
	if activeReservations > 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q has active reservations", allocationID)
	}
	var activeLeases int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM execution_leases
		WHERE allocation_id = $1 AND revoked = FALSE AND expires_at > $2
	`, strings.TrimSpace(allocationID), now.UTC()).Scan(&activeLeases); err != nil {
		return fmt.Errorf("count active allocation leases: %w", err)
	}
	if activeLeases > 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q has active leases", allocationID)
	}
	var activeTunnels int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM tunnel_sessions
		WHERE allocation_id = $1
		  AND revoked = FALSE
		  AND status IN ($2, $3, $4)
	`, strings.TrimSpace(allocationID),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String()).Scan(&activeTunnels); err != nil {
		return fmt.Errorf("count active allocation tunnels: %w", err)
	}
	if activeTunnels > 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q has active tunnel sessions", allocationID)
	}
	return nil
}

func requireOwnerConvergedForClear(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem) error {
	switch item.OwnerType {
	case allocationkernel.OwnerRun:
		return requireRunConvergedForClear(ctx, tx, item)
	case allocationkernel.OwnerService:
		return requireServiceConvergedForClear(ctx, tx, item)
	default:
		return grpcstatus.Errorf(codes.FailedPrecondition, "unsupported allocation lifecycle retry owner_type %q", item.OwnerType)
	}
}

func requireRunConvergedForClear(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status
		FROM runs
		WHERE allocation_id = $1
		FOR UPDATE
	`, strings.TrimSpace(item.AllocationID)).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock run owner for lifecycle retry clear: %w", err)
	}
	switch status {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(),
		runv1.RunStatus_RUN_STATUS_FAILED.String(),
		runv1.RunStatus_RUN_STATUS_CANCELLED.String():
		return nil
	default:
		return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q cannot be cleared while owner run status is %s", item.AllocationID, status)
	}
}

func requireServiceConvergedForClear(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem) error {
	var stillReferenced bool
	err := tx.QueryRow(ctx, `
		WITH locked_service AS (
			SELECT allocation_ids
			FROM services
			WHERE service_id = $1
			FOR UPDATE
		)
		SELECT EXISTS (
			SELECT 1
			FROM locked_service s, jsonb_array_elements_text(s.allocation_ids) AS existing(allocation_id)
			WHERE existing.allocation_id = $2
		)
	`, strings.TrimSpace(item.OwnerID), strings.TrimSpace(item.AllocationID)).Scan(&stillReferenced)
	if err != nil {
		return fmt.Errorf("check service owner for lifecycle retry clear: %w", err)
	}
	if stillReferenced {
		return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q cannot be cleared while owner service still references it", item.AllocationID)
	}
	return nil
}
