package pgrun

import (
	"context"
	"fmt"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

// ListNodeExecutionAllocationIDs returns the complete desired execution set
// for one bound node. The authenticated ReportNode response turns this durable
// Allocation fact into a short node-local lease; no parallel lease entity is
// needed for runtime liveness.
func (s *Store) ListNodeExecutionAllocationIDs(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT a.allocation_id
		FROM allocations a
		JOIN runs r ON r.run_id = a.run_id
		WHERE a.node_id = $1
		  AND a.lifecycle_state IN ($2, $3, $4)
		  AND r.status NOT IN ($5, $6, $7)
		ORDER BY a.allocation_id
	`, strings.TrimSpace(nodeID),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(),
		runv1.RunStatus_RUN_STATUS_SUCCEEDED.String(),
		runv1.RunStatus_RUN_STATUS_FAILED.String(),
		runv1.RunStatus_RUN_STATUS_CANCELLED.String())
	if err != nil {
		return nil, fmt.Errorf("list node execution allocations: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var allocationID string
		if err := rows.Scan(&allocationID); err != nil {
			return nil, err
		}
		ids = append(ids, allocationID)
	}
	return ids, rows.Err()
}
