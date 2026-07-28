package pgretention

import (
	"context"
	"fmt"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/jackc/pgx/v5"
)

type serviceAllocationRetentionRequest struct {
	cutoff    time.Time
	keep      int
	batchSize int
	now       time.Time
}

func (s *PGStore) deleteServiceAllocations(ctx context.Context, tx pgx.Tx, req serviceAllocationRetentionRequest) (int64, error) {
	allocationIDs, err := candidateServiceAllocationIDs(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	return pgallocation.DeleteHistoryTx(ctx, tx, pgallocation.DeleteHistoryRequest{
		AllocationIDs: allocationIDs,
		Reason:        "delete service allocation history",
		Now:           req.now,
	})
}

func candidateServiceAllocationIDs(ctx context.Context, tx pgx.Tx, req serviceAllocationRetentionRequest) ([]string, error) {
	rows, err := tx.Query(ctx, `
		WITH ranked AS (
			SELECT a.allocation_id,
				ROW_NUMBER() OVER (PARTITION BY a.owner_id ORDER BY a.updated_at DESC, a.allocation_id DESC) AS rn
			FROM allocations a
			WHERE a.owner_type = $4
			  AND a.status IN (
				'ALLOCATION_STATUS_EXITED',
				'ALLOCATION_STATUS_FAILED',
				'ALLOCATION_STATUS_RELEASED'
			  )
		)
		SELECT r.allocation_id
		FROM ranked r
		JOIN allocations a ON a.allocation_id = r.allocation_id
		WHERE r.rn > $2
		  AND a.updated_at < $1
		  AND NOT EXISTS (
			SELECT 1
			FROM services s, jsonb_array_elements_text(s.allocation_ids) AS current_id(value)
			WHERE current_id.value = a.allocation_id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM allocation_reconcile_queue q
			WHERE q.allocation_id = a.allocation_id
		  )
		ORDER BY a.updated_at ASC, a.allocation_id ASC
		LIMIT $3
	`, req.cutoff.UTC(), req.keep, req.batchSize, allocationkernel.OwnerService)
	if err != nil {
		return nil, fmt.Errorf("query service allocation retention candidates: %w", err)
	}
	defer rows.Close()
	return scanStrings(rows, "service allocation retention candidate")
}
