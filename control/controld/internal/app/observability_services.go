package app

import (
	"context"
	"fmt"
	"slices"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"go.opentelemetry.io/otel/attribute"
)

func (a *App) observeServices(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	counts := map[string]int64{}
	rows, err := a.db.Pool().Query(ctx, `SELECT status, count(*) FROM services GROUP BY status`)
	if err != nil {
		return fmt.Errorf("query service metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return err
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, status := range observableServiceStatuses() {
		observe(counts[status], attribute.String(sdkobs.AttrStatus, status))
	}
	for status, count := range counts {
		if !knownString(status, observableServiceStatuses()) {
			observe(count, attribute.String(sdkobs.AttrStatus, status))
		}
	}
	return nil
}

func (a *App) observeServiceReplicas(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	var desired, ready, unhealthy int64
	if err := a.db.Pool().QueryRow(ctx, `
		SELECT COALESCE(sum(replicas), 0), COALESCE(sum(ready_replicas), 0), COALESCE(sum(unhealthy_replicas), 0)
		FROM services
		WHERE status NOT IN ($1, $2)
	`, servicev1.ServiceStatus_SERVICE_STATUS_DELETING.String(), servicev1.ServiceStatus_SERVICE_STATUS_DELETED.String()).Scan(&desired, &ready, &unhealthy); err != nil {
		return fmt.Errorf("query service replica metrics: %w", err)
	}
	observe(desired, attribute.String(sdkobs.AttrState, "desired"))
	observe(ready, attribute.String(sdkobs.AttrState, "ready"))
	observe(unhealthy, attribute.String(sdkobs.AttrState, "unhealthy"))
	return nil
}

func (a *App) observeServiceWatch(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	stats := a.servicePG.WatchStats()
	observe(int64(stats.Active), attribute.String(sdkobs.AttrState, "active"))
	ready := int64(0)
	if stats.ListenerReady {
		ready = 1
	}
	observe(ready, attribute.String(sdkobs.AttrState, "listener_ready"))
	return nil
}

func (a *App) observeRolloutNotifications(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	stats := a.rolloutPG.NotificationStats()
	observe(int64(stats.EventWaiters), attribute.String(sdkobs.AttrState, "event_waiters"))
	observe(int64(stats.WorkWaiters), attribute.String(sdkobs.AttrState, "work_waiters"))
	ready := int64(0)
	if stats.ListenerReady {
		ready = 1
	}
	observe(ready, attribute.String(sdkobs.AttrState, "listener_ready"))
	return nil
}

type rolloutWorkQueueCounts struct {
	pendingDue       int64
	pendingScheduled int64
	leasedActive     int64
	leasedExpired    int64
}

func (a *App) rolloutWorkQueueCounts(ctx context.Context) (rolloutWorkQueueCounts, error) {
	counts := rolloutWorkQueueCounts{}
	err := a.db.Pool().QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status='PENDING' AND cancel_requested=FALSE AND next_run_at<=now()),
			count(*) FILTER (WHERE status='PENDING' AND cancel_requested=FALSE AND next_run_at>now()),
			count(*) FILTER (WHERE status='LEASED' AND lease_expires_at>now()),
			count(*) FILTER (WHERE status='LEASED' AND cancel_requested=FALSE AND lease_expires_at<=now())
		FROM rollout_work_items
		WHERE (status='PENDING' AND cancel_requested=FALSE) OR status='LEASED'
	`).Scan(
		&counts.pendingDue,
		&counts.pendingScheduled,
		&counts.leasedActive,
		&counts.leasedExpired,
	)
	if err != nil {
		return rolloutWorkQueueCounts{}, fmt.Errorf("query rollout work queue counts: %w", err)
	}
	return counts, nil
}

type rolloutWorkQueueAges struct {
	pendingDueSeconds float64
	leasedDueSeconds  float64
}

func (a *App) rolloutWorkQueueAges(ctx context.Context) (rolloutWorkQueueAges, error) {
	ages := rolloutWorkQueueAges{}
	err := a.db.Pool().QueryRow(ctx, `
		SELECT
			COALESCE(EXTRACT(EPOCH FROM now()-(
				SELECT next_run_at
				FROM rollout_work_items
				WHERE status='PENDING' AND cancel_requested=FALSE AND next_run_at<=now()
				ORDER BY next_run_at,work_id
				LIMIT 1
			)),0),
			COALESCE(EXTRACT(EPOCH FROM now()-(
				SELECT lease_expires_at
				FROM rollout_work_items
				WHERE status='LEASED' AND cancel_requested=FALSE AND lease_expires_at<=now()
				ORDER BY lease_expires_at,work_id
				LIMIT 1
			)),0)
	`).Scan(&ages.pendingDueSeconds, &ages.leasedDueSeconds)
	if err != nil {
		return rolloutWorkQueueAges{}, fmt.Errorf("query rollout work queue ages: %w", err)
	}
	return ages, nil
}

func (a *App) observeRolloutWorkQueue(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	counts, err := a.rolloutWorkQueueCounts(ctx)
	if err != nil {
		return err
	}
	observe(counts.pendingDue, attribute.String(sdkobs.AttrState, "pending_due"))
	observe(counts.pendingScheduled, attribute.String(sdkobs.AttrState, "pending_scheduled"))
	observe(counts.leasedActive, attribute.String(sdkobs.AttrState, "leased_active"))
	observe(counts.leasedExpired, attribute.String(sdkobs.AttrState, "leased_expired"))
	return nil
}

func (a *App) observeRolloutWorkOldestDueAge(ctx context.Context, observe sdkobs.Float64GaugeObserver) error {
	ages, err := a.rolloutWorkQueueAges(ctx)
	if err != nil {
		return err
	}
	observe(ages.pendingDueSeconds, attribute.String(sdkobs.AttrState, "pending_due"))
	observe(ages.leasedDueSeconds, attribute.String(sdkobs.AttrState, "leased_expired"))
	return nil
}

func (a *App) observeFunctionInvocationQueue(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	var queuedDue, queuedScheduled, leasedActive, leasedExpired int64
	if err := a.db.Pool().QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status='FUNCTION_INVOCATION_STATUS_QUEUED' AND next_run_at<=now()),
			count(*) FILTER (WHERE status='FUNCTION_INVOCATION_STATUS_QUEUED' AND next_run_at>now()),
			count(*) FILTER (WHERE status='FUNCTION_INVOCATION_STATUS_RUNNING' AND lease_expires_at>now()),
			count(*) FILTER (WHERE status='FUNCTION_INVOCATION_STATUS_RUNNING' AND lease_expires_at<=now())
		FROM function_invocations
		WHERE mode='FUNCTION_INVOCATION_MODE_ASYNC'
		  AND status IN ('FUNCTION_INVOCATION_STATUS_QUEUED','FUNCTION_INVOCATION_STATUS_RUNNING')
	`).Scan(&queuedDue, &queuedScheduled, &leasedActive, &leasedExpired); err != nil {
		return fmt.Errorf("query function invocation queue counts: %w", err)
	}
	observe(queuedDue, attribute.String(sdkobs.AttrState, "queued_due"))
	observe(queuedScheduled, attribute.String(sdkobs.AttrState, "queued_scheduled"))
	observe(leasedActive, attribute.String(sdkobs.AttrState, "leased_active"))
	observe(leasedExpired, attribute.String(sdkobs.AttrState, "leased_expired"))
	return nil
}

