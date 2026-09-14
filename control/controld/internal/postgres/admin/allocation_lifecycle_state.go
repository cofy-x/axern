package pgadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	pgtunnel "github.com/cofy-x/axern/control/controld/internal/postgres/tunnel"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type lockedLifecycleRetry struct {
	Item            allocationkernel.LifecycleRetryItem
	AllocationState string
}

func lockLifecycleRetry(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) (*lockedLifecycleRetry, error) {
	var out lockedLifecycleRetry
	clearanceInput := allocationkernel.LifecycleRetryClearanceInput{}
	err := tx.QueryRow(ctx, `
		SELECT q.allocation_id, a.run_id, r.environment_id, a.lifecycle_state, a.node_id, n.node_target,
			q.reconcile_attempts, q.last_error, q.next_run_at, q.created_at, q.updated_at, a.lifecycle_state,
			EXISTS (
				SELECT 1 FROM reservations res
				WHERE res.allocation_id = q.allocation_id AND res.released_at IS NULL
			),
			EXISTS (
				SELECT 1 FROM execution_leases el
				WHERE el.allocation_id = q.allocation_id AND el.revoked = FALSE AND el.expires_at > $2
			),
			EXISTS (
				SELECT 1 FROM tunnel_sessions ts
				WHERE ts.allocation_id = q.allocation_id
				  AND ts.status IN ($3, $4, $5)
			),
			COALESCE((
				SELECT r.status FROM runs r
				WHERE r.run_id = a.run_id
				LIMIT 1
			), '')
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		JOIN runs r ON r.run_id = a.run_id
		JOIN nodes n ON n.node_id = a.node_id
		WHERE q.allocation_id = $1
		FOR UPDATE OF q, a
	`, strings.TrimSpace(allocationID), now.UTC(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String()).Scan(
		&out.Item.AllocationID,
		&out.Item.RunID,
		&out.Item.EnvironmentID,
		&out.AllocationState,
		&out.Item.NodeID,
		&out.Item.NodeTarget,
		&out.Item.ReconcileAttempts,
		&out.Item.LastReconcileError,
		&out.Item.NextRunAt,
		&out.Item.CreatedAt,
		&out.Item.UpdatedAt,
		&clearanceInput.AllocationState,
		&clearanceInput.HasActiveReservation,
		&clearanceInput.HasActiveLease,
		&clearanceInput.HasActiveTunnelSession,
		&clearanceInput.RunStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q not found", allocationID)
	}
	if err != nil {
		return nil, fmt.Errorf("lock allocation lifecycle retry: %w", err)
	}
	if strings.TrimSpace(out.Item.AllocationID) == "" {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q not found", allocationID)
	}
	now = now.UTC()
	out.Item.LifecycleState = allocationkernel.ParseLifecycleState(out.AllocationState).String()
	out.Item.AgeSeconds = int64(now.Sub(out.Item.CreatedAt).Seconds())
	out.Item.Due = !out.Item.NextRunAt.After(now)
	clearanceInput.AllocationID = out.Item.AllocationID
	clearanceInput.AllocationState = out.AllocationState
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

func loadLifecycleRetry(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) (*allocationkernel.LifecycleRetryItem, error) {
	item, ok, err := pgallocation.LoadLifecycleRetry(ctx, tx, allocationID, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q not found", allocationID)
	}
	return item, nil
}

func failRunLifecycleRetry(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem, message string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE allocations
		SET lifecycle_state = $2, updated_at = $3
		WHERE allocation_id = $1 AND run_id = $4
	`, item.AllocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(), now.UTC(), item.RunID); err != nil {
		return fmt.Errorf("fail run allocation: %w", err)
	}
	tag, err := tx.Exec(ctx, `
		UPDATE runs
		SET status = $2, diagnostic_code = $3, message = $4, version = version + 1, updated_at = $5
		WHERE run_id = $1 AND status NOT IN ($6, $7, $8)
	`, item.RunID, runv1.RunStatus_RUN_STATUS_FAILED.String(), commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR.String(), strings.TrimSpace(message), now.UTC(), runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(), runv1.RunStatus_RUN_STATUS_FAILED.String(), runv1.RunStatus_RUN_STATUS_CANCELLED.String())
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
	if _, err := tx.Exec(ctx, `
		UPDATE allocation_reconcile_queue
		SET reconcile_attempts = 0, next_run_at = $2,
			last_error = '', lease_owner = '', lease_expires_at = NULL, updated_at = $2
		WHERE allocation_id = $1
	`, item.AllocationID, now.UTC()); err != nil {
		return fmt.Errorf("schedule cleanup after failed allocation create: %w", err)
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

func deleteLifecycleRetry(ctx context.Context, tx pgx.Tx, allocationID string) error {
	tag, err := tx.Exec(ctx, `
		DELETE FROM allocation_reconcile_queue
		WHERE allocation_id = $1
	`, strings.TrimSpace(allocationID))
	if err != nil {
		return fmt.Errorf("delete allocation lifecycle retry: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return grpcstatus.Errorf(codes.NotFound, "allocation lifecycle retry %q not found", allocationID)
	}
	return nil
}

func requireNoActiveAllocationCleanupState(ctx context.Context, tx pgx.Tx, allocationID string, now time.Time) error {
	var activeReservations int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM reservations
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

func requireRunConvergedForClear(ctx context.Context, tx pgx.Tx, item allocationkernel.LifecycleRetryItem) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status
		FROM runs
		WHERE run_id = $1
		FOR UPDATE
	`, strings.TrimSpace(item.RunID)).Scan(&status)
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
		return grpcstatus.Errorf(codes.FailedPrecondition, "allocation lifecycle retry %q cannot be cleared while run status is %s", item.AllocationID, status)
	}
}
