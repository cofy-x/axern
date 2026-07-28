package pggateway

import (
	"context"
	"fmt"

	appgateway "github.com/cofy-x/axern/control/controld/internal/application/gateway"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type Reader struct {
	db *postgres.DB
}

func NewReader(db *postgres.DB) *Reader {
	return &Reader{db: db}
}

func (r *Reader) LoadService(ctx context.Context, serviceID string) (*servicev1.Service, error) {
	if r == nil || r.db == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway route reader is not configured")
	}
	var (
		service    servicev1.Service
		statusText string
		configJSON []byte
	)
	err := r.db.Pool().QueryRow(ctx, `
		SELECT service_id, namespace, status, config
		FROM services
		WHERE service_id = $1
	`, serviceID).Scan(&service.ID, &service.Namespace, &statusText, &configJSON)
	if err == pgx.ErrNoRows {
		return nil, grpcstatus.Error(codes.NotFound, "service not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load service: %w", err)
	}
	if n, ok := servicev1.ServiceStatus_value[statusText]; ok {
		service.Status = servicev1.ServiceStatus(n)
	}
	service.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, service.Config); err != nil {
		return nil, fmt.Errorf("unmarshal service config: %w", err)
	}
	return &service, nil
}

func (r *Reader) LoadAllocation(ctx context.Context, allocationID string) (*appgateway.Allocation, error) {
	if r == nil || r.db == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway route reader is not configured")
	}
	a := &appgateway.Allocation{}
	var statusText string
	err := r.db.Pool().QueryRow(ctx, `
		SELECT a.allocation_id, a.owner_type, a.owner_id, a.node_id, n.node_target, a.attempt, a.status, a.ready
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.allocation_id = $1
	`, allocationID).Scan(&a.AllocationID, &a.OwnerType, &a.OwnerID, &a.NodeID, &a.NodeTarget, &a.Attempt, &statusText, &a.Ready)
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

func (r *Reader) ReadyServiceEndpoints(ctx context.Context, serviceID string) ([]appgateway.EndpointTarget, error) {
	if r == nil || r.db == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway route reader is not configured")
	}
	rows, err := r.db.Pool().Query(ctx, `
		SELECT a.allocation_id, a.node_id, n.node_target, a.attempt, a.ready
		FROM allocations a
		JOIN nodes n ON n.node_id = a.node_id
		WHERE a.owner_type = $1 AND a.owner_id = $2 AND a.status = $3 AND a.ready = true
		ORDER BY a.created_at ASC, a.allocation_id ASC
	`, allocationkernel.OwnerService, serviceID, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String())
	if err != nil {
		return nil, fmt.Errorf("query ready service endpoints: %w", err)
	}
	defer rows.Close()
	var out []appgateway.EndpointTarget
	for rows.Next() {
		var ep appgateway.EndpointTarget
		if err := rows.Scan(&ep.AllocationID, &ep.NodeID, &ep.NodeTarget, &ep.Attempt, &ep.Ready); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

var _ appgateway.RouteReader = (*Reader)(nil)
