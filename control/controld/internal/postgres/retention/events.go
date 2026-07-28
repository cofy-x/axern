package pgretention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PGStore) deleteServiceEvents(ctx context.Context, tx pgx.Tx, cutoff time.Time, keep, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT event_id,
				ROW_NUMBER() OVER (PARTITION BY service_id ORDER BY created_at DESC, event_id DESC) AS rn
			FROM service_events
		), candidates AS (
			SELECT event_id
			FROM ranked
			WHERE rn > $2
			  AND event_id IN (
				SELECT event_id FROM service_events WHERE created_at < $1
			  )
			ORDER BY event_id ASC
			LIMIT $3
		)
		DELETE FROM service_events
		WHERE event_id IN (SELECT event_id FROM candidates)
	`, cutoff.UTC(), keep, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete service events: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *PGStore) deleteTunnelEvents(ctx context.Context, tx pgx.Tx, cutoff time.Time, keep, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH ranked AS (
			SELECT event_id,
				ROW_NUMBER() OVER (PARTITION BY session_id ORDER BY created_at DESC, event_id DESC) AS rn
			FROM tunnel_session_events
		), candidates AS (
			SELECT event_id
			FROM ranked
			WHERE rn > $2
			  AND event_id IN (
				SELECT event_id FROM tunnel_session_events WHERE created_at < $1
			  )
			ORDER BY event_id ASC
			LIMIT $3
		)
		DELETE FROM tunnel_session_events
		WHERE event_id IN (SELECT event_id FROM candidates)
	`, cutoff.UTC(), keep, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete tunnel session events: %w", err)
	}
	return tag.RowsAffected(), nil
}
