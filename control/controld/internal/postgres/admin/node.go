package pgadmin

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) AdmitNode(ctx context.Context, req adminkernel.AdmitNodeRequest) (*nodekernel.Record, error) {
	req = adminkernel.NormalizeAdmitNodeRequest(req)
	if err := adminkernel.ValidateAdmitNodeRequest(req); err != nil {
		return nil, err
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin node admission: %w", err)
	}
	defer tx.Rollback(ctx)

	credentialHash := sha256.Sum256([]byte(req.NodeCredential))
	command, err := tx.Exec(ctx, `
		INSERT INTO nodes (
			node_id, node_target, node_credential_hash, admitted_at,
			last_heartbeat_at, lifecycle_status
		) VALUES ($1, '', $2, $3, NULL, 'active')
		ON CONFLICT (node_id) DO NOTHING
	`, req.NodeID, hex.EncodeToString(credentialHash[:]), req.Now)
	if err != nil {
		return nil, fmt.Errorf("admit node: %w", err)
	}
	if command.RowsAffected() == 0 {
		return nil, grpcstatus.Error(codes.AlreadyExists, "node identity already exists")
	}
	if err := insertAdminAuditEvent(ctx, tx, adminAuditEvent{
		EventID: "admaudit-" + uuid.NewString(), Operation: adminkernel.AuditOperationAdmitNode,
		TargetType: adminkernel.AuditTargetNode, TargetID: req.NodeID,
		OperatorReason: req.OperatorReason, CreatedAt: req.Now,
	}); err != nil {
		return nil, err
	}
	record, err := loadAdminNode(ctx, tx, req.NodeID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit node admission: %w", err)
	}
	return record, nil
}

// BootstrapNode creates the initial node identity before controld starts. It is
// idempotent only when the existing identity has the same credential and is
// still active; bootstrap never rotates or revives an identity.
func (s *Store) BootstrapNode(ctx context.Context, req adminkernel.AdmitNodeRequest) error {
	req = adminkernel.NormalizeAdmitNodeRequest(req)
	if err := adminkernel.ValidateAdmitNodeRequest(req); err != nil {
		return err
	}
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin node bootstrap: %w", err)
	}
	defer tx.Rollback(ctx)

	credentialHash := sha256.Sum256([]byte(req.NodeCredential))
	wantHash := hex.EncodeToString(credentialHash[:])
	command, err := tx.Exec(ctx, `
		INSERT INTO nodes (
			node_id, node_target, node_credential_hash, admitted_at,
			last_heartbeat_at, lifecycle_status
		) VALUES ($1, '', $2, $3, NULL, 'active')
		ON CONFLICT (node_id) DO NOTHING
	`, req.NodeID, wantHash, req.Now)
	if err != nil {
		return fmt.Errorf("bootstrap node: %w", err)
	}
	if command.RowsAffected() == 0 {
		var existingHash, lifecycle string
		if err := tx.QueryRow(ctx, `SELECT node_credential_hash, lifecycle_status FROM nodes WHERE node_id = $1`, req.NodeID).Scan(&existingHash, &lifecycle); err != nil {
			return fmt.Errorf("load bootstrapped node: %w", err)
		}
		if subtle.ConstantTimeCompare([]byte(existingHash), []byte(wantHash)) != 1 || lifecycle != string(nodekernel.LifecycleActive) {
			return grpcstatus.Error(codes.FailedPrecondition, "existing node identity does not match bootstrap credential or lifecycle")
		}
		return tx.Commit(ctx)
	}
	if err := insertAdminAuditEvent(ctx, tx, adminAuditEvent{
		EventID: "admaudit-" + uuid.NewString(), Operation: adminkernel.AuditOperationAdmitNode,
		TargetType: adminkernel.AuditTargetNode, TargetID: req.NodeID,
		OperatorReason: req.OperatorReason, CreatedAt: req.Now,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node bootstrap: %w", err)
	}
	return nil
}

func (s *Store) ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	query := `
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.admitted_at, n.last_heartbeat_at,
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
	var updatedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT lifecycle_status, last_heartbeat_at FROM nodes WHERE node_id = $1 FOR UPDATE`, req.NodeID).Scan(&lifecycle, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, grpcstatus.Error(codes.NotFound, "node not found")
		}
		return nil, fmt.Errorf("lock node for retirement: %w", err)
	}
	if lifecycle == string(nodekernel.LifecycleRetired) {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "node is already retired")
	}
	if updatedAt != nil && nodekernel.HeartbeatFresh(*updatedAt, req.Now, req.HeartbeatWindow) {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "node %q cannot be retired while its heartbeat is fresh", req.NodeID)
	}
	if err := requireNodeRetirementClear(ctx, tx, req); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE nodes
		SET lifecycle_status = 'retired', retired_at = $2, retired_reason = $3
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
		{"unreleased allocation(s)", `SELECT COUNT(*) FROM allocations WHERE node_id = $1 AND lifecycle_state <> $2`, []any{req.NodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String()}},
		{"active allocation access grant(s)", `SELECT COUNT(*) FROM allocation_access_grants WHERE node_id = $1 AND revoked = FALSE AND expires_at > $2`, []any{req.NodeID, req.Now}},
		{"active tunnel session(s)", `SELECT COUNT(*) FROM tunnel_sessions t JOIN allocations a ON a.allocation_id = t.allocation_id WHERE a.node_id = $1 AND t.status IN ($2, $3, $4)`, []any{req.NodeID, tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_PENDING.String(), tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING.String(), tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_DEGRADED.String()}},
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
		SELECT n.node_id, n.node_target, n.lifecycle_status, n.admitted_at, n.last_heartbeat_at,
		       n.retired_at, n.retired_reason, s.summary
		FROM nodes n
		LEFT JOIN node_summaries s ON s.node_id = n.node_id
		WHERE n.node_id = $1
	`, nodeID))
}

func scanAdminNode(row adminNodeScanner) (*nodekernel.Record, error) {
	var record nodekernel.Record
	var summaryJSON []byte
	var lastHeartbeatAt *time.Time
	var retiredAt *time.Time
	if err := row.Scan(&record.NodeID, &record.NodeTarget, &record.Lifecycle, &record.AdmittedAt, &lastHeartbeatAt, &retiredAt, &record.RetiredReason, &summaryJSON); err != nil {
		return nil, fmt.Errorf("scan admin node: %w", err)
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
			return nil, fmt.Errorf("unmarshal admin node summary: %w", err)
		}
	}
	record.RetiredReason = strings.TrimSpace(record.RetiredReason)
	return &record, nil
}
