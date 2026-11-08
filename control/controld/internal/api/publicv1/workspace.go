package publicv1

import (
	"context"
	"strings"

	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) CreateWorkspace(ctx context.Context, req *workspacev1.CreateWorkspaceRequest) (*workspacev1.CreateWorkspaceResponse, error) {
	if s.deps.Workspaces == nil {
		return nil, status.Error(codes.FailedPrecondition, "Workspace control is unavailable")
	}
	w, err := s.deps.Workspaces.CreateWorkspace(ctx, req, s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workspacev1.CreateWorkspaceResponse{Workspace: w}, nil
}
func (s *Server) GetWorkspace(ctx context.Context, req *workspacev1.GetWorkspaceRequest) (*workspacev1.GetWorkspaceResponse, error) {
	if s.deps.Workspaces == nil {
		return nil, status.Error(codes.FailedPrecondition, "Workspace control is unavailable")
	}
	if strings.TrimSpace(req.GetWorkspaceID()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id is required")
	}
	w, err := s.deps.Workspaces.GetWorkspace(ctx, req.GetWorkspaceID())
	if err != nil {
		return nil, err
	}
	return &workspacev1.GetWorkspaceResponse{Workspace: w}, nil
}
func (s *Server) ListWorkspaces(ctx context.Context, req *workspacev1.ListWorkspacesRequest) (*workspacev1.ListWorkspacesResponse, error) {
	if s.deps.Workspaces == nil {
		return nil, status.Error(codes.FailedPrecondition, "Workspace control is unavailable")
	}
	if strings.TrimSpace(req.GetNamespace()) == "" {
		return nil, status.Error(codes.InvalidArgument, "namespace is required")
	}
	ws, next, err := s.deps.Workspaces.ListWorkspaces(ctx, req)
	if err != nil {
		return nil, err
	}
	return &workspacev1.ListWorkspacesResponse{Workspaces: ws, NextCursor: next}, nil
}
func (s *Server) DeleteWorkspace(ctx context.Context, req *workspacev1.DeleteWorkspaceRequest) (*workspacev1.DeleteWorkspaceResponse, error) {
	if s.deps.Workspaces == nil {
		return nil, status.Error(codes.FailedPrecondition, "Workspace control is unavailable")
	}
	if strings.TrimSpace(req.GetWorkspaceID()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace_id is required")
	}
	w, err := s.deps.Workspaces.DeleteWorkspace(ctx, req.GetWorkspaceID(), s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &workspacev1.DeleteWorkspaceResponse{Workspace: w}, nil
}
