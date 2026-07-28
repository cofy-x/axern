package pgretention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PGStore) deleteFunctionEvents(ctx context.Context, tx pgx.Tx, cutoff time.Time, keep, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT event_id,
				ROW_NUMBER() OVER (PARTITION BY function_id ORDER BY created_at DESC, event_id DESC) AS rn
			FROM function_events
		), candidates AS (
			SELECT event_id
			FROM ranked
			WHERE rn > $2
			  AND event_id IN (
				SELECT event_id FROM function_events WHERE created_at < $1
			  )
			ORDER BY event_id ASC
			LIMIT $3
		)
		DELETE FROM function_events
		WHERE event_id IN (SELECT event_id FROM candidates)
	`, cutoff.UTC(), keep, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete function events: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *PGStore) deleteFunctionInvocations(ctx context.Context, tx pgx.Tx, cutoff time.Time, keep, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT invocation_id,
				ROW_NUMBER() OVER (PARTITION BY function_id ORDER BY created_at DESC, invocation_id DESC) AS rn
			FROM function_invocations
			WHERE status NOT IN ('FUNCTION_INVOCATION_STATUS_ACCEPTED', 'FUNCTION_INVOCATION_STATUS_QUEUED', 'FUNCTION_INVOCATION_STATUS_RUNNING')
		), candidates AS (
			SELECT invocation_id
			FROM ranked
			WHERE rn > $2
			  AND invocation_id IN (
				SELECT invocation_id FROM function_invocations WHERE created_at < $1
			  )
			ORDER BY invocation_id ASC
			LIMIT $3
		)
		DELETE FROM function_invocations
		WHERE invocation_id IN (SELECT invocation_id FROM candidates)
	`, cutoff.UTC(), keep, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete function invocations: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *PGStore) deleteFunctionIdempotencyRecords(ctx context.Context, tx pgx.Tx, cutoff time.Time, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		DELETE FROM function_idempotency_records
		WHERE ctid IN (
			SELECT ctid FROM function_idempotency_records
			WHERE created_at < $1
			ORDER BY created_at ASC
			LIMIT $2
		)
	`, cutoff.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete function idempotency records: %w", err)
	}
	return tag.RowsAffected(), nil
}
