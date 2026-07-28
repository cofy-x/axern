package pgretention

import (
	"context"
	"fmt"
	"time"

	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	"github.com/jackc/pgx/v5"
)

type terminalRunRetentionRequest struct {
	cutoff    time.Time
	batchSize int
	now       time.Time
}

func (s *PGStore) deleteTerminalRuns(ctx context.Context, tx pgx.Tx, req terminalRunRetentionRequest) (int64, error) {
	runIDs, allocationIDs, err := candidateTerminalRuns(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	if len(runIDs) == 0 {
		return 0, nil
	}
	if _, err := pgallocation.DeleteHistoryTx(ctx, tx, pgallocation.DeleteHistoryRequest{
		AllocationIDs: allocationIDs,
		Reason:        "delete terminal run allocation history",
		Now:           req.now,
	}); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `
		DELETE FROM runs
		WHERE run_id = ANY($1)
	`, runIDs)
	if err != nil {
		return 0, fmt.Errorf("delete terminal runs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func candidateTerminalRuns(ctx context.Context, tx pgx.Tx, req terminalRunRetentionRequest) ([]string, []string, error) {
	rows, err := tx.Query(ctx, `
		SELECT run_id, allocation_id
		FROM runs
		WHERE updated_at < $1
		  AND status IN (
			'RUN_STATUS_SUCCEEDED',
			'RUN_STATUS_FAILED',
			'RUN_STATUS_CANCELLED'
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM allocation_reconcile_queue q
			WHERE q.allocation_id = runs.allocation_id
		  )
		ORDER BY updated_at ASC, run_id ASC
		LIMIT $2
	`, req.cutoff.UTC(), req.batchSize)
	if err != nil {
		return nil, nil, fmt.Errorf("query terminal run retention candidates: %w", err)
	}
	defer rows.Close()
	runIDs := make([]string, 0)
	allocationIDs := make([]string, 0)
	for rows.Next() {
		var runID, allocationID string
		if err := rows.Scan(&runID, &allocationID); err != nil {
			return nil, nil, fmt.Errorf("scan terminal run retention candidate: %w", err)
		}
		runIDs = append(runIDs, runID)
		allocationIDs = append(allocationIDs, allocationID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate terminal run retention candidates: %w", err)
	}
	return runIDs, allocationIDs, nil
}
