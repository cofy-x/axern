package pgservice

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func nextLeaseRevision(ctx context.Context, tx pgx.Tx) (int64, error) {
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE control_revisions
		SET revision = revision + 1
		WHERE name = $1
		RETURNING revision
	`, leaseRevisionName).Scan(&revision); err != nil {
		return 0, fmt.Errorf("advance lease revision: %w", err)
	}
	return revision, nil
}

func (s *PGStore) revokeAllocationLeases(ctx context.Context, tx pgx.Tx, allocationIDs []string) error {
	for _, allocationID := range allocationIDs {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			continue
		}
		revision, err := nextLeaseRevision(ctx, tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE execution_leases
			SET revoked = true, revision = $2
			WHERE allocation_id = $1 AND revoked = false
		`, allocationID, revision); err != nil {
			return fmt.Errorf("revoke allocation leases: %w", err)
		}
	}
	return nil
}
