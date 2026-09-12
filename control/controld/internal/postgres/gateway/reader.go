package pggateway

import (
	"context"
	"fmt"

	appgateway "github.com/cofy-x/axern/control/controld/internal/application/gateway"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Reader struct {
	db *postgres.DB
}

func NewReader(db *postgres.DB) *Reader {
	return &Reader{db: db}
}

func (r *Reader) LoadAllocation(ctx context.Context, allocationID string) (*appgateway.Allocation, error) {
	if r == nil || r.db == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway route reader is not configured")
	}
	a := &appgateway.Allocation{}
	var statusText string
	err := r.db.Pool().QueryRow(ctx, `
		SELECT a.allocation_id, a.owner_type, a.owner_id, a.node_id, n.node_target, a.attempt, a.status
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.allocation_id = $1
	`, allocationID).Scan(&a.AllocationID, &a.OwnerType, &a.OwnerID, &a.NodeID, &a.NodeTarget, &a.Attempt, &statusText)
	if err == pgx.ErrNoRows {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load allocation: %w", err)
	}
	if n, ok := commonv1.AllocationStatus_value[statusText]; ok {
		a.Status = commonv1.AllocationStatus(n)
	}
	return a, nil
}

var _ appgateway.RouteReader = (*Reader)(nil)
