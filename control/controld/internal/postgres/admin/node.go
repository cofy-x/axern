package pgadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	query := `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.registered_at, n.updated_at,
		       n.retired_at, n.retired_reason, s.summary
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id`
	args := make([]any, 0, 1)
	if filter.Lifecycle != "" {
		query += " WHERE n.lifecycle_status = $1"
		args = append(args, string(filter.Lifecycle))
	}
	query += " ORDER BY n.node_id"
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list admin nodes: %w", err)
	}
	defer rows.Close()
	out := make([]*nodekernel.Record, 0)
	for rows.Next() {
		record, err := scanAdminNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *Store) RetireNode(ctx context.Context, req adminkernel.RetireNodeRequest) (*nodekernel.Record, error) {
	req = adminkernel.NormalizeRetireNodeRequest(req)
	if err := adminkernel.ValidateRetireNodeRequest(req); err != nil {
		return nil, err
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin node retirement: %w", err)
	}
	defer tx.Rollback(ctx)

	var lifecycle string
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `SELECT lifecycle_status, updated_at FROM nodes WHERE node_id = $1 FOR UPDATE`, req.NodeID).Scan(&lifecycle, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, grpcstatus.Error(codes.NotFound, "node not found")
		}
		return nil, fmt.Errorf("lock node for retirement: %w", err)
	}
	if lifecycle == string(nodekernel.LifecycleRetired) {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "node is already retired")
	}
	if nodekernel.HeartbeatFresh(updatedAt, req.Now, req.HeartbeatWindow) {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "node %q cannot be retired while its heartbeat is fresh", req.NodeID)
	}
	if err := requireNodeRetirementClear(ctx, tx, req); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE nodes
		SET lifecycle_status = 'retired', retired_at = $2, retired_reason = $3, version = version + 1
		WHERE node_id = $1
	`, req.NodeID, req.Now, req.OperatorReason); err != nil {
		return nil, fmt.Errorf("retire node: %w", err)
	}
	if err := insertAdminAuditEvent(ctx, tx, adminAuditEvent{
		EventID:        "admaudit-" + uuid.NewString(),
		Operation:      adminkernel.AuditOperationRetireNode,
		TargetType:     adminkernel.AuditTargetNode,
		TargetID:       req.NodeID,
		OperatorReason: req.OperatorReason,
		CreatedAt:      req.Now,
	}); err != nil {
		return nil, err
	}
	record, err := loadAdminNode(ctx, tx, req.NodeID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit node retirement: %w", err)
	}
	return record, nil
}

func requireNodeRetirementClear(ctx context.Context, tx pgx.Tx, req adminkernel.RetireNodeRequest) error {
	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{"active allocation(s)", `SELECT COUNT(*) FROM allocations WHERE node_id = $1 AND status NOT IN ($2, $3, $4)`, []any{req.NodeID, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(), commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String()}},
		{"active reservation(s)", `SELECT COUNT(*) FROM workload_reservations WHERE node_id = $1 AND released_at IS NULL`, []any{req.NodeID}},
		{"active execution lease(s)", `SELECT COUNT(*) FROM execution_leases WHERE node_id = $1 AND revoked = FALSE AND expires_at > $2`, []any{req.NodeID, req.Now}},
		{"active tunnel session(s)", `SELECT COUNT(*) FROM tunnel_sessions WHERE node_id = $1 AND revoked = FALSE AND status IN ($2, $3, $4)`, []any{req.NodeID, tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(), tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(), tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String()}},
		{"allocation lifecycle retry item(s)", `SELECT COUNT(*) FROM allocation_reconcile_queue q JOIN allocations a ON a.allocation_id = q.allocation_id WHERE a.node_id = $1`, []any{req.NodeID}},
	}
	for _, check := range checks {
		var count int64
		if err := tx.QueryRow(ctx, check.query, check.args...).Scan(&count); err != nil {
			return fmt.Errorf("count node retirement %s: %w", check.name, err)
		}
		if count > 0 {
			return grpcstatus.Errorf(codes.FailedPrecondition, "node %q cannot be retired while it has %d %s", req.NodeID, count, check.name)
		}
	}
	return nil
}

type adminNodeScanner interface {
	Scan(...any) error
}

func loadAdminNode(ctx context.Context, tx pgx.Tx, nodeID string) (*nodekernel.Record, error) {
	return scanAdminNode(tx.QueryRow(ctx, `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.registered_at, n.updated_at,
		       n.retired_at, n.retired_reason, s.summary
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id
		WHERE n.node_id = $1
	`, nodeID))
}

func scanAdminNode(row adminNodeScanner) (*nodekernel.Record, error) {
	var record nodekernel.Record
	var summaryJSON []byte
	var retiredAt *time.Time
	if err := row.Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.RegisteredAt, &record.UpdatedAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
		return nil, fmt.Errorf("scan admin node: %w", err)
	}
	if retiredAt != nil {
		record.RetiredAt = *retiredAt
	}
	if len(summaryJSON) > 0 {
		record.Summary = &nodev1.NodeSummary{}
		if err := protojson.Unmarshal(summaryJSON, record.Summary); err != nil {
			return nil, fmt.Errorf("unmarshal admin node summary: %w", err)
		}
	}
	record.RetiredReason = strings.TrimSpace(record.RetiredReason)
	return &record, nil
}
