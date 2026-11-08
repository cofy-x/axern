package workspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"google.golang.org/grpc"
)

type Client interface {
	CreateWorkspace(context.Context, *workspacev1.CreateWorkspaceRequest, ...grpc.CallOption) (*workspacev1.CreateWorkspaceResponse, error)
	GetWorkspace(context.Context, *workspacev1.GetWorkspaceRequest, ...grpc.CallOption) (*workspacev1.GetWorkspaceResponse, error)
	ListWorkspaces(context.Context, *workspacev1.ListWorkspacesRequest, ...grpc.CallOption) (*workspacev1.ListWorkspacesResponse, error)
	DeleteWorkspace(context.Context, *workspacev1.DeleteWorkspaceRequest, ...grpc.CallOption) (*workspacev1.DeleteWorkspaceResponse, error)
}

type Control struct{ client Client }

func New(client Client) Control { return Control{client: client} }

type CreateParams struct {
	Namespace     string
	NodeID        string
	CapacityBytes int64
	InodeLimit    int64
	Labels        map[string]string
}

func (c Control) Create(ctx context.Context, params CreateParams) (*workspacev1.CreateWorkspaceResponse, error) {
	if strings.TrimSpace(params.Namespace) == "" || strings.TrimSpace(params.NodeID) == "" || params.CapacityBytes <= 0 || params.CapacityBytes%512 != 0 || params.InodeLimit <= 0 {
		return nil, fmt.Errorf("namespace, node ID, positive 512-byte-aligned capacity and positive inode limit are required")
	}
	return c.client.CreateWorkspace(ctx, &workspacev1.CreateWorkspaceRequest{Namespace: params.Namespace, NodeID: params.NodeID, CapacityBytes: params.CapacityBytes, InodeLimit: params.InodeLimit, Labels: params.Labels})
}

func (c Control) Get(ctx context.Context, id string) (*workspacev1.GetWorkspaceResponse, error) {
	return c.client.GetWorkspace(ctx, &workspacev1.GetWorkspaceRequest{WorkspaceID: id})
}
func (c Control) List(ctx context.Context, namespace, cursor string, pageSize int32) (*workspacev1.ListWorkspacesResponse, error) {
	return c.client.ListWorkspaces(ctx, &workspacev1.ListWorkspacesRequest{Namespace: namespace, Cursor: cursor, PageSize: pageSize})
}
func (c Control) Delete(ctx context.Context, id string) (*workspacev1.DeleteWorkspaceResponse, error) {
	return c.client.DeleteWorkspace(ctx, &workspacev1.DeleteWorkspaceRequest{WorkspaceID: id})
}

func (c Control) WaitReady(ctx context.Context, id string) (*workspacev1.Workspace, error) {
	for {
		response, err := c.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		value := response.GetWorkspace()
		if value == nil || value.GetID() != id {
			return nil, fmt.Errorf("Workspace readiness returned a missing or different identity")
		}
		switch value.GetStatus() {
		case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY:
			if value.GetAttachedAllocationID() == "" {
				return value, nil
			}
		case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING:
		default:
			return nil, fmt.Errorf("Workspace is not available: %s: %s", value.GetStatus(), value.GetMessage())
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
