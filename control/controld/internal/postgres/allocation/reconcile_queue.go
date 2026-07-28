package pgallocation

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type reconcileQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type reconcileExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func DueReconcileItems(ctx context.Context, queryer reconcileQueryer, ownerType string, limit int, now time.Time) ([]allocationkernel.ReconcileItem, error) {
	if limit <= 0 {
		limit = allocationkernel.DefaultReconcileLimit
	}
	rows, err := queryer.Query(ctx, `
		SELECT q.allocation_id, a.owner_id, a.environment_id, q.reason, a.node_id, n.node_target, a.attempt, q.reconcile_attempts, q.last_error, q.next_run_at,
			GREATEST(q.next_run_at, q.updated_at, COALESCE(q.lease_expires_at, '-infinity'::timestamptz)) AS eligible_at
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		JOIN nodes n ON n.node_id = a.node_id
		WHERE q.next_run_at <= $1 AND a.owner_type = $3
		ORDER BY q.next_run_at ASC, q.allocation_id ASC
		LIMIT $2
	`, now.UTC(), limit, strings.TrimSpace(ownerType))
	if err != nil {
		return nil, fmt.Errorf("query reconcile queue: %w", err)
	}
	defer rows.Close()
	out := make([]allocationkernel.ReconcileItem, 0)
	for rows.Next() {
		var item allocationkernel.ReconcileItem
		if err := rows.Scan(&item.AllocationID, &item.OwnerID, &item.EnvironmentID, &item.Reason, &item.NodeID, &item.NodeTarget, &item.Attempt, &item.ReconcileAttempts, &item.LastReconcileError, &item.NextRunAt, &item.EligibleAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ClaimDueReconcileItems(ctx context.Context, queryer reconcileQueryer, ownerType, owner string, limit int, now time.Time, leaseTTL time.Duration) ([]allocationkernel.ReconcileItem, error) {
	ownerType = strings.TrimSpace(ownerType)
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("reconcile claim owner is required")
	}
	if limit <= 0 {
		limit = allocationkernel.DefaultReconcileLimit
	}
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("reconcile claim lease TTL must be positive")
	}
	leaseExpiresAt := now.Add(leaseTTL).UTC()
	rows, err := queryer.Query(ctx, `
		WITH ranked AS (
			SELECT q.allocation_id, a.owner_id, a.environment_id, q.reason, a.node_id, n.node_target,
				a.attempt, q.reconcile_attempts, q.last_error, q.next_run_at,
				GREATEST(q.next_run_at, q.updated_at, COALESCE(q.lease_expires_at, '-infinity'::timestamptz)) AS eligible_at,
				ROW_NUMBER() OVER (PARTITION BY a.node_id ORDER BY q.next_run_at ASC, q.allocation_id ASC) AS node_rank
			FROM allocation_reconcile_queue q
			JOIN allocations a ON a.allocation_id = q.allocation_id
			JOIN nodes n ON n.node_id = a.node_id
			WHERE q.next_run_at <= $1
			  AND a.owner_type = $3
			  AND (q.lease_expires_at IS NULL OR q.lease_expires_at <= $1)
		), candidates AS (
			SELECT r.allocation_id, r.owner_id, r.environment_id, r.reason, r.node_id, r.node_target,
				r.attempt, r.reconcile_attempts, r.last_error, r.next_run_at, r.eligible_at
			FROM ranked r
			JOIN allocation_reconcile_queue q ON q.allocation_id = r.allocation_id
			ORDER BY r.node_rank ASC, r.next_run_at ASC, r.allocation_id ASC
			LIMIT $2
			FOR UPDATE OF q SKIP LOCKED
		), claimed AS (
			UPDATE allocation_reconcile_queue q
			SET lease_owner = $4, lease_expires_at = $5, updated_at = $1
			FROM candidates c
			WHERE q.allocation_id = c.allocation_id
			  AND (q.lease_expires_at IS NULL OR q.lease_expires_at <= $1)
			RETURNING q.allocation_id
		)
		SELECT c.allocation_id, c.owner_id, c.environment_id, c.reason, c.node_id, c.node_target,
			c.attempt, c.reconcile_attempts, c.last_error, c.next_run_at, c.eligible_at
		FROM candidates c
		JOIN claimed USING (allocation_id)
		ORDER BY c.allocation_id ASC
	`, now.UTC(), limit, ownerType, owner, leaseExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("claim reconcile queue: %w", err)
	}
	defer rows.Close()
	out := make([]allocationkernel.ReconcileItem, 0)
	for rows.Next() {
		item := allocationkernel.ReconcileItem{ClaimOwner: owner}
		if err := rows.Scan(&item.AllocationID, &item.OwnerID, &item.EnvironmentID, &item.Reason, &item.NodeID, &item.NodeTarget, &item.Attempt, &item.ReconcileAttempts, &item.LastReconcileError, &item.NextRunAt, &item.EligibleAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func RenewReconcileClaim(ctx context.Context, executor reconcileExecutor, allocationID, owner string, now time.Time, leaseTTL time.Duration) (bool, error) {
	if leaseTTL <= 0 {
		return false, fmt.Errorf("reconcile claim lease TTL must be positive")
	}
	tag, err := executor.Exec(ctx, `
		UPDATE allocation_reconcile_queue
		SET lease_expires_at = $3, updated_at = $2
		WHERE allocation_id = $1 AND lease_owner = $4
	`, strings.TrimSpace(allocationID), now.UTC(), now.Add(leaseTTL).UTC(), strings.TrimSpace(owner))
	if err != nil {
		return false, fmt.Errorf("renew allocation reconcile claim: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func ScheduleReconcile(ctx context.Context, executor reconcileExecutor, req allocationkernel.ScheduleReconcileRequest, now time.Time) error {
	nextRunAt := req.NextRunAt
	if nextRunAt.IsZero() {
		nextRunAt = now
	}
	_, err := executor.Exec(ctx, `
		INSERT INTO allocation_reconcile_queue(allocation_id, reason, next_run_at, reconcile_attempts, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, CASE WHEN $4 THEN 1 ELSE 0 END, $5, $6, clock_timestamp())
		ON CONFLICT (allocation_id) DO UPDATE SET
			reason = EXCLUDED.reason,
			next_run_at = EXCLUDED.next_run_at,
			reconcile_attempts = CASE
				WHEN allocation_reconcile_queue.reason <> EXCLUDED.reason THEN EXCLUDED.reconcile_attempts
				WHEN $4 THEN allocation_reconcile_queue.reconcile_attempts + 1
				ELSE allocation_reconcile_queue.reconcile_attempts
			END,
			last_error = EXCLUDED.last_error,
			lease_owner = CASE
				WHEN allocation_reconcile_queue.reason <> EXCLUDED.reason THEN ''
				ELSE allocation_reconcile_queue.lease_owner
			END,
			lease_expires_at = CASE
				WHEN allocation_reconcile_queue.reason <> EXCLUDED.reason THEN NULL
				ELSE allocation_reconcile_queue.lease_expires_at
			END,
			updated_at = clock_timestamp()
	`, strings.TrimSpace(req.AllocationID), strings.TrimSpace(req.Reason), nextRunAt.UTC(), req.IncrementAttempts, strings.TrimSpace(req.LastReconcileError), now.UTC())
	if err != nil {
		return fmt.Errorf("schedule allocation reconcile: %w", err)
	}
	return nil
}

// RescheduleReconcile only updates an existing lifecycle item. A reconciler
// must not recreate work that an operator or a concurrent terminal transition
// already removed.
func RescheduleReconcile(ctx context.Context, executor reconcileExecutor, req allocationkernel.ScheduleReconcileRequest, now time.Time) (bool, error) {
	nextRunAt := req.NextRunAt
	if nextRunAt.IsZero() {
		nextRunAt = now
	}
	tag, err := executor.Exec(ctx, `
		UPDATE allocation_reconcile_queue
		SET next_run_at = $3,
			reconcile_attempts = CASE WHEN $4 THEN reconcile_attempts + 1 ELSE reconcile_attempts END,
			last_error = $5,
			updated_at = clock_timestamp()
		WHERE allocation_id = $1 AND reason = $2
	`, strings.TrimSpace(req.AllocationID), strings.TrimSpace(req.Reason), nextRunAt.UTC(), req.IncrementAttempts, strings.TrimSpace(req.LastReconcileError))
	if err != nil {
		return false, fmt.Errorf("reschedule allocation reconcile: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func ScheduleClaimedReconcile(ctx context.Context, executor reconcileExecutor, req allocationkernel.ScheduleReconcileRequest, owner string, now time.Time) (bool, error) {
	nextRunAt := req.NextRunAt
	if nextRunAt.IsZero() {
		nextRunAt = now
	}
	tag, err := executor.Exec(ctx, `
		UPDATE allocation_reconcile_queue
		SET reason = $2,
			next_run_at = $3,
			reconcile_attempts = CASE WHEN $4 THEN reconcile_attempts + 1 ELSE reconcile_attempts END,
			last_error = $5,
			lease_owner = '',
			lease_expires_at = NULL,
			updated_at = clock_timestamp()
		WHERE allocation_id = $1 AND lease_owner = $6
	`, strings.TrimSpace(req.AllocationID), strings.TrimSpace(req.Reason), nextRunAt.UTC(), req.IncrementAttempts, strings.TrimSpace(req.LastReconcileError), strings.TrimSpace(owner))
	if err != nil {
		return false, fmt.Errorf("schedule claimed allocation reconcile: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func ListLifecycleRetries(ctx context.Context, queryer reconcileQueryer, filter allocationkernel.LifecycleRetryFilter, now time.Time) ([]allocationkernel.LifecycleRetryItem, error) {
	filter = allocationkernel.NormalizeLifecycleRetryFilter(filter)
	if err := allocationkernel.ValidateLifecycleRetryFilter(filter); err != nil {
		return nil, err
	}
	rows, err := queryer.Query(ctx, `
		SELECT q.allocation_id, a.owner_id, a.owner_type, a.environment_id, q.reason, a.node_id, n.node_target, a.attempt,
			q.reconcile_attempts, q.last_error, q.next_run_at, q.created_at, q.updated_at,
			a.status,
			EXISTS (
				SELECT 1 FROM workload_reservations wr
				WHERE wr.allocation_id = q.allocation_id AND wr.released_at IS NULL
			),
			EXISTS (
				SELECT 1 FROM execution_leases el
				WHERE el.allocation_id = q.allocation_id AND el.revoked = FALSE AND el.expires_at > $1
			),
			EXISTS (
				SELECT 1 FROM tunnel_sessions ts
				WHERE ts.allocation_id = q.allocation_id
				  AND ts.revoked = FALSE
				  AND ts.status IN ($6, $7, $8)
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
		WHERE ($2 = '' OR a.owner_type = $2)
		  AND ($3 = '' OR q.reason = $3)
		  AND (NOT $4 OR q.next_run_at <= $1)
		ORDER BY q.created_at ASC, q.allocation_id ASC
		LIMIT $5
	`, now.UTC(), filter.OwnerType, filter.Reason, filter.DueOnly, filter.Limit,
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String())
	if err != nil {
		return nil, fmt.Errorf("query allocation lifecycle retry queue: %w", err)
	}
	defer rows.Close()

	now = now.UTC()
	return scanLifecycleRetryRows(rows, now)
}

func DebugReconcileItems(ctx context.Context, queryer reconcileQueryer, now time.Time, limit int) ([]allocationkernel.LifecycleRetryItem, error) {
	return ListLifecycleRetries(ctx, queryer, allocationkernel.LifecycleRetryFilter{Limit: limit}, now)
}

func LoadLifecycleRetry(ctx context.Context, queryer reconcileQueryer, allocationID string, reason string, now time.Time) (*allocationkernel.LifecycleRetryItem, bool, error) {
	rows, err := queryer.Query(ctx, `
		SELECT q.allocation_id, a.owner_id, a.owner_type, a.environment_id, q.reason, a.node_id, n.node_target, a.attempt,
			q.reconcile_attempts, q.last_error, q.next_run_at, q.created_at, q.updated_at,
			a.status,
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
		LIMIT 1
	`, strings.TrimSpace(allocationID), strings.TrimSpace(reason), now.UTC(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(),
		tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String())
	if err != nil {
		return nil, false, fmt.Errorf("load allocation lifecycle retry: %w", err)
	}
	defer rows.Close()
	items, err := scanLifecycleRetryRows(rows, now)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return nil, false, nil
	}
	return &items[0], true, nil
}

func scanLifecycleRetryRows(rows pgx.Rows, now time.Time) ([]allocationkernel.LifecycleRetryItem, error) {
	now = now.UTC()
	out := make([]allocationkernel.LifecycleRetryItem, 0)
	for rows.Next() {
		var item allocationkernel.LifecycleRetryItem
		clearanceInput := allocationkernel.LifecycleRetryClearanceInput{}
		if err := rows.Scan(
			&item.AllocationID,
			&item.OwnerID,
			&item.OwnerType,
			&item.EnvironmentID,
			&item.Reason,
			&item.NodeID,
			&item.NodeTarget,
			&item.Attempt,
			&item.ReconcileAttempts,
			&item.LastReconcileError,
			&item.NextRunAt,
			&item.CreatedAt,
			&item.UpdatedAt,
			&clearanceInput.AllocationStatus,
			&clearanceInput.HasActiveReservation,
			&clearanceInput.HasActiveLease,
			&clearanceInput.HasActiveTunnelSession,
			&clearanceInput.OwnerRunStatus,
			&clearanceInput.OwnerServiceReferencesAllocation,
		); err != nil {
			return nil, err
		}
		item.AgeSeconds = max(int64(now.Sub(item.CreatedAt).Seconds()), 0)
		item.Due = !item.NextRunAt.After(now)
		clearanceInput.AllocationID = item.AllocationID
		clearanceInput.OwnerType = item.OwnerType
		clearance := allocationkernel.EvaluateLifecycleRetryClearance(clearanceInput)
		item.Clearable = clearance.Clearable
		item.ClearBlockedReason = clearance.BlockedReason
		out = append(out, item)
	}
	return out, rows.Err()
}
