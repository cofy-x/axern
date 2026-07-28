package pgservice

import (
	"context"
	"fmt"
	"strings"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"github.com/jackc/pgx/v5"
)

func (s *PGStore) listAllocationRecordsTx(ctx context.Context, tx pgx.Tx, serviceID string, allocationIDs []string) ([]*servicekernel.AllocationRecord, error) {
	if len(allocationIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT allocation_id, environment_id, node_id, ''::text AS node_target, attempt, status, ready, readiness_message, readiness_probe, liveness_probe, config
		FROM allocations
		WHERE owner_type = $1 AND owner_id = $2 AND allocation_id = ANY($3::text[])
	`, allocationOwnerService, strings.TrimSpace(serviceID), allocationIDs)
	if err != nil {
		return nil, fmt.Errorf("query service allocation records: %w", err)
	}
	defer rows.Close()
	out := make([]*servicekernel.AllocationRecord, 0)
	for rows.Next() {
		record, err := scanAllocationRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service allocation records: %w", err)
	}
	return out, nil
}
