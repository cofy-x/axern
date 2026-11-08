package axernsdk

import (
	"context"
	"fmt"
	"time"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
)

// CreateWorkspaceOptions requests retained capacity on one explicit Node.
// Closing a Sandbox never deletes its Workspace.
type CreateWorkspaceOptions struct {
	Namespace     string
	NodeID        string
	CapacityBytes int64
	InodeLimit    int64
	Labels        map[string]string
}

type ListWorkspacesOptions struct {
	Namespace string
	Cursor    string
	PageSize  int32
}

func (c *Client) CreateWorkspace(ctx context.Context, options CreateWorkspaceOptions) (*workspacev1.Workspace, error) {
	if options.NodeID == "" {
		return nil, requiredError("node_id")
	}
	if options.CapacityBytes <= 0 || options.CapacityBytes%512 != 0 || options.InodeLimit <= 0 {
		return nil, fmt.Errorf("workspace capacity_bytes must be positive and 512-byte aligned; inode_limit must be positive")
	}
	response, err := c.workspaces.CreateWorkspace(ctx, &workspacev1.CreateWorkspaceRequest{
		Namespace: defaultString(options.Namespace, "default"), NodeID: options.NodeID,
		CapacityBytes: options.CapacityBytes, InodeLimit: options.InodeLimit, Labels: cloneMap(options.Labels),
	})
	if err != nil {
		return nil, mapRPCError(err, "create workspace", "")
	}
	return response.GetWorkspace(), nil
}

func (c *Client) GetWorkspace(ctx context.Context, id string) (*workspacev1.Workspace, error) {
	if id == "" {
		return nil, requiredError("workspace_id")
	}
	response, err := c.workspaces.GetWorkspace(ctx, &workspacev1.GetWorkspaceRequest{WorkspaceID: id})
	if err != nil {
		return nil, mapRPCError(err, "get workspace", "")
	}
	return response.GetWorkspace(), nil
}

func (c *Client) ListWorkspaces(ctx context.Context, options ListWorkspacesOptions) (*workspacev1.ListWorkspacesResponse, error) {
	response, err := c.workspaces.ListWorkspaces(ctx, &workspacev1.ListWorkspacesRequest{
		Namespace: defaultString(options.Namespace, "default"), Cursor: options.Cursor, PageSize: options.PageSize,
	})
	if err != nil {
		return nil, mapRPCError(err, "list workspaces", "")
	}
	return response, nil
}

// DeleteWorkspace records explicit deletion intent. Retained capacity remains
// charged until node cleanup completes; use GetWorkspace to observe completion.
func (c *Client) DeleteWorkspace(ctx context.Context, id string) (*workspacev1.Workspace, error) {
	if id == "" {
		return nil, requiredError("workspace_id")
	}
	response, err := c.workspaces.DeleteWorkspace(ctx, &workspacev1.DeleteWorkspaceRequest{WorkspaceID: id})
	if err != nil {
		return nil, mapRPCError(err, "delete workspace", "")
	}
	return response.GetWorkspace(), nil
}

// WaitWorkspaceReady waits for ready data with no current writer. The observation
// does not reserve write authority; it never retries a mutation. The wait is
// bounded by two minutes or an earlier caller deadline.
func (c *Client) WaitWorkspaceReady(ctx context.Context, id string) (*workspacev1.Workspace, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	for {
		workspace, err := c.GetWorkspace(ctx, id)
		if err != nil {
			return nil, err
		}
		if workspace == nil || workspace.GetID() != id {
			return nil, fmt.Errorf("workspace readiness returned a missing or different identity")
		}
		switch workspace.GetStatus() {
		case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY:
			if workspace.GetAttachedAllocationID() == "" {
				return workspace, nil
			}
		case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING:
		default:
			return nil, fmt.Errorf("workspace %s is not available: %s (%s)", id, workspace.GetStatus(), workspace.GetMessage())
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
