package api

import (
	"context"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) WaitSandbox(ctx context.Context, req *nodesandboxv1.WaitSandboxRequest) (*nodesandboxv1.WaitSandboxResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}

	resp, err := s.svc.Wait(ctx, &runtimev1.WaitRequest{ID: target.targetID})
	if err == nil {
		s.reportExit(allocationExitReport{
			allocationID:  target.allocationID,
			attempt:       target.attempt,
			exitCode:      int32(resp.GetExitCode()),
			exitCodeKnown: true,
			message:       resp.GetMessage(),
		})
		return &nodesandboxv1.WaitSandboxResponse{
			State:         nodesandboxv1.SandboxProcessState_SANDBOX_PROCESS_STATE_EXITED,
			ExitCode:      resp.GetExitCode(),
			ExitCodeKnown: true,
			Message:       resp.GetMessage(),
		}, nil
	}

	if grpcstatus.Code(err) == codes.Unavailable && resp != nil {
		s.reportExit(allocationExitReport{
			allocationID:  target.allocationID,
			attempt:       target.attempt,
			exitCode:      resp.GetExitCode(),
			exitCodeKnown: false,
			message:       resp.GetMessage(),
		})
		return &nodesandboxv1.WaitSandboxResponse{
			State:         nodesandboxv1.SandboxProcessState_SANDBOX_PROCESS_STATE_EXITED,
			ExitCode:      resp.GetExitCode(),
			ExitCodeKnown: false,
			Message:       resp.GetMessage(),
		}, nil
	}
	return nil, err
}
