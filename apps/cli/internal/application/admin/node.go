package admin

import (
	"context"
	"fmt"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc"
)

type NodeClient interface {
	ListAdminNodes(context.Context, *adminv1.ListAdminNodesRequest, ...grpc.CallOption) (*adminv1.ListAdminNodesResponse, error)
	RetireAdminNode(context.Context, *adminv1.RetireAdminNodeRequest, ...grpc.CallOption) (*adminv1.RetireAdminNodeResponse, error)
	GetNodeCapabilitySnapshot(context.Context, *adminv1.GetNodeCapabilitySnapshotRequest, ...grpc.CallOption) (*adminv1.GetNodeCapabilitySnapshotResponse, error)
	ListNodeCapabilityTransitions(context.Context, *adminv1.ListNodeCapabilityTransitionsRequest, ...grpc.CallOption) (*adminv1.ListNodeCapabilityTransitionsResponse, error)
	ListCapabilityReconcileQueue(context.Context, *adminv1.ListCapabilityReconcileQueueRequest, ...grpc.CallOption) (*adminv1.ListCapabilityReconcileQueueResponse, error)
	GetAllocationCapabilityDiagnostics(context.Context, *adminv1.GetAllocationCapabilityDiagnosticsRequest, ...grpc.CallOption) (*adminv1.GetAllocationCapabilityDiagnosticsResponse, error)
}

type NodeControl struct{ client NodeClient }

func NewNode(client NodeClient) NodeControl { return NodeControl{client: client} }

func (c NodeControl) List(ctx context.Context, lifecycle string) (*adminv1.ListAdminNodesResponse, error) {
	return c.client.ListAdminNodes(ctx, &adminv1.ListAdminNodesRequest{LifecycleStatus: ParseNodeLifecycle(lifecycle)})
}

func (c NodeControl) Retire(ctx context.Context, nodeID, operatorReason string) (*adminv1.RetireAdminNodeResponse, error) {
	return c.client.RetireAdminNode(ctx, &adminv1.RetireAdminNodeRequest{NodeID: strings.TrimSpace(nodeID), OperatorReason: strings.TrimSpace(operatorReason)})
}

func (c NodeControl) CapabilitySnapshot(ctx context.Context, nodeID string) (*adminv1.GetNodeCapabilitySnapshotResponse, error) {
	return c.client.GetNodeCapabilitySnapshot(ctx, &adminv1.GetNodeCapabilitySnapshotRequest{NodeID: strings.TrimSpace(nodeID)})
}

func (c NodeControl) CapabilityTransitions(ctx context.Context, nodeID string, limit int) (*adminv1.ListNodeCapabilityTransitionsResponse, error) {
	return c.client.ListNodeCapabilityTransitions(ctx, &adminv1.ListNodeCapabilityTransitionsRequest{NodeID: strings.TrimSpace(nodeID), Limit: int32(limit)})
}

func (c NodeControl) CapabilityBacklog(ctx context.Context, nodeID string, limit int) (*adminv1.ListCapabilityReconcileQueueResponse, error) {
	return c.client.ListCapabilityReconcileQueue(ctx, &adminv1.ListCapabilityReconcileQueueRequest{NodeID: strings.TrimSpace(nodeID), Limit: int32(limit)})
}

func (c NodeControl) AllocationCapability(ctx context.Context, allocationID string) (*adminv1.GetAllocationCapabilityDiagnosticsResponse, error) {
	return c.client.GetAllocationCapabilityDiagnostics(ctx, &adminv1.GetAllocationCapabilityDiagnosticsRequest{AllocationID: strings.TrimSpace(allocationID)})
}

func ParseNodeLifecycle(value string) adminv1.AdminNodeLifecycleStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_UNSPECIFIED
	case "active":
		return adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE
	case "retired":
		return adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_RETIRED
	default:
		return adminv1.AdminNodeLifecycleStatus(-1)
	}
}

func ValidateNodeLifecycle(value string) error {
	if ParseNodeLifecycle(value) == adminv1.AdminNodeLifecycleStatus(-1) {
		return fmt.Errorf("node status must be active or retired")
	}
	return nil
}
