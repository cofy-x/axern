package api

import (
	"context"

	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *nodeLifecycleServer) validateWorkspaceAuthority(spec *nodelifecyclev1.WorkspaceSpec) error {
	if s.allowLocal {
		return status.Error(codes.PermissionDenied, "Workspace storage is control-plane only")
	}
	if spec.GetNodeID() == "" || spec.GetWorkspaceID() == "" || spec.GetNamespace() == "" || spec.GetCapacityBytes() <= 0 || spec.GetInodeLimit() <= 0 || spec.GetStorageRootIdentity() == "" {
		return status.Error(codes.InvalidArgument, "complete Workspace specification is required")
	}
	if spec.GetNodeID() != s.nodeID {
		return status.Error(codes.PermissionDenied, "Workspace node_id does not match this node")
	}
	return nil
}

func (s *nodeLifecycleServer) ProvisionWorkspace(ctx context.Context, req *nodelifecyclev1.ProvisionWorkspaceRequest) (*nodelifecyclev1.ProvisionWorkspaceResponse, error) {
	if err := s.validateWorkspaceAuthority(req.GetSpec()); err != nil {
		return nil, err
	}
	return s.svc.ProvisionWorkspace(ctx, req)
}
func (s *nodeLifecycleServer) InspectWorkspace(ctx context.Context, req *nodelifecyclev1.InspectWorkspaceRequest) (*nodelifecyclev1.InspectWorkspaceResponse, error) {
	if err := s.validateWorkspaceAuthority(req.GetSpec()); err != nil {
		return nil, err
	}
	return s.svc.InspectWorkspace(ctx, req)
}
func (s *nodeLifecycleServer) DeleteWorkspace(ctx context.Context, req *nodelifecyclev1.DeleteWorkspaceRequest) (*nodelifecyclev1.DeleteWorkspaceResponse, error) {
	if err := s.validateWorkspaceAuthority(req.GetSpec()); err != nil {
		return nil, err
	}
	return s.svc.DeleteWorkspace(ctx, req)
}
