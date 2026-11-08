package service

import (
	"context"
	"fmt"

	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/workspace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (h *sandboxService) ProvisionWorkspace(_ context.Context, request *nodelifecyclev1.ProvisionWorkspaceRequest) (*nodelifecyclev1.ProvisionWorkspaceResponse, error) {
	value, err := h.workspaceStore().Provision(workspace.FromSpec(request.GetSpec()))
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Workspace provisioning unavailable: %v", err)
	}
	return &nodelifecyclev1.ProvisionWorkspaceResponse{StorageIdentity: value.Identity, UsedBytes: value.Bytes, UsedInodes: value.Inodes}, nil
}

func (h *sandboxService) InspectWorkspace(_ context.Context, request *nodelifecyclev1.InspectWorkspaceRequest) (*nodelifecyclev1.InspectWorkspaceResponse, error) {
	value, err := h.workspaceStore().Inspect(workspace.FromSpec(request.GetSpec()), request.GetStorageIdentity())
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Workspace storage unavailable: %v", err)
	}
	return &nodelifecyclev1.InspectWorkspaceResponse{UsedBytes: value.Bytes, UsedInodes: value.Inodes}, nil
}

func (h *sandboxService) DeleteWorkspace(_ context.Context, request *nodelifecyclev1.DeleteWorkspaceRequest) (*nodelifecyclev1.DeleteWorkspaceResponse, error) {
	if err := h.workspaceStore().Delete(workspace.FromSpec(request.GetSpec()), request.GetStorageIdentity()); err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "Workspace deletion incomplete: %v", err)
	}
	return &nodelifecyclev1.DeleteWorkspaceResponse{}, nil
}

func (h *sandboxService) AcknowledgeWorkspaceCleanup(nodeID string, value *nodelifecyclev1.WorkspaceCleanupReceipt) error {
	if nodeID == "" || nodeID != h.config.ControlPlaneNodeID {
		return status.Error(codes.PermissionDenied, "Workspace cleanup node identity mismatch")
	}
	receipt, err := workspace.FromReceipt(value)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if err := h.workspaceStore().Acknowledge(receipt); err != nil {
		return fmt.Errorf("acknowledge Workspace cleanup: %w", err)
	}
	return nil
}
