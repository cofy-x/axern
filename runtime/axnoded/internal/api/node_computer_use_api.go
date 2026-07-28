package api

import (
	"context"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *nodeSandboxServer) ComputerUseStatus(ctx context.Context, req *nodesandboxv1.ComputerUseStatusRequest) (*nodesandboxv1.ComputerUseStatusResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.ComputerUseStatus(ctx, &runtimev1.ComputerUseStatusRequest{ID: target.targetID})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ComputerUseStatusResponse{
		Available:    resp.GetAvailable(),
		Display:      resp.GetDisplay(),
		Backend:      resp.GetBackend(),
		Reason:       resp.GetReason(),
		Dependencies: apiComputerUseDependencies(resp.GetDependencies()),
	}, nil
}

func (s *nodeSandboxServer) ComputerUseScreenshot(ctx context.Context, req *nodesandboxv1.ComputerUseScreenshotRequest) (*nodesandboxv1.ComputerUseScreenshotResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.ComputerUseScreenshot(ctx, &runtimev1.ComputerUseScreenshotRequest{
		ID:         target.targetID,
		ShowCursor: req.GetShowCursor(),
		Region:     apiComputerUseRegion(req.GetRegion()),
		Format:     req.GetFormat(),
		Quality:    req.GetQuality(),
		Scale:      req.GetScale(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ComputerUseScreenshotResponse{
		Data:        resp.GetData(),
		ContentType: resp.GetContentType(),
	}, nil
}

func (s *nodeSandboxServer) ComputerUseDisplay(ctx context.Context, req *nodesandboxv1.ComputerUseDisplayRequest) (*nodesandboxv1.ComputerUseDisplayResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	resp, err := s.svc.ComputerUseDisplay(ctx, &runtimev1.ComputerUseDisplayRequest{ID: target.targetID})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ComputerUseDisplayResponse{
		Display: resp.GetDisplay(),
		Backend: resp.GetBackend(),
		Width:   resp.GetWidth(),
		Height:  resp.GetHeight(),
	}, nil
}

func (s *nodeSandboxServer) ComputerUseMouse(ctx context.Context, req *nodesandboxv1.ComputerUseMouseRequest) (*nodesandboxv1.ComputerUseMouseResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	_, err = s.svc.ComputerUseMouse(ctx, &runtimev1.ComputerUseMouseRequest{
		ID:        target.targetID,
		Action:    req.GetAction(),
		X:         req.GetX(),
		Y:         req.GetY(),
		ToX:       req.GetToX(),
		ToY:       req.GetToY(),
		Button:    req.GetButton(),
		Direction: req.GetDirection(),
		Amount:    req.GetAmount(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ComputerUseMouseResponse{}, nil
}

func (s *nodeSandboxServer) ComputerUseKeyboard(ctx context.Context, req *nodesandboxv1.ComputerUseKeyboardRequest) (*nodesandboxv1.ComputerUseKeyboardResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	_, err = s.svc.ComputerUseKeyboard(ctx, &runtimev1.ComputerUseKeyboardRequest{
		ID:      target.targetID,
		Text:    req.GetText(),
		Key:     req.GetKey(),
		Keys:    req.GetKeys(),
		DelayMs: req.GetDelayMs(),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ComputerUseKeyboardResponse{}, nil
}

func apiComputerUseRegion(region *nodesandboxv1.ComputerUseRegion) *runtimev1.ComputerUseRegion {
	if region == nil {
		return nil
	}
	return &runtimev1.ComputerUseRegion{
		X:      region.GetX(),
		Y:      region.GetY(),
		Width:  region.GetWidth(),
		Height: region.GetHeight(),
	}
}

func apiComputerUseDependencies(items []*runtimev1.ComputerUseDependencyStatus) []*nodesandboxv1.ComputerUseDependencyStatus {
	out := make([]*nodesandboxv1.ComputerUseDependencyStatus, 0, len(items))
	for _, item := range items {
		out = append(out, &nodesandboxv1.ComputerUseDependencyStatus{
			Name:      item.GetName(),
			Available: item.GetAvailable(),
			Reason:    item.GetReason(),
		})
	}
	return out
}
