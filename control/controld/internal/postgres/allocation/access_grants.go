package pgallocation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// NextAccessGrantRevision serializes delivery within one Node stream. The
// cursor and grant changes commit together; a rolled-back writer cannot advance
// a watcher past an unpublished change. Other Nodes never share this lock.
func NextAccessGrantRevision(ctx context.Context, tx pgx.Tx, nodeID string) (int64, error) {
	var revision int64
	err := tx.QueryRow(ctx, `
		INSERT INTO node_access_grant_cursors(node_id, revision) VALUES ($1, 1)
		ON CONFLICT (node_id) DO UPDATE
		SET revision = node_access_grant_cursors.revision + 1
		RETURNING revision
	`, nodeID).Scan(&revision)
	if err != nil {
		return 0, fmt.Errorf("advance node access grant cursor: %w", err)
	}
	return revision, nil
}

// RevokeAccessGrants publishes one atomic change set for the Allocation.
// Callers hold its authoritative row lock before entering this operation.
func RevokeAccessGrants(ctx context.Context, tx pgx.Tx, allocationID string) error {
	var nodeID string
	err := tx.QueryRow(ctx, `
		SELECT node_id FROM allocation_access_grants
		WHERE allocation_id = $1 AND NOT revoked LIMIT 1
	`, allocationID).Scan(&nodeID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load allocation access grant owner: %w", err)
	}
	revision, err := NextAccessGrantRevision(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE allocation_access_grants SET revoked = TRUE, revision = $2
		WHERE allocation_id = $1 AND NOT revoked
	`, allocationID, revision)
	if err != nil {
		return fmt.Errorf("revoke allocation access grants: %w", err)
	}
	return nil
}
