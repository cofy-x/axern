package pgnodes

import (
	"context"
	"fmt"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

func loadNodeRecord(ctx context.Context, tx pgx.Tx, nodeID string) (*nodekernel.Record, error) {
	var (
		record      nodekernel.Record
		summaryJSON []byte
		retiredAt   *time.Time
	)
	if err := tx.QueryRow(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.registered_at, n.updated_at,
		       n.retired_at, n.retired_reason, s.summary
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id
		WHERE n.node_id = $1
	`, nodeID).Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.RegisteredAt, &record.UpdatedAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
		return nil, fmt.Errorf("load node record: %w", err)
	}
	if retiredAt != nil {
		record.RetiredAt = *retiredAt
	}

	rows, err := tx.Query(ctx, `SELECT runtime_name FROM node_runtime_sets WHERE node_id = $1 ORDER BY runtime_name`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load node runtimes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var runtimeName string
		if err := rows.Scan(&runtimeName); err != nil {
			return nil, fmt.Errorf("scan node runtime: %w", err)
		}
		record.Runtimes = append(record.Runtimes, runtimeName)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node runtimes: %w", err)
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
		NodeID:        in.NodeID,
		NodeTarget:    in.NodeTarget,
		Runtimes:      append([]string(nil), in.Runtimes...),
		Summary:       nodekernel.CloneNodeSummary(in.Summary),
		Lifecycle:     in.Lifecycle,
		RegisteredAt:  in.RegisteredAt,
		UpdatedAt:     in.UpdatedAt,
		RetiredAt:     in.RetiredAt,
		RetiredReason: in.RetiredReason,
	}
}
