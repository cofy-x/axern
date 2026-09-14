package pgrun

import (
	"context"
	"fmt"
	"strings"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
)

func (s *Store) WatchAllocationAccessGrants(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*accessgrantkernel.Record, int64, error) {
	nodeID = strings.TrimSpace(nodeID)
	subscription, err := s.accessGrantWatches.subscribe(ctx, nodeID)
	if err != nil {
		return nil, afterRevision, fmt.Errorf("subscribe allocation access grant changes: %w", err)
	}
	defer subscription.close()
	for {
		grants, current, err := s.loadAllocationAccessGrants(ctx, nodeID, afterRevision, now)
		if err != nil {
			return nil, afterRevision, err
		}
		if current > afterRevision {
			return grants, current, nil
		}
		if err := subscription.wait(ctx); err != nil {
			return nil, afterRevision, err
		}
	}
}

func (s *Store) loadAllocationAccessGrants(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*accessgrantkernel.Record, int64, error) {
	var current int64
	if err := s.db.Pool().QueryRow(ctx, `SELECT revision FROM control_revisions WHERE name = $1`, accessGrantRevisionName).Scan(&current); err != nil {
		return nil, 0, fmt.Errorf("load allocation access grant revision: %w", err)
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT grant_id, allocation_id, node_id, expires_at, revision, revoked, token_hash
		FROM allocation_access_grants
		WHERE node_id = $1 AND revision > $2 AND revision <= $3
		ORDER BY revision ASC, grant_id ASC
	`, nodeID, afterRevision, current)
	if err != nil {
		return nil, 0, fmt.Errorf("query allocation access grants: %w", err)
	}
	defer rows.Close()
	grants := make([]*accessgrantkernel.Record, 0)
	for rows.Next() {
		grant := &accessgrantkernel.Record{}
		if err := rows.Scan(&grant.GrantID, &grant.AllocationID, &grant.NodeID, &grant.ExpiresAt, &grant.Revision, &grant.Revoked, &grant.ValidationTokenHash); err != nil {
			return nil, 0, err
		}
		if accessgrantkernel.IsExpired(grant, now) {
			grant.Revoked = true
		}
		grants = append(grants, grant)
	}
	return grants, current, rows.Err()
}