func (a *App) observeFunctionInvocationNotifications(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	ready := int64(0)
	if a.functionPG != nil && a.functionPG.InvocationListenerReady() {
		ready = 1
	}
	observe(ready, attribute.String(sdkobs.AttrState, "listener_ready"))
	return nil
}

func (a *App) observeFunctionInvocationOldestDueAge(ctx context.Context, observe sdkobs.Float64GaugeObserver) error {
	var queuedDue, leasedExpired float64
	if err := a.db.Pool().QueryRow(ctx, `
		SELECT
			COALESCE(EXTRACT(EPOCH FROM now()-(
				SELECT next_run_at FROM function_invocations
				WHERE mode='FUNCTION_INVOCATION_MODE_ASYNC' AND status='FUNCTION_INVOCATION_STATUS_QUEUED' AND next_run_at<=now()
				ORDER BY next_run_at,created_at,invocation_id LIMIT 1
			)),0),
			COALESCE(EXTRACT(EPOCH FROM now()-(
				SELECT lease_expires_at FROM function_invocations
				WHERE mode='FUNCTION_INVOCATION_MODE_ASYNC' AND status='FUNCTION_INVOCATION_STATUS_RUNNING' AND lease_expires_at<=now()
				ORDER BY lease_expires_at,invocation_id LIMIT 1
			)),0)
	`).Scan(&queuedDue, &leasedExpired); err != nil {
		return fmt.Errorf("query function invocation queue ages: %w", err)
	}
	observe(queuedDue, attribute.String(sdkobs.AttrState, "queued_due"))
	observe(leasedExpired, attribute.String(sdkobs.AttrState, "leased_expired"))
	return nil
}

