package admin

import (
	"context"
	"fmt"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc"
)

type NodeClient interface {
	AdmitAdminNode(context.Context, *adminv1.AdmitAdminNodeRequest, ...grpc.CallOption) (*adminv1.AdmitAdminNodeResponse, error)
	ListAdminNodes(context.Context, *adminv1.ListAdminNodesRequest, ...grpc.CallOption) (*adminv1.ListAdminNodesResponse, error)
	RetireAdminNode(context.Context, *adminv1.RetireAdminNodeRequest, ...grpc.CallOption) (*adminv1.RetireAdminNodeResponse, error)
	GetNodeCapabilitySnapshot(context.Context, *adminv1.GetNodeCapabilitySnapshotRequest, ...grpc.CallOption) (*adminv1.GetNodeCapabilitySnapshotResponse, error)
	GetAllocationCapabilityDiagnostics(context.Context, *adminv1.GetAllocationCapabilityDiagnosticsRequest, ...grpc.CallOption) (*adminv1.GetAllocationCapabilityDiagnosticsResponse, error)
}

type NodeControl struct{ client NodeClient }

func NewNode(client NodeClient) NodeControl { return NodeControl{client: client} }

func (c NodeControl) Admit(ctx context.Context, nodeID, nodeCredential, operatorReason string) (*adminv1.AdmitAdminNodeResponse, error) {
	return c.client.AdmitAdminNode(ctx, &adminv1.AdmitAdminNodeRequest{
		NodeID:         strings.TrimSpace(nodeID),
		NodeCredential: strings.TrimSpace(nodeCredential), OperatorReason: strings.TrimSpace(operatorReason),
	})
}

func (c NodeControl) List(ctx context.Context, lifecycle string) (*adminv1.ListAdminNodesResponse, error) {
	return c.client.ListAdminNodes(ctx, &adminv1.ListAdminNodesRequest{LifecycleStatus: ParseNodeLifecycle(lifecycle)})
}

func (c NodeControl) Retire(ctx context.Context, nodeID, operatorReason string) (*adminv1.RetireAdminNodeResponse, error) {
	return c.client.RetireAdminNode(ctx, &adminv1.RetireAdminNodeRequest{NodeID: strings.TrimSpace(nodeID), OperatorReason: strings.TrimSpace(operatorReason)})
}

func (c NodeControl) CapabilitySnapshot(ctx context.Context, nodeID string) (*adminv1.GetNodeCapabilitySnapshotResponse, error) {
	return c.client.GetNodeCapabilitySnapshot(ctx, &adminv1.GetNodeCapabilitySnapshotRequest{NodeID: strings.TrimSpace(nodeID)})
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
