package pgnodes

import (
	"context"
	"fmt"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func loadNodeRecord(ctx context.Context, tx pgx.Tx, nodeID string) (*nodekernel.Record, error) {
	var (
		record          nodekernel.Record
		summaryJSON     []byte
		lastHeartbeatAt *time.Time
		retiredAt       *time.Time
	)
	if err := tx.QueryRow(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.admitted_at, n.last_heartbeat_at,
		       n.retired_at, n.retired_reason, s.summary
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id
		WHERE n.node_id = $1
	`, nodeID).Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.AdmittedAt, &lastHeartbeatAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
		return nil, fmt.Errorf("load node record: %w", err)
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
	return cloneRecord(&record), nil
}

func cloneRecord(in *nodekernel.Record) *nodekernel.Record {
	if in == nil {
		return nil
	}
	return &nodekernel.Record{
		NodeID:          in.NodeID,
		NodeTarget:      in.NodeTarget,
		Summary:         nodekernel.CloneNodeSummary(in.Summary),
		Lifecycle:       in.Lifecycle,
		AdmittedAt:      in.AdmittedAt,
		LastHeartbeatAt: in.LastHeartbeatAt,
		RetiredAt:       in.RetiredAt,
		RetiredReason:   in.RetiredReason,
	}
}
