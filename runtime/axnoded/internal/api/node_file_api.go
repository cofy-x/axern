package api

import (
	"context"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) StatFile(ctx context.Context, req *nodesandboxv1.StatFileRequest) (*nodesandboxv1.StatFileResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	resp, err := s.svc.StatFile(ctx, &runtimev1.StatFileRequest{ID: target.targetID, Path: req.GetPath()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.StatFileResponse{Info: resp.GetInfo()}, nil
}

func (s *nodeSandboxServer) ListDir(ctx context.Context, req *nodesandboxv1.ListDirRequest) (*nodesandboxv1.ListDirResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	resp, err := s.svc.ListDir(ctx, &runtimev1.ListDirRequest{ID: target.targetID, Path: req.GetPath()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ListDirResponse{Entries: resp.GetEntries()}, nil
}

func (s *nodeSandboxServer) ReadFile(ctx context.Context, req *nodesandboxv1.ReadFileRequest) (*nodesandboxv1.ReadFileResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	resp, err := s.svc.ReadFile(ctx, &runtimev1.ReadFileRequest{ID: target.targetID, Path: req.GetPath()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ReadFileResponse{Data: resp.GetData()}, nil
}

func (s *nodeSandboxServer) WriteFile(ctx context.Context, req *nodesandboxv1.WriteFileRequest) (*nodesandboxv1.WriteFileResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	_, err = s.svc.WriteFile(ctx, &runtimev1.WriteFileRequest{
		ID:            target.targetID,
		Path:          req.GetPath(),
		Data:          req.GetData(),
		CreateParents: req.GetCreateParents(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.WriteFileResponse{}, nil
}

func (s *nodeSandboxServer) MaterializeTaskAssets(ctx context.Context, req *nodesandboxv1.MaterializeTaskAssetsRequest) (*nodesandboxv1.MaterializeTaskAssetsResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	materializer, ok := s.svc.(interface {
		MaterializeTaskAssets(context.Context, *runtimev1.MaterializeTaskAssetsRequest) (*runtimev1.MaterializeTaskAssetsResponse, error)
	})
	if !ok {
		return nil, grpcstatus.Error(codes.Unimplemented, "task asset materialization is unavailable")
	}
	kind := runtimev1.TaskAssetKind(req.GetKind())
	response, err := materializer.MaterializeTaskAssets(ctx, &runtimev1.MaterializeTaskAssetsRequest{ID: target.targetID, SourcePath: req.GetSourcePath(), Target: req.GetTarget(), Kind: kind})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.MaterializeTaskAssetsResponse{DurationMs: response.GetDurationMs()}, nil
}

func (s *nodeSandboxServer) Mkdir(ctx context.Context, req *nodesandboxv1.MkdirRequest) (*nodesandboxv1.MkdirResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	_, err = s.svc.Mkdir(ctx, &runtimev1.MkdirRequest{ID: target.targetID, Path: req.GetPath(), Parents: req.GetParents()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.MkdirResponse{}, nil
}

func (s *nodeSandboxServer) Remove(ctx context.Context, req *nodesandboxv1.RemoveRequest) (*nodesandboxv1.RemoveResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	_, err = s.svc.Remove(ctx, &runtimev1.RemoveRequest{
		ID:        target.targetID,
		Path:      req.GetPath(),
		Recursive: req.GetRecursive(),
		Force:     req.GetForce(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.RemoveResponse{}, nil
}

func (s *nodeSandboxServer) Exists(ctx context.Context, req *nodesandboxv1.ExistsRequest) (*nodesandboxv1.ExistsResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	resp, err := s.svc.Exists(ctx, &runtimev1.ExistsRequest{ID: target.targetID, Path: req.GetPath()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ExistsResponse{Exists: resp.GetExists()}, nil
}

func (s *nodeSandboxServer) Copy(ctx context.Context, req *nodesandboxv1.CopyRequest) (*nodesandboxv1.CopyResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetSrcPath() == "" || req.GetDstPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "src_path and dst_path are required")
	}
	_, err = s.svc.Copy(ctx, &runtimev1.CopyRequest{
		ID:        target.targetID,
		SrcPath:   req.GetSrcPath(),
		DstPath:   req.GetDstPath(),
		Recursive: req.GetRecursive(),
		Overwrite: req.GetOverwrite(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.CopyResponse{}, nil
}

func (s *nodeSandboxServer) Move(ctx context.Context, req *nodesandboxv1.MoveRequest) (*nodesandboxv1.MoveResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetSrcPath() == "" || req.GetDstPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "src_path and dst_path are required")
	}
	_, err = s.svc.Move(ctx, &runtimev1.MoveRequest{ID: target.targetID, SrcPath: req.GetSrcPath(), DstPath: req.GetDstPath(), Overwrite: req.GetOverwrite()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.MoveResponse{}, nil
}

func (s *nodeSandboxServer) Chmod(ctx context.Context, req *nodesandboxv1.ChmodRequest) (*nodesandboxv1.ChmodResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	_, err = s.svc.Chmod(ctx, &runtimev1.ChmodRequest{ID: target.targetID, Path: req.GetPath(), Mode: req.GetMode(), Recursive: req.GetRecursive()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ChmodResponse{}, nil
}

func (s *nodeSandboxServer) Touch(ctx context.Context, req *nodesandboxv1.TouchRequest) (*nodesandboxv1.TouchResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetPath() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	_, err = s.svc.Touch(ctx, &runtimev1.TouchRequest{ID: target.targetID, Path: req.GetPath(), Create: req.GetCreate(), MtimeNs: req.GetMtimeNs()})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.TouchResponse{}, nil
}
