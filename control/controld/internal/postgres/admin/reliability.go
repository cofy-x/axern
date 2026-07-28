package pgadmin

import (
	"context"
	"fmt"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	pgconsistency "github.com/cofy-x/axern/control/controld/internal/postgres/consistency"
)

func (s *Store) ConsistencySnapshot(ctx context.Context, now time.Time) (consistencykernel.Snapshot, error) {
	return pgconsistency.Snapshot(ctx, s.db.Pool(), now)
}

func (s *Store) CountAllocationLifecycleRetries(ctx context.Context, now time.Time) (adminkernel.AllocationLifecycleRetryCounts, error) {
	var counts adminkernel.AllocationLifecycleRetryCounts
	if err := s.db.Pool().QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE next_run_at <= $1)
		FROM allocation_reconcile_queue
	`, now.UTC()).Scan(&counts.Total, &counts.Due); err != nil {
		return adminkernel.AllocationLifecycleRetryCounts{}, fmt.Errorf("count allocation lifecycle retries: %w", err)
	}
	return counts, nil
}
