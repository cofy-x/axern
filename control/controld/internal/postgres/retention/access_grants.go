package pgretention

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *PGStore) deleteExpiredAccessGrants(ctx context.Context, tx pgx.Tx, cutoff, now time.Time, batchSize int) (int64, error) {
	tag, err := tx.Exec(ctx, `
		WITH candidates AS (
			SELECT grant_id
			FROM allocation_access_grants
			WHERE created_at < $1
			  AND (revoked = TRUE OR expires_at < $2)
			ORDER BY created_at ASC, grant_id ASC
			LIMIT $3
		)
		DELETE FROM allocation_access_grants
		WHERE grant_id IN (SELECT grant_id FROM candidates)
	`, cutoff.UTC(), now.UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete expired allocation access grants: %w", err)
	}
	return tag.RowsAffected(), nil
}
