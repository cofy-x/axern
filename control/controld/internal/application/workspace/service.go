package appworkspace

import (
	"context"
	"fmt"
	"time"

	workspacekernel "github.com/cofy-x/axern/control/controld/internal/kernel/workspace"
	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
)

type Service struct {
	store workspacekernel.Store
	node  workspacekernel.NodeClient
}

func New(store workspacekernel.Store, node workspacekernel.NodeClient) *Service {
	return &Service{store: store, node: node}
}

func (s *Service) CreateWorkspace(ctx context.Context, req *workspacev1.CreateWorkspaceRequest, now time.Time) (*workspacev1.Workspace, error) {
	return s.store.CreateWorkspace(ctx, req, now)
}
func (s *Service) GetWorkspace(ctx context.Context, id string) (*workspacev1.Workspace, error) {
	return s.store.GetWorkspace(ctx, id)
}
func (s *Service) ListWorkspaces(ctx context.Context, req *workspacev1.ListWorkspacesRequest) ([]*workspacev1.Workspace, string, error) {
	return s.store.ListWorkspaces(ctx, req)
}
func (s *Service) DeleteWorkspace(ctx context.Context, id string, now time.Time) (*workspacev1.Workspace, error) {
	return s.store.DeleteWorkspace(ctx, id, now)
}

// ReconcileOne delivers one durable storage intent. The claim fences delivery
// completion only; it never grants or releases a filesystem writer.
func (s *Service) ReconcileOne(parent context.Context, owner string, now time.Time) (bool, error) {
	item, err := s.store.ClaimWorkspaceReconcile(parent, owner, now)
	if err != nil || item == nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	r := item.Record
	identity := r.StorageIdentity
	usedBytes, usedInodes := r.Workspace.GetUsedBytes(), r.Workspace.GetUsedInodes()
	var operationErr error
	if r.NodeTarget == "" {
		operationErr = fmt.Errorf("Workspace Node is unavailable")
	} else {
		switch r.Workspace.GetStatus() {
		case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING:
			var result *nodelifecyclev1.ProvisionWorkspaceResponse
			result, operationErr = s.node.ProvisionWorkspace(ctx, r.NodeTarget, &nodelifecyclev1.ProvisionWorkspaceRequest{Spec: r.Spec()})
			if operationErr == nil {
				identity = result.GetStorageIdentity()
				usedBytes = result.GetUsedBytes()
				usedInodes = result.GetUsedInodes()
			}
		case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING:
			// A delete racing an unacknowledged provision first resolves the
			// exact provision result; absence is never treated as data deletion.
			if identity == "" {
				var result *nodelifecyclev1.ProvisionWorkspaceResponse
				result, operationErr = s.node.ProvisionWorkspace(ctx, r.NodeTarget, &nodelifecyclev1.ProvisionWorkspaceRequest{Spec: r.Spec()})
				if operationErr == nil {
					identity = result.GetStorageIdentity()
				}
			}
			if operationErr == nil {
				_, operationErr = s.node.DeleteWorkspace(ctx, r.NodeTarget, &nodelifecyclev1.DeleteWorkspaceRequest{Spec: r.Spec(), StorageIdentity: identity})
			}
			if operationErr == nil {
				usedBytes = 0
				usedInodes = 0
			}
		default:
			var result *nodelifecyclev1.InspectWorkspaceResponse
			result, operationErr = s.node.InspectWorkspace(ctx, r.NodeTarget, &nodelifecyclev1.InspectWorkspaceRequest{Spec: r.Spec(), StorageIdentity: identity})
			if operationErr == nil {
				usedBytes = result.GetUsedBytes()
				usedInodes = result.GetUsedInodes()
			}
		}
	}
	if err := s.store.CompleteWorkspaceReconcile(parent, item, identity, usedBytes, usedInodes, operationErr, time.Now().UTC()); err != nil {
		return true, err
	}
	return true, nil
}
