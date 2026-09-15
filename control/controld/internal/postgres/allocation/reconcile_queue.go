package pgallocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/encoding/protojson"
)

type reconcileQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type reconcileClaimQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type reconcileExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const capabilityDependenciesProjectionSQL = `COALESCE((
	SELECT COALESCE(jsonb_agg(jsonb_build_object('key', cd.capability_key, 'lossPolicy', cd.loss_policy) ORDER BY cd.capability_key_id), '[]'::jsonb)
	FROM allocation_capability_requirements cd
	WHERE cd.allocation_id = a.allocation_id
), '[]'::jsonb)`

func ClaimDueReconcileItems(ctx context.Context, queryer reconcileQueryer, owner string, limit int, now time.Time, claimTTL time.Duration) ([]allocationkernel.ReconcileItem, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("reconcile claim owner is required")
	}
	if limit <= 0 {
		limit = allocationkernel.DefaultReconcileLimit
	}
	if claimTTL <= 0 {
		return nil, fmt.Errorf("reconcile claim TTL must be positive")
	}
	claimExpiresAt := now.Add(claimTTL).UTC()
	rows, err := queryer.Query(ctx, `
		WITH ranked AS (
			SELECT q.allocation_id, a.run_id, r.environment_id, a.lifecycle_state, a.node_id, n.node_target,
				q.reconcile_attempts, q.last_error, q.next_run_at,
				`+capabilityDependenciesProjectionSQL+` AS capability_requirements,
				GREATEST(q.next_run_at, q.updated_at, COALESCE(q.claim_expires_at, '-infinity'::timestamptz)) AS eligible_at, a.output_expires_at,
				ROW_NUMBER() OVER (PARTITION BY a.node_id ORDER BY q.next_run_at ASC, q.allocation_id ASC) AS node_rank
			FROM allocation_reconcile_queue q
			JOIN allocations a ON a.allocation_id = q.allocation_id
			JOIN runs r ON r.run_id = a.run_id
			JOIN nodes n ON n.node_id = a.node_id
			WHERE q.next_run_at <= $1
			  AND (q.claim_expires_at IS NULL OR q.claim_expires_at <= $1)
		), candidates AS (
			SELECT r.allocation_id, r.run_id, r.environment_id, r.lifecycle_state, r.node_id, r.node_target,
				r.reconcile_attempts, r.last_error, r.next_run_at, r.capability_requirements, r.eligible_at, r.output_expires_at
			FROM ranked r
			JOIN allocation_reconcile_queue q ON q.allocation_id = r.allocation_id
			ORDER BY r.node_rank ASC, r.next_run_at ASC, r.allocation_id ASC
			LIMIT $2
			FOR UPDATE OF q SKIP LOCKED
		), claimed AS (
			UPDATE allocation_reconcile_queue q
			SET claim_owner = $3, claim_expires_at = $4, updated_at = $1
			FROM candidates c
			WHERE q.allocation_id = c.allocation_id
			  AND (q.claim_expires_at IS NULL OR q.claim_expires_at <= $1)
			RETURNING q.allocation_id
		)
		SELECT c.allocation_id, c.run_id, c.environment_id, c.lifecycle_state, c.node_id, c.node_target,
			c.reconcile_attempts, c.last_error, c.next_run_at, c.capability_requirements, c.eligible_at, c.output_expires_at
		FROM candidates c
		JOIN claimed USING (allocation_id)
		ORDER BY c.allocation_id ASC
	`, now.UTC(), limit, owner, claimExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("claim reconcile queue: %w", err)
	}
	defer rows.Close()
	out := make([]allocationkernel.ReconcileItem, 0)
	for rows.Next() {
		item := allocationkernel.ReconcileItem{ClaimOwner: owner}
		var dependenciesJSON []byte
		var lifecycleState string
		if err := rows.Scan(&item.AllocationID, &item.RunID, &item.EnvironmentID, &lifecycleState, &item.NodeID, &item.NodeTarget, &item.ReconcileAttempts, &item.LastReconcileError, &item.NextRunAt, &dependenciesJSON, &item.EligibleAt, &item.OutputExpiresAt); err != nil {
			return nil, err
		}
		item.LifecycleState = allocationkernel.ParseLifecycleState(lifecycleState)
		if err := decodeCapabilityRequirements(dependenciesJSON, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func RequireReconcileClaim(ctx context.Context, queryer reconcileClaimQueryer, allocationID, owner string, intent allocationkernel.ReconcileIntent, now time.Time) error {
	var held bool
	err := queryer.QueryRow(ctx, `
		SELECT TRUE
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		WHERE q.allocation_id = $1 AND q.claim_owner = $2
		  AND q.claim_expires_at > $3
		  AND (($4 = 1 AND a.lifecycle_state IN ($5, $6, $7))
		    OR ($4 = 2 AND a.lifecycle_state IN ($8, $9)))
		FOR UPDATE OF q
	`, strings.TrimSpace(allocationID), strings.TrimSpace(owner), now.UTC(), intent,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()).Scan(&held)
	if errors.Is(err, pgx.ErrNoRows) {
		return allocationkernel.ErrReconcileClaimLost
	}
	if err != nil {
		return fmt.Errorf("verify allocation reconcile claim: %w", err)
	}
	if !held {
		return allocationkernel.ErrReconcileClaimLost
	}
	return nil
}

func decodeCapabilityRequirements(payload []byte, item *allocationkernel.ReconcileItem) error {
	var raw []json.RawMessage
	if len(payload) > 0 && string(payload) != "null" {
		if err := json.Unmarshal(payload, &raw); err != nil {
			return fmt.Errorf("unmarshal allocation capability requirements: %w", err)
		}
	}
	for _, entry := range raw {
		requirement := &capabilityv1.CapabilityRequirement{}
		if err := protojson.Unmarshal(entry, requirement); err != nil {
			return fmt.Errorf("unmarshal allocation capability requirement: %w", err)
		}
		item.CapabilityRequirements = append(item.CapabilityRequirements, requirement)
	}
	return nil
}

func RenewReconcileClaim(ctx context.Context, executor reconcileExecutor, allocationID, owner string, now time.Time, claimTTL time.Duration) (bool, error) {
	if claimTTL <= 0 {
		return false, fmt.Errorf("reconcile claim TTL must be positive")
	}
	tag, err := executor.Exec(ctx, `
		UPDATE allocation_reconcile_queue
		SET claim_expires_at = $3, updated_at = $2
		WHERE allocation_id = $1 AND claim_owner = $4
		  AND claim_expires_at > $2
	`, strings.TrimSpace(allocationID), now.UTC(), now.Add(claimTTL).UTC(), strings.TrimSpace(owner))
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
	tag, err := executor.Exec(ctx, `
		INSERT INTO allocation_reconcile_queue(allocation_id, next_run_at, reconcile_attempts, last_error, created_at, updated_at)
		SELECT a.allocation_id, $2, CASE WHEN $3 THEN 1 ELSE 0 END, $4, $5, $5
		FROM allocations a
		WHERE a.allocation_id = $1
		  AND (($6 = 1 AND a.lifecycle_state IN ($7, $8, $9))
		    OR ($6 = 2 AND a.lifecycle_state IN ($10, $11)))
		ON CONFLICT (allocation_id) DO UPDATE SET
			next_run_at = EXCLUDED.next_run_at,
			reconcile_attempts = CASE WHEN $3 THEN allocation_reconcile_queue.reconcile_attempts + 1 ELSE 0 END,
			last_error = EXCLUDED.last_error,
			claim_owner = '',
			claim_expires_at = NULL,
			updated_at = $5
	`, strings.TrimSpace(req.AllocationID), nextRunAt.UTC(), req.IncrementAttempts, strings.TrimSpace(req.LastReconcileError), now.UTC(), req.Intent,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String())
	if err != nil {
		return fmt.Errorf("schedule allocation reconcile: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("allocation %q lifecycle does not accept reconcile intent %d", req.AllocationID, req.Intent)
	}
	return nil
}

// ScheduleClaimedReconcile updates or replaces only work still owned by the
// calling worker. A stale worker cannot recreate work that another worker or
// an operator has already replaced or removed.
func ScheduleClaimedReconcile(ctx context.Context, executor reconcileExecutor, req allocationkernel.ScheduleReconcileRequest, owner string, now time.Time) (bool, error) {
	nextRunAt := req.NextRunAt
	if nextRunAt.IsZero() {
		nextRunAt = now
	}
	tag, err := executor.Exec(ctx, `
		UPDATE allocation_reconcile_queue
		SET next_run_at = $2,
			reconcile_attempts = CASE WHEN $3 THEN reconcile_attempts + 1 ELSE reconcile_attempts END,
			last_error = $4,
			claim_owner = '',
			claim_expires_at = NULL,
			updated_at = $6
		WHERE allocation_id = $1 AND claim_owner = $5
		  AND claim_expires_at > $6
		  AND EXISTS (
			SELECT 1 FROM allocations a
			WHERE a.allocation_id = allocation_reconcile_queue.allocation_id
			  AND (($7 = 1 AND a.lifecycle_state IN ($8, $9, $10))
			    OR ($7 = 2 AND a.lifecycle_state IN ($11, $12)))
		  )
	`, strings.TrimSpace(req.AllocationID), nextRunAt.UTC(), req.IncrementAttempts, strings.TrimSpace(req.LastReconcileError), strings.TrimSpace(owner), now.UTC(), req.Intent,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String())
	if err != nil {
		return false, fmt.Errorf("schedule claimed allocation reconcile: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func ListLifecycleRetries(ctx context.Context, queryer reconcileQueryer, filter allocationkernel.LifecycleRetryFilter, now time.Time) ([]allocationkernel.LifecycleRetryItem, error) {
	filter = allocationkernel.NormalizeLifecycleRetryFilter(filter)
	rows, err := queryer.Query(ctx, `
		SELECT q.allocation_id, a.run_id, r.environment_id, a.lifecycle_state, a.node_id, n.node_target,
			q.reconcile_attempts, q.last_error, q.next_run_at, q.created_at, q.updated_at,
			a.lifecycle_state,
			EXISTS (
				SELECT 1 FROM allocation_access_grants ag
				WHERE ag.allocation_id = q.allocation_id AND ag.revoked = FALSE AND ag.expires_at > $1
			),
			EXISTS (
				SELECT 1 FROM tunnel_sessions ts
				WHERE ts.allocation_id = q.allocation_id
				  AND ts.status IN ($4, $5, $6)
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
		WHERE (NOT $2 OR q.next_run_at <= $1)
		ORDER BY q.created_at ASC, q.allocation_id ASC
		LIMIT $3
	`, now.UTC(), filter.DueOnly, filter.Limit,
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

func LoadLifecycleRetry(ctx context.Context, queryer reconcileQueryer, allocationID string, now time.Time) (*allocationkernel.LifecycleRetryItem, bool, error) {
	rows, err := queryer.Query(ctx, `
		SELECT q.allocation_id, a.run_id, r.environment_id, a.lifecycle_state, a.node_id, n.node_target,
			q.reconcile_attempts, q.last_error, q.next_run_at, q.created_at, q.updated_at,
			a.lifecycle_state,
			EXISTS (
				SELECT 1 FROM allocation_access_grants ag
				WHERE ag.allocation_id = q.allocation_id AND ag.revoked = FALSE AND ag.expires_at > $2
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
		LIMIT 1
	`, strings.TrimSpace(allocationID), now.UTC(),
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
		var lifecycleState string
		clearanceInput := allocationkernel.LifecycleRetryClearanceInput{}
		if err := rows.Scan(
			&item.AllocationID,
			&item.RunID,
			&item.EnvironmentID,
			&lifecycleState,
			&item.NodeID,
			&item.NodeTarget,
			&item.ReconcileAttempts,
			&item.LastReconcileError,
			&item.NextRunAt,
			&item.CreatedAt,
			&item.UpdatedAt,
			&clearanceInput.AllocationState,
			&clearanceInput.HasActiveAccessGrant,
			&clearanceInput.HasActiveTunnelSession,
			&clearanceInput.RunStatus,
		); err != nil {
			return nil, err
		}
		item.LifecycleState = allocationkernel.ParseLifecycleState(lifecycleState).String()
		item.AgeSeconds = max(int64(now.Sub(item.CreatedAt).Seconds()), 0)
		item.Due = !item.NextRunAt.After(now)
		clearanceInput.AllocationID = item.AllocationID
		clearance := allocationkernel.EvaluateLifecycleRetryClearance(clearanceInput)
		item.Clearable = clearance.Clearable
		item.ClearBlockedReason = clearance.BlockedReason
		out = append(out, item)
	}
	return out, rows.Err()
}
