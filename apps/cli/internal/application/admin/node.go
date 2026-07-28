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
}

type NodeControl struct{ client NodeClient }

func NewNode(client NodeClient) NodeControl { return NodeControl{client: client} }

func (c NodeControl) List(ctx context.Context, lifecycle string) (*adminv1.ListAdminNodesResponse, error) {
	return c.client.ListAdminNodes(ctx, &adminv1.ListAdminNodesRequest{LifecycleStatus: ParseNodeLifecycle(lifecycle)})
}

func (c NodeControl) Retire(ctx context.Context, nodeID, operatorReason string) (*adminv1.RetireAdminNodeResponse, error) {
	return c.client.RetireAdminNode(ctx, &adminv1.RetireAdminNodeRequest{NodeID: strings.TrimSpace(nodeID), OperatorReason: strings.TrimSpace(operatorReason)})
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