func (a *App) observeVolumeReclaimQueue(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	if a.storage == nil {
		return nil
	}
	health, err := a.storage.VolumeReclaimQueueHealth(ctx)
	if err != nil {
		return err
	}
	observe(health.GetDue(), attribute.String(sdkobs.AttrState, "due"))
	observe(health.GetScheduled(), attribute.String(sdkobs.AttrState, "scheduled"))
	observe(health.GetLeasedActive(), attribute.String(sdkobs.AttrState, "leased_active"))
	observe(health.GetLeasedExpired(), attribute.String(sdkobs.AttrState, "leased_expired"))
	return nil
}

func (a *App) observeVolumeReclaimOldestDueAge(ctx context.Context, observe sdkobs.Float64GaugeObserver) error {
	if a.storage == nil {
		return nil
	}
	health, err := a.storage.VolumeReclaimQueueHealth(ctx)
	if err != nil {
		return err
	}
	observe(health.GetOldestDueAgeSeconds())
	return nil
}

func (a *App) observePostgresPoolConnections(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	stats := a.db.Pool().Stat()
	observe(int64(stats.MaxConns()), attribute.String(sdkobs.AttrState, "max"))
	observe(int64(stats.TotalConns()), attribute.String(sdkobs.AttrState, "total"))
	observe(int64(stats.AcquiredConns()), attribute.String(sdkobs.AttrState, "acquired"))
	observe(int64(stats.IdleConns()), attribute.String(sdkobs.AttrState, "idle"))
	return nil
}

