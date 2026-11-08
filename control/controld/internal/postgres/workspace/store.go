package pgworkspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	"github.com/cofy-x/axern/control/controld/internal/kernel/pagecursor"
	workspacekernel "github.com/cofy-x/axern/control/controld/internal/kernel/workspace"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgaccess "github.com/cofy-x/axern/control/controld/internal/postgres/access"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Store struct {
	db                             *postgres.DB
	heartbeatWindow, summaryWindow time.Duration
}

func NewStore(db *postgres.DB, heartbeatWindow, summaryWindow time.Duration) *Store {
	return &Store{db: db, heartbeatWindow: heartbeatWindow, summaryWindow: summaryWindow}
}

const selectSQL = `SELECT w.workspace_id,w.namespace,w.node_id,w.status,w.capacity_bytes,w.inode_limit,
	w.generation,COALESCE(w.attached_allocation_id,''),w.used_bytes,w.used_inodes,w.message,w.labels,w.created_at,w.updated_at,
	w.storage_root_identity,w.storage_identity,n.node_target FROM workspaces w JOIN nodes n ON n.node_id=w.node_id`

type scanner interface{ Scan(...any) error }

func scan(row scanner) (*workspacekernel.Record, error) {
	r := &workspacekernel.Record{Workspace: &workspacev1.Workspace{}}
	w := r.Workspace
	var statusText string
	var labels []byte
	var created, updated time.Time
	if err := row.Scan(&w.ID, &w.Namespace, &w.NodeID, &statusText, &w.CapacityBytes, &w.InodeLimit, &w.Generation, &w.AttachedAllocationID, &w.UsedBytes, &w.UsedInodes, &w.Message, &labels, &created, &updated, &r.StorageRootIdentity, &r.StorageIdentity, &r.NodeTarget); err != nil {
		return nil, err
	}
	w.Status = workspacev1.WorkspaceStatus(workspacev1.WorkspaceStatus_value[statusText])
	if err := json.Unmarshal(labels, &w.Labels); err != nil {
		return nil, err
	}
	w.CreatedAt = timestamppb.New(created)
	w.UpdatedAt = timestamppb.New(updated)
	return r, nil
}

func (s *Store) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CreateWorkspace(ctx context.Context, req *workspacev1.CreateWorkspaceRequest, now time.Time) (*workspacev1.Workspace, error) {
	if req.GetCapacityBytes() <= 0 || req.GetCapacityBytes()%512 != 0 || req.GetInodeLimit() <= 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "positive 512-byte-aligned Workspace capacity_bytes and positive inode_limit are required")
	}
	namespace := strings.TrimSpace(req.GetNamespace())
	if namespace == "" {
		namespace = "default"
	}
	if len(namespace) > 4096 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "Workspace namespace must not exceed 4096 UTF-8 bytes")
	}
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	started := time.Now()
	var out *workspacev1.Workspace
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := pgnamespace.LockQuotaRows(ctx, tx, namespace); err != nil {
			return err
		}
		var byteLimit, inodeLimit sql.NullInt64
		if err := tx.QueryRow(ctx, `SELECT persistent_storage_bytes_limit,persistent_storage_inodes_limit FROM namespace_resource_quotas WHERE namespace=$1`, namespace).Scan(&byteLimit, &inodeLimit); err != nil {
			return err
		}
		var usedBytes, usedInodes int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(capacity_bytes),0),COALESCE(SUM(inode_limit),0) FROM workspaces WHERE namespace=$1 AND status<>'WORKSPACE_STATUS_DELETED'`, namespace).Scan(&usedBytes, &usedInodes); err != nil {
			return err
		}
		if usedBytes > math.MaxInt64-req.GetCapacityBytes() || usedInodes > math.MaxInt64-req.GetInodeLimit() {
			return grpcstatus.Error(codes.ResourceExhausted, "namespace persistent Workspace accounting capacity exhausted")
		}
		if byteLimit.Valid && (req.GetCapacityBytes() > byteLimit.Int64 || usedBytes > byteLimit.Int64-req.GetCapacityBytes()) || inodeLimit.Valid && (req.GetInodeLimit() > inodeLimit.Int64 || usedInodes > inodeLimit.Int64-req.GetInodeLimit()) {
			return grpcstatus.Error(codes.ResourceExhausted, "namespace persistent Workspace quota exceeded")
		}
		var lifecycle, target string
		var heartbeat *time.Time
		var summaryJSON []byte
		err := tx.QueryRow(ctx, `SELECT n.lifecycle_status,n.node_target,n.last_heartbeat_at,s.summary FROM nodes n LEFT JOIN node_summaries s ON s.node_id=n.node_id WHERE n.node_id=$1 FOR UPDATE OF n`, nodeID).Scan(&lifecycle, &target, &heartbeat, &summaryJSON)
		if errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Error(codes.NotFound, "Node not found")
		}
		if err != nil {
			return err
		}
		summary := &nodev1.NodeSummary{}
		if err := protojson.Unmarshal(summaryJSON, summary); err != nil {
			return grpcstatus.Error(codes.FailedPrecondition, "Node has no valid storage observation")
		}
		current := now.Add(time.Since(started))
		storage := summary.GetWorkspaceStorage()
		if lifecycle != "active" || target == "" || heartbeat == nil || current.Sub(*heartbeat) > s.heartbeatWindow || summary.GetCollectedAt() == nil || current.Sub(summary.GetCollectedAt().AsTime()) > s.summaryWindow || summary.GetNodeState() != nodev1.NodeState_NODE_STATE_READY || !storage.GetReady() || storage.GetStorageIdentity() == "" || storage.GetCapacityBytes() <= 0 || storage.GetInodeCapacity() <= 0 {
			return grpcstatus.Error(codes.FailedPrecondition, "Node persistent Workspace storage is unavailable")
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(capacity_bytes),0),COALESCE(SUM(inode_limit),0) FROM workspaces WHERE node_id=$1 AND status<>'WORKSPACE_STATUS_DELETED'`, nodeID).Scan(&usedBytes, &usedInodes); err != nil {
			return err
		}
		if req.GetCapacityBytes() > storage.GetCapacityBytes() || usedBytes > storage.GetCapacityBytes()-req.GetCapacityBytes() || req.GetInodeLimit() > storage.GetInodeCapacity() || usedInodes > storage.GetInodeCapacity()-req.GetInodeLimit() {
			return grpcstatus.Error(codes.ResourceExhausted, "Node persistent Workspace capacity exhausted")
		}
		if err := pgaccess.AuthorizeTransaction(ctx, tx, accesskernel.ActionResourceWrite, namespace, now.Add(time.Since(started))); err != nil {
			return err
		}
		labels, err := json.Marshal(req.GetLabels())
		if err != nil {
			return err
		}
		if string(labels) == "null" {
			labels = []byte("{}")
		}
		id := "ws-" + uuid.NewString()
		if _, err := tx.Exec(ctx, `INSERT INTO workspaces(workspace_id,namespace,node_id,status,capacity_bytes,inode_limit,storage_root_identity,labels,created_at,updated_at,next_reconcile_at) VALUES($1,$2,$3,'WORKSPACE_STATUS_PROVISIONING',$4,$5,$6,$7::jsonb,$8,$8,$8)`, id, namespace, nodeID, req.GetCapacityBytes(), req.GetInodeLimit(), storage.GetStorageIdentity(), labels, current); err != nil {
			return err
		}
		r, err := scan(tx.QueryRow(ctx, selectSQL+` WHERE w.workspace_id=$1`, id))
		if err != nil {
			return err
		}
		out = r.Workspace
		return nil
	})
	return out, err
}

