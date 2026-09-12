package appadmin

import (
	"context"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type NodeLifecycleStore interface {
	ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error)
	RetireNode(ctx context.Context, req adminkernel.RetireNodeRequest) (*nodekernel.Record, error)
}

type NodeRegistryUpdater interface {
	MarkRetired(nodeID string, retiredAt time.Time, reason string)
}

type NodeControl struct {
	store           NodeLifecycleStore
	registry        NodeRegistryUpdater
	heartbeatWindow time.Duration
}

func NewNodeControl(store NodeLifecycleStore, registry NodeRegistryUpdater, heartbeatWindow time.Duration) NodeControl {
	return NodeControl{store: store, registry: registry, heartbeatWindow: heartbeatWindow}
}

func (c NodeControl) ListNodes(ctx context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	if c.store == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "node admin is unavailable")
	}
	return c.store.ListNodes(ctx, filter)
}

func (c NodeControl) RetireNode(ctx context.Context, nodeID, operatorReason string, now time.Time) (*nodekernel.Record, error) {
	req := adminkernel.NormalizeRetireNodeRequest(adminkernel.RetireNodeRequest{NodeID: nodeID, OperatorReason: operatorReason, Now: now, HeartbeatWindow: c.heartbeatWindow})
	if err := adminkernel.ValidateRetireNodeRequest(req); err != nil {
		return nil, err
	}
	if c.store == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "node admin is unavailable")
	}
	record, err := c.store.RetireNode(ctx, req)
	if err != nil {
		return nil, err
	}
	if c.registry != nil {
		c.registry.MarkRetired(record.NodeID, record.RetiredAt, record.RetiredReason)
	}
	return record, nil
}
