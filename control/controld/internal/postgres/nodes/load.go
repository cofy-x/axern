package pgnodes

import (
	"context"
	"fmt"
	"sort"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *PGStore) Load(ctx context.Context) ([]*nodekernel.Record, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.registered_at, n.updated_at,
		       n.retired_at, n.retired_reason, s.summary
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id
		ORDER BY n.node_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	records := make([]*nodekernel.Record, 0)
	for rows.Next() {
		var (
			record      nodekernel.Record
			summaryJSON []byte
			retiredAt   *time.Time
		)
		if err := rows.Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.RegisteredAt, &record.UpdatedAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		if retiredAt != nil {
			record.RetiredAt = *retiredAt
		}
		runtimes, err := s.loadRuntimes(ctx, record.NodeID)
		if err != nil {
			return nil, err
		}
		record.Runtimes = runtimes
		if len(summaryJSON) > 0 {
			record.Summary = &nodev1.NodeSummary{}
			if err := protojson.Unmarshal(summaryJSON, record.Summary); err != nil {
				return nil, fmt.Errorf("unmarshal node summary: %w", err)
			}
		}
		records = append(records, cloneRecord(&record))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return records, nil
}

func (s *PGStore) loadRuntimes(ctx context.Context, nodeID string) ([]string, error) {
	rows, err := s.db.Pool().Query(ctx, `SELECT runtime_name FROM node_runtime_sets WHERE node_id = $1 ORDER BY runtime_name`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query node runtimes: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var runtimeName string
		if err := rows.Scan(&runtimeName); err != nil {
			return nil, fmt.Errorf("scan node runtime: %w", err)
		}
		out = append(out, runtimeName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node runtimes: %w", err)
	}
	sort.Strings(out)
	return out, nil
}
