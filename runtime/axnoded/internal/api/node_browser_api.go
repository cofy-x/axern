package api

import (
	"context"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *nodeSandboxServer) BrowserStatus(ctx context.Context, req *nodesandboxv1.BrowserStatusRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserStatus(ctx, &runtimev1.BrowserStatusRequest{ID: target.targetID})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserOpen(ctx context.Context, req *nodesandboxv1.BrowserOpenRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserOpen(ctx, &runtimev1.BrowserOpenRequest{ID: target.targetID, Url: req.GetUrl()})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserClose(ctx context.Context, req *nodesandboxv1.BrowserCloseRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserClose(ctx, &runtimev1.BrowserCloseRequest{ID: target.targetID})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserNavigate(ctx context.Context, req *nodesandboxv1.BrowserNavigateRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserNavigate(ctx, &runtimev1.BrowserNavigateRequest{ID: target.targetID, Url: req.GetUrl()})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserResize(ctx context.Context, req *nodesandboxv1.BrowserResizeRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserResize(ctx, &runtimev1.BrowserResizeRequest{ID: target.targetID, Width: req.GetWidth(), Height: req.GetHeight()})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserClick(ctx context.Context, req *nodesandboxv1.BrowserClickRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserClick(ctx, &runtimev1.BrowserClickRequest{ID: target.targetID, X: req.GetX(), Y: req.GetY(), Button: req.GetButton()})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserType(ctx context.Context, req *nodesandboxv1.BrowserTypeRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserType(ctx, &runtimev1.BrowserTypeRequest{ID: target.targetID, Text: req.GetText(), DelayMs: req.GetDelayMs()})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func (s *nodeSandboxServer) BrowserWait(ctx context.Context, req *nodesandboxv1.BrowserWaitRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.BrowserWait(ctx, &runtimev1.BrowserWaitRequest{ID: target.targetID, TimeoutMs: req.GetTimeoutMs()})
	if err != nil {
		return nil, err
	}
	return apiBrowserStatus(resp), nil
}

func apiBrowserStatus(status *runtimev1.BrowserStatusResponse) *nodesandboxv1.BrowserStatusResponse {
	return &nodesandboxv1.BrowserStatusResponse{
		Available: status.GetAvailable(),
		Command:   status.GetCommand(),
		Running:   status.GetRunning(),
		Pid:       status.GetPid(),
		Url:       status.GetUrl(),
		Reason:    status.GetReason(),
	}
}
