package pgservice

import (
	"context"
	"fmt"
	"strings"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func (s *PGStore) CurrentServiceAllocations(ctx context.Context, serviceID string) ([]*servicekernel.AllocationRecord, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT `+allocationRecordSelectColumnsWithServiceID+`
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.owner_type = $1 AND a.owner_id = $2 AND a.status NOT IN ($3, $4, $5)
		ORDER BY a.created_at ASC, a.allocation_id ASC
	`, allocationOwnerService, strings.TrimSpace(serviceID), commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED.String())
	if err != nil {
		return nil, fmt.Errorf("query current service allocations: %w", err)
	}
	defer rows.Close()
	out := make([]*servicekernel.AllocationRecord, 0)
	for rows.Next() {
		_, record, err := scanAllocationRecordWithServiceID(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *PGStore) ServiceAllocationHistory(ctx context.Context, serviceID string) ([]*servicekernel.AllocationRecord, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT `+allocationRecordSelectColumnsWithServiceID+`
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.owner_type = $1 AND a.owner_id = $2
		ORDER BY a.created_at ASC, a.allocation_id ASC
	`, allocationOwnerService, strings.TrimSpace(serviceID))
	if err != nil {
		return nil, fmt.Errorf("query service allocation history: %w", err)
	}
	defer rows.Close()
	out := make([]*servicekernel.AllocationRecord, 0)
	for rows.Next() {
		_, record, err := scanAllocationRecordWithServiceID(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