func (s *Store) GetWorkspace(ctx context.Context, id string) (*workspacev1.Workspace, error) {
	r, err := scan(s.db.Pool().QueryRow(ctx, selectSQL+` WHERE w.workspace_id=$1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "Workspace not found")
	}
	if err != nil {
		return nil, err
	}
	return r.Workspace, nil
}

func (s *Store) ListWorkspaces(ctx context.Context, req *workspacev1.ListWorkspacesRequest) ([]*workspacev1.Workspace, string, error) {
	cursor, err := pagecursor.Decode(req.GetCursor())
	if err != nil {
		return nil, "", err
	}
	namespace := strings.TrimSpace(req.GetNamespace())
	if namespace == "" {
		namespace = "default"
	}
	query := selectSQL + ` WHERE w.namespace=$1`
	args := []any{namespace}
	if !cursor.CreatedAt.IsZero() {
		query += ` AND (w.created_at,w.workspace_id)<($2,$3)`
		args = append(args, cursor.CreatedAt, cursor.ID)
	}
	size := pagecursor.PageSize(req.GetPageSize())
	query += fmt.Sprintf(` ORDER BY w.created_at DESC,w.workspace_id DESC LIMIT $%d`, len(args)+1)
	args = append(args, size+1)
	rows, err := s.db.Pool().Query(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []*workspacev1.Workspace{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, r.Workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > size {
		out = out[:size]
		last := out[len(out)-1]
		next = pagecursor.Encode(last.GetCreatedAt().AsTime(), last.GetID())
	}
	return out, next, nil
}

func (s *Store) DeleteWorkspace(ctx context.Context, id string, now time.Time) (*workspacev1.Workspace, error) {
	id = strings.TrimSpace(id)
	var out *workspacev1.Workspace
	started := time.Now()
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		var namespace, nodeID string
		if err := tx.QueryRow(ctx, `SELECT namespace,node_id FROM workspaces WHERE workspace_id=$1`, strings.TrimSpace(id)).Scan(&namespace, &nodeID); errors.Is(err, pgx.ErrNoRows) {
			return grpcstatus.Error(codes.NotFound, "Workspace not found")
		} else if err != nil {
			return err
		}
		if _, err := pgnamespace.LockQuotaRows(ctx, tx, namespace); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT node_id FROM nodes WHERE node_id=$1 FOR UPDATE`, nodeID); err != nil {
			return err
		}
		r, err := scan(tx.QueryRow(ctx, selectSQL+` WHERE w.workspace_id=$1 FOR UPDATE OF w`, strings.TrimSpace(id)))
		if err != nil {
			return err
		}
		if err := pgaccess.AuthorizeTransaction(ctx, tx, accesskernel.ActionResourceWrite, namespace, now.Add(time.Since(started))); err != nil {
			return err
		}
		if r.Workspace.GetAttachedAllocationID() != "" {
			return grpcstatus.Error(codes.FailedPrecondition, "Workspace has an active or unresolved writer")
		}
		if r.Workspace.GetStatus() != workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETED {
			current := now.Add(time.Since(started))
			if _, err := tx.Exec(ctx, `UPDATE workspaces SET status='WORKSPACE_STATUS_DELETING',updated_at=$2,next_reconcile_at=$2 WHERE workspace_id=$1`, id, current); err != nil {
				return err
			}
			r.Workspace.Status = workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING
			r.Workspace.UpdatedAt = timestamppb.New(current)
		}
		out = r.Workspace
		return nil
	})
	return out, err
}

var _ workspacekernel.Store = (*Store)(nil)
