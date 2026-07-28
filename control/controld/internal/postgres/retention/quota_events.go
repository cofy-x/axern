package pgretention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PGStore) deleteQuotaEvents(ctx context.Context, tx pgx.Tx, cutoff time.Time, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH doomed AS (
			SELECT event_id
			FROM namespace_quota_events
			WHERE created_at < $1
			ORDER BY created_at ASC, event_id ASC
			LIMIT $2
		)
		DELETE FROM namespace_quota_events e
		USING doomed
		WHERE e.event_id = doomed.event_id
	`, cutoff.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete namespace quota events: %w", err)
	}
	return tag.RowsAffected(), nil
}
