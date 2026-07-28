package pgtunnel

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type allocationRecord struct {
	AllocationID string
	NodeID       string
	NodeTarget   string
	Attempt      int64
	Status       string
}

func lookupAllocation(ctx context.Context, tx pgx.Tx, allocationID string) (*allocationRecord, error) {
	var alloc allocationRecord
	err := tx.QueryRow(ctx, `
		SELECT a.allocation_id, a.node_id, n.node_target, a.attempt, a.status
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.allocation_id = $1
		FOR UPDATE OF a
	`, allocationID).Scan(&alloc.AllocationID, &alloc.NodeID, &alloc.NodeTarget, &alloc.Attempt, &alloc.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	}
	if err != nil {
		return nil, err
	}
	return &alloc, nil
}
