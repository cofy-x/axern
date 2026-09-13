package app

import (
	"context"
	"fmt"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

func (a *App) observePostgresPoolConnections(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	stats := a.db.Pool().Stat()
	observe(int64(stats.MaxConns()), attribute.String(sdkobs.AttrState, "max"))
	observe(int64(stats.TotalConns()), attribute.String(sdkobs.AttrState, "total"))
	observe(int64(stats.AcquiredConns()), attribute.String(sdkobs.AttrState, "acquired"))
	observe(int64(stats.IdleConns()), attribute.String(sdkobs.AttrState, "idle"))
	return nil
}

func (a *App) observeAllocations(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT lifecycle_state, count(*)
		FROM allocations
		GROUP BY lifecycle_state
	`)
	if err != nil {
		return fmt.Errorf("query allocation metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lifecycleState string
		var count int64
		if err := rows.Scan(&lifecycleState, &count); err != nil {
			return err
		}
		observe(count,
			attribute.String(sdkobs.AttrState, lifecycleState),
		)
	}
	return rows.Err()
}

func (a *App) observeNodeAllocations(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT node_id, lifecycle_state, count(*)
		FROM allocations
		GROUP BY node_id, lifecycle_state
	`)
	if err != nil {
		return fmt.Errorf("query node allocation metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, lifecycleState string
		var count int64
		if err := rows.Scan(&nodeID, &lifecycleState, &count); err != nil {
			return err
		}
		observe(count,
			attribute.String(sdkobs.AttrNodeID, nodeID),
			attribute.String(sdkobs.AttrState, lifecycleState),
		)
	}
	return rows.Err()
}

type allocationReconcileMetricRow struct {
	reason           string
	count            int64
	maxAttempts      int64
	oldestAgeSeconds int64
}

func (a *App) allocationReconcileQueueMetrics(ctx context.Context) ([]allocationReconcileMetricRow, error) {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT q.reason, count(*), COALESCE(max(q.reconcile_attempts), 0),
			FLOOR(GREATEST(0, EXTRACT(EPOCH FROM ($1::timestamptz - min(q.created_at)))))::bigint
		FROM allocation_reconcile_queue q
		GROUP BY q.reason
	`, a.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("query allocation reconcile queue metrics: %w", err)
	}
	defer rows.Close()
	metrics := []allocationReconcileMetricRow{
		{reason: allocationkernel.ReconcileReasonCreate},
		{reason: allocationkernel.ReconcileReasonDelete},
	}
	for rows.Next() {
		var row allocationReconcileMetricRow
		if err := rows.Scan(&row.reason, &row.count, &row.maxAttempts, &row.oldestAgeSeconds); err != nil {
			return nil, err
		}
		replaced := false
		for i := range metrics {
			if metrics[i].reason == row.reason {
				metrics[i] = row
				replaced = true
				break
			}
		}
		if !replaced {
			metrics = append(metrics, row)
		}
	}
	return metrics, rows.Err()
}

func (a *App) observeAllocationReconcileQueue(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.allocationReconcileQueueMetrics(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		observe(row.count, attribute.String(sdkobs.AttrReason, row.reason))
	}
	return nil
}

func (a *App) observeAllocationReconcileQueueOldestAge(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.allocationReconcileQueueMetrics(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		observe(row.oldestAgeSeconds, attribute.String(sdkobs.AttrReason, row.reason))
	}
	return nil
}

func (a *App) observeAllocationReconcileAttempts(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.allocationReconcileQueueMetrics(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		observe(row.maxAttempts, attribute.String(sdkobs.AttrReason, row.reason))
	}
	return nil
}

func (a *App) observeCapabilityConditionAllocations(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT condition->>'state', count(DISTINCT allocation_id)
		FROM allocation_capability_conditions, LATERAL jsonb_array_elements(conditions->'conditions') condition
		WHERE condition->>'state' IN (
			'CAPABILITY_CONDITION_STATE_DEGRADED',
			'CAPABILITY_CONDITION_STATE_FAILED',
			'CAPABILITY_CONDITION_STATE_UNKNOWN'
		)
		GROUP BY condition->>'state'
	`)
	if err != nil {
		return fmt.Errorf("query capability condition allocation metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			return err
		}
		observe(count, attribute.String(sdkobs.AttrState, state))
	}
	return rows.Err()
}
