package appadmin

import (
	"context"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
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

type NodeStorageState interface {
	ListStorageBindings(ctx context.Context, filter adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error)
	ListStorageReclaims(ctx context.Context, filter adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error)
}

type NodeControl struct {
	store           NodeLifecycleStore
	registry        NodeRegistryUpdater
	storage         NodeStorageState
	heartbeatWindow time.Duration
}

func NewNodeControl(store NodeLifecycleStore, registry NodeRegistryUpdater, storage NodeStorageState, heartbeatWindow time.Duration) NodeControl {
	return NodeControl{store: store, registry: registry, storage: storage, heartbeatWindow: heartbeatWindow}
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
	if err := c.requireStorageClear(ctx, req.NodeID); err != nil {
		return nil, err
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

func (c NodeControl) requireStorageClear(ctx context.Context, nodeID string) error {
	if c.storage == nil {
		return nil
	}
	bindings, err := c.storage.ListStorageBindings(ctx, adminkernel.StorageBindingFilter{
		NodeID: nodeID,
		Statuses: []storagev1.VolumeStatus{
			storagev1.VolumeStatus_VOLUME_STATUS_PENDING,
			storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
			storagev1.VolumeStatus_VOLUME_STATUS_AVAILABLE,
			storagev1.VolumeStatus_VOLUME_STATUS_DELETING,
			storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
			storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHING,
			storagev1.VolumeStatus_VOLUME_STATUS_PUBLISHED,
			storagev1.VolumeStatus_VOLUME_STATUS_RELEASING,
		},
		Limit: 1,
	})
	if err != nil {
		return grpcstatus.Errorf(codes.Unavailable, "check node storage bindings: %v", err)
	}
	if len(bindings) > 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "node %q cannot be retired while storage binding %q is not deleted", nodeID, strings.TrimSpace(bindings[0].BindingID))
	}
	reclaims, err := c.storage.ListStorageReclaims(ctx, adminkernel.StorageReclaimFilter{NodeID: nodeID, Limit: 1})
	if err != nil {
		return grpcstatus.Errorf(codes.Unavailable, "check node storage reclaims: %v", err)
	}
	if len(reclaims) > 0 {
		return grpcstatus.Errorf(codes.FailedPrecondition, "node %q cannot be retired while volume claim %q is pending reclaim", nodeID, strings.TrimSpace(reclaims[0].ClaimID))
	}
	return nil
}
