package pgnodes

import (
	"context"
	"fmt"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *PGStore) Load(ctx context.Context) ([]*nodekernel.Record, error) {
	rows, err := s.db.Pool().Query(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.admitted_at, n.last_heartbeat_at,
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
			record          nodekernel.Record
			summaryJSON     []byte
			lastHeartbeatAt *time.Time
			retiredAt       *time.Time
		)
		if err := rows.Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.AdmittedAt, &lastHeartbeatAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		if lastHeartbeatAt != nil {
			record.LastHeartbeatAt = *lastHeartbeatAt
		}
		if retiredAt != nil {
			record.RetiredAt = *retiredAt
		}
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
