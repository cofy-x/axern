package pgretention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type terminalRunRetentionRequest struct {
	cutoff    time.Time
	now       time.Time
	batchSize int
}

func (s *PGStore) deleteTerminalRuns(ctx context.Context, tx pgx.Tx, req terminalRunRetentionRequest) (int64, error) {
	runIDs, err := candidateTerminalRuns(ctx, tx, req)
	if err != nil {
		return 0, err
	}
	if len(runIDs) == 0 {
		return 0, nil
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

func candidateTerminalRuns(ctx context.Context, tx pgx.Tx, req terminalRunRetentionRequest) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT r.run_id
		FROM runs r
		JOIN allocations a ON a.run_id = r.run_id
		WHERE r.updated_at < $1
		  AND a.lifecycle_state = 'ALLOCATION_LIFECYCLE_STATE_RELEASED'
		  AND r.status IN (
			'RUN_STATUS_SUCCEEDED',
			'RUN_STATUS_FAILED',
			'RUN_STATUS_CANCELLED'
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM allocation_reconcile_queue q
			WHERE q.allocation_id = a.allocation_id
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM allocation_access_grants ag
			WHERE ag.allocation_id = a.allocation_id
			  AND ag.revoked = FALSE
			  AND ag.expires_at >= $3
		  )
		  AND NOT EXISTS (
			SELECT 1
			FROM tunnel_sessions t
			WHERE t.allocation_id = a.allocation_id
			  AND t.status IN (
				'TUNNEL_SESSION_STATUS_PENDING',
				'TUNNEL_SESSION_STATUS_RUNNING',
				'TUNNEL_SESSION_STATUS_DEGRADED'
			  )
		  )
		ORDER BY r.updated_at ASC, r.run_id ASC
		LIMIT $2
	`, req.cutoff.UTC(), req.batchSize, req.now.UTC())
	if err != nil {
		return nil, fmt.Errorf("query terminal run retention candidates: %w", err)
	}
	defer rows.Close()
	runIDs := make([]string, 0)
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return nil, fmt.Errorf("scan terminal run retention candidate: %w", err)
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate terminal run retention candidates: %w", err)
	}
	return runIDs, nil
}
