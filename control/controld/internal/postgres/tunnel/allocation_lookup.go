package pgtunnel

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type allocationRecord struct {
	AllocationID   string
	Namespace      string
	NodeID         string
	LifecycleState string
}

func lookupAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*allocationRecord, error) {
	var alloc allocationRecord
	err := tx.QueryRow(ctx, `
		SELECT a.allocation_id, r.namespace, a.node_id, a.lifecycle_state
		FROM allocations a
		JOIN runs r ON r.run_id = a.run_id
		WHERE a.allocation_id = $1
		FOR UPDATE OF a
	`, allocationID).Scan(&alloc.AllocationID, &alloc.Namespace, &alloc.NodeID, &alloc.LifecycleState)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	}
	if err != nil {
		return nil, err
	}
	if err := requireActiveNode(ctx, tx, alloc.NodeID); err != nil {
		return nil, err
	}
	return &alloc, nil
}

func requireActiveNode(ctx context.Context, tx pgx.Tx, nodeID string) error {
	var lifecycle string
	if err := tx.QueryRow(ctx, "SELECT lifecycle_status FROM nodes WHERE node_id = $1 FOR SHARE", nodeID).Scan(&lifecycle); err != nil {
		return err
	}
	if lifecycle != "active" {
		return grpcstatus.Error(codes.PermissionDenied, "Node identity is not active")
	}
	return nil
}
