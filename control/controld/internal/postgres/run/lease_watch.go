package pgrun

import (
	"context"
	"fmt"
	"strings"
	"time"

	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func (s *Store) WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	nodeID = strings.TrimSpace(nodeID)
	subscription, err := s.leaseWatches.subscribe(ctx, nodeID)
	if err != nil {
		return nil, afterRevision, fmt.Errorf("subscribe execution lease changes: %w", err)
	}
	defer subscription.close()
	for {
		leases, current, err := s.loadExecutionLeases(ctx, nodeID, afterRevision, now)
		if err != nil {
			return nil, afterRevision, err
		}
		if current > afterRevision {
			return leases, current, nil
		}
		if err := subscription.wait(ctx); err != nil {
			return nil, afterRevision, err
		}
	}
}

func (s *Store) loadExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	// Fix the high-water mark before reading rows. A lease committed after this
	// query is intentionally left for the next response instead of being skipped
	// by an advanced cursor.
	var current int64
	if err := s.db.Pool().QueryRow(ctx, `SELECT revision FROM control_revisions WHERE name = $1`, leaseRevisionName).Scan(&current); err != nil {
		return nil, 0, fmt.Errorf("load lease revision: %w", err)
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT lease_id, allocation_id, node_id, node_target, attempt, lease_type,
			expires_at, revision, revoked, token_hash
		FROM execution_leases
		WHERE node_id = $1 AND revision > $2 AND revision <= $3
		ORDER BY revision ASC, lease_id ASC
	`, nodeID, afterRevision, current)
	if err != nil {
		return nil, 0, fmt.Errorf("query execution leases: %w", err)
	}
	defer rows.Close()
	leases := make([]*commonv1.ExecutionLease, 0)
	for rows.Next() {
		lease, err := scanLease(rows)
		if err != nil {
			return nil, 0, err
		}
		if leasekernel.IsExpired(lease, now) {
			lease.Revoked = true
		}
		leases = append(leases, lease)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return leases, current, nil
}
