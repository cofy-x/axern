package pgservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func (s *PGStore) activeServiceInventoryExpectations(ctx context.Context, nodeID string) ([]allocationkernel.NodeInventoryExpectation, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT a.allocation_id, a.attempt, a.node_active_at
		FROM allocations a
		WHERE a.owner_type = $1 AND a.node_id = $2 AND a.status IN ($3, $4)
		ORDER BY a.created_at ASC, a.allocation_id ASC
	`, allocationOwnerService, strings.TrimSpace(nodeID), commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String())
	if err != nil {
		return nil, fmt.Errorf("query active service inventory expectations: %w", err)
	}
	defer rows.Close()
	out := make([]allocationkernel.NodeInventoryExpectation, 0)
	for rows.Next() {
		var record allocationkernel.NodeInventoryExpectation
		var nodeActiveAt *time.Time
		if err := rows.Scan(&record.AllocationID, &record.Attempt, &nodeActiveAt); err != nil {
			return nil, fmt.Errorf("scan active service inventory expectation: %w", err)
		}
		if nodeActiveAt != nil {
			record.NodeActiveAt = *nodeActiveAt
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active service inventory expectations: %w", err)
	}
	return out, nil
}

func (s *PGStore) unavailableServiceAllocations(ctx context.Context, nodeID string) ([]allocationkernel.NodeAllocationRef, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT a.allocation_id, a.attempt
		FROM allocations a
		WHERE a.owner_type = $1 AND a.node_id = $2 AND a.status IN ($3, $4, $5, $6)
		ORDER BY a.created_at ASC, a.allocation_id ASC
	`, allocationOwnerService, strings.TrimSpace(nodeID),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String())
	if err != nil {
		return nil, fmt.Errorf("query unavailable-node service allocations: %w", err)
	}
	defer rows.Close()
	out := make([]allocationkernel.NodeAllocationRef, 0)
	for rows.Next() {
		var record allocationkernel.NodeAllocationRef
		if err := rows.Scan(&record.AllocationID, &record.Attempt); err != nil {
			return nil, fmt.Errorf("scan unavailable-node service allocation: %w", err)
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unavailable-node service allocations: %w", err)
	}
	return out, nil
}
