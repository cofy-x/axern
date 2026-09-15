package api

import (
	"context"
	"maps"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) Exec(ctx context.Context, req *nodesandboxv1.ExecRequest) (*nodesandboxv1.ExecResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID())
	if err != nil {
		return nil, err
	}
	if req.GetSpec() == nil || len(req.GetSpec().GetArgv()) == 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "spec.argv is required")
	}

	resp, err := s.svc.Exec(ctx, &runtimev1.ExecRequest{
		ID:      target.targetID,
		Command: append([]string(nil), req.GetSpec().GetArgv()...),
		Timeout: req.GetSpec().GetTimeoutSeconds(),
		Env:     cloneStringMap(req.GetSpec().GetEnv()),
		Cwd:     req.GetSpec().GetCwd(),
		User:    req.GetSpec().GetUser(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ExecResponse{
		ExitCode:        resp.GetExitCode(),
		Stdout:          resp.GetStdout(),
		Stderr:          resp.GetStderr(),
		StdoutTruncated: resp.GetStdoutTruncated(),
		StderrTruncated: resp.GetStderrTruncated(),
	}, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