func (a *App) observeAllocations(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	counts := map[allocationMetricKey]int64{}
	rows, err := a.db.Pool().Query(ctx, `
		SELECT owner_type, status, ready, count(*)
		FROM allocations
		WHERE status IN ($1, $2, $3, $4, $5)
		GROUP BY owner_type, status, ready
	`,
		commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING.String(),
	)
	if err != nil {
		return fmt.Errorf("query allocation metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ownerType, status string
		var ready bool
		var count int64
		if err := rows.Scan(&ownerType, &status, &ready, &count); err != nil {
			return err
		}
		counts[allocationMetricKey{ownerType: ownerType, status: status, ready: ready}] = count
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range observableAllocationKeys() {
		observeAllocationCount(observe, counts[key], key.ownerType, key.status, key.ready)
	}
	for key, count := range counts {
		if !knownAllocationMetricKey(key, observableAllocationKeys()) {
			observeAllocationCount(observe, count, key.ownerType, key.status, key.ready)
		}
	}
	return nil
}

func (a *App) observeNodeAllocations(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	counts := map[nodeAllocationMetricKey]int64{}
	rows, err := a.db.Pool().Query(ctx, `
		SELECT node_id, owner_type, status, ready, count(*)
		FROM allocations
		WHERE status IN ($1, $2, $3, $4, $5)
		GROUP BY node_id, owner_type, status, ready
	`,
		commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING.String(),
	)
	if err != nil {
		return fmt.Errorf("query node allocation metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key nodeAllocationMetricKey
		var count int64
		if err := rows.Scan(&key.nodeID, &key.ownerType, &key.status, &key.ready, &count); err != nil {
			return err
		}
		counts[key] = count
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, record := range a.readyNodeRecords() {
		for _, key := range observableAllocationKeys() {
			nodeKey := nodeAllocationMetricKey{nodeID: record.NodeID, ownerType: key.ownerType, status: key.status, ready: key.ready}
			observeNodeAllocationCount(observe, counts[nodeKey], nodeKey)
			delete(counts, nodeKey)
		}
	}
	for key, count := range counts {
		observeNodeAllocationCount(observe, count, key)
	}
	return nil
}

func (a *App) observeAllocationReconcileQueue(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.allocationReconcileQueueMetrics(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		observe(row.count,
			attribute.String(sdkobs.AttrOwnerType, row.ownerType),
			attribute.String(sdkobs.AttrReason, row.reason),
		)
	}
	return nil
}

func (a *App) observeAllocationReconcileQueueOldestAge(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.allocationReconcileQueueMetrics(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		observe(row.oldestAgeSeconds,
			attribute.String(sdkobs.AttrOwnerType, row.ownerType),
			attribute.String(sdkobs.AttrReason, row.reason),
		)
	}
	return nil
}

func (a *App) observeAllocationReconcileAttempts(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	rows, err := a.allocationReconcileQueueMetrics(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		observe(row.maxAttempts,
			attribute.String(sdkobs.AttrOwnerType, row.ownerType),
			attribute.String(sdkobs.AttrReason, row.reason),
		)
	}
	return nil
}

type allocationReconcileMetricRow struct {
	ownerType        string
	reason           string
	count            int64
	maxAttempts      int64
	oldestAgeSeconds int64
}

func (a *App) allocationReconcileQueueMetrics(ctx context.Context) ([]allocationReconcileMetricRow, error) {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT a.owner_type, q.reason, count(*), COALESCE(max(q.reconcile_attempts), 0),
			FLOOR(GREATEST(0, EXTRACT(EPOCH FROM ($1::timestamptz - min(q.created_at)))))::bigint
		FROM allocation_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		GROUP BY a.owner_type, q.reason
	`, a.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("query allocation reconcile queue metrics: %w", err)
	}
	defer rows.Close()
	metrics := defaultAllocationReconcileMetricRows()
	index := make(map[allocationReconcileMetricKey]int, len(metrics))
	for i, row := range metrics {
		index[allocationReconcileMetricKey{ownerType: row.ownerType, reason: row.reason}] = i
	}
	for rows.Next() {
		var row allocationReconcileMetricRow
		if err := rows.Scan(&row.ownerType, &row.reason, &row.count, &row.maxAttempts, &row.oldestAgeSeconds); err != nil {
			return nil, err
		}
		key := allocationReconcileMetricKey{ownerType: row.ownerType, reason: row.reason}
		if i, ok := index[key]; ok {
			metrics[i] = row
			continue
		}
		metrics = append(metrics, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return metrics, nil
}

type allocationReconcileMetricKey struct {
	ownerType string
	reason    string
}

func defaultAllocationReconcileMetricRows() []allocationReconcileMetricRow {
	ownerTypes := []string{allocationkernel.OwnerRun, allocationkernel.OwnerService}
	reasons := []string{allocationkernel.ReconcileReasonCreate, allocationkernel.ReconcileReasonDelete}
	rows := make([]allocationReconcileMetricRow, 0, len(ownerTypes)*len(reasons))
	for _, ownerType := range ownerTypes {
		for _, reason := range reasons {
			rows = append(rows, allocationReconcileMetricRow{ownerType: ownerType, reason: reason})
		}
	}
	return rows
}

type allocationMetricKey struct {
	ownerType string
	status    string
	ready     bool
}

type nodeAllocationMetricKey struct {
	nodeID    string
	ownerType string
	status    string
	ready     bool
}

func observeAllocationCount(observe sdkobs.Int64GaugeObserver, count int64, ownerType, status string, ready bool) {
	observe(count,
		attribute.String(sdkobs.AttrOwnerType, ownerType),
		attribute.String(sdkobs.AttrStatus, status),
		attribute.Bool(sdkobs.AttrReady, ready),
	)
}

func observeNodeAllocationCount(observe sdkobs.Int64GaugeObserver, count int64, key nodeAllocationMetricKey) {
	observe(count,
		attribute.String(sdkobs.AttrNodeID, key.nodeID),
		attribute.String(sdkobs.AttrOwnerType, key.ownerType),
		attribute.String(sdkobs.AttrStatus, key.status),
		attribute.Bool(sdkobs.AttrReady, key.ready),
	)
}

func observableServiceStatuses() []string {
	return []string{
		servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING.String(),
		servicev1.ServiceStatus_SERVICE_STATUS_READY.String(),
		servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED.String(),
		servicev1.ServiceStatus_SERVICE_STATUS_FAILED.String(),
		servicev1.ServiceStatus_SERVICE_STATUS_DELETING.String(),
		servicev1.ServiceStatus_SERVICE_STATUS_DELETED.String(),
	}
}

func observableAllocationStatuses() []string {
	return []string{
		commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING.String(),
	}
}

func observableAllocationKeys() []allocationMetricKey {
	keys := make([]allocationMetricKey, 0, len(observableAllocationStatuses())*3)
	for _, status := range observableAllocationStatuses() {
		keys = append(keys,
			allocationMetricKey{ownerType: allocationkernel.OwnerService, status: status, ready: false},
			allocationMetricKey{ownerType: allocationkernel.OwnerService, status: status, ready: true},
			allocationMetricKey{ownerType: allocationkernel.OwnerRun, status: status, ready: false},
		)
	}
	return keys
}

func knownString(value string, known []string) bool {
	for _, candidate := range known {
		if value == candidate {
			return true
		}
	}
	return false
}

func knownAllocationMetricKey(key allocationMetricKey, known []allocationMetricKey) bool {
	return slices.Contains(known, key)
}
