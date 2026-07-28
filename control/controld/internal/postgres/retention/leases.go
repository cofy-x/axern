package pgretention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PGStore) deleteExpiredLeases(ctx context.Context, tx pgx.Tx, cutoff, now time.Time, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH candidates AS (
			SELECT lease_id
			FROM execution_leases
			WHERE created_at < $1
			  AND (revoked = TRUE OR expires_at < $2)
			ORDER BY created_at ASC, lease_id ASC
			LIMIT $3
		)
		DELETE FROM execution_leases
		WHERE lease_id IN (SELECT lease_id FROM candidates)
	`, cutoff.UTC(), now.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete expired execution leases: %w", err)
	}
	return tag.RowsAffected(), nil
}
