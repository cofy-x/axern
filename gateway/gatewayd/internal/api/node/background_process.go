package node

import (
	"context"

	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
)

func (s *Server) StartBackgroundProcess(ctx context.Context, req *nodesandboxv1.StartBackgroundProcessRequest) (*nodesandboxv1.StartBackgroundProcessResponse, error) {
	var response *nodesandboxv1.StartBackgroundProcessResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.StartBackgroundProcess(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) GetBackgroundProcess(ctx context.Context, req *nodesandboxv1.GetBackgroundProcessRequest) (*nodesandboxv1.GetBackgroundProcessResponse, error) {
	var response *nodesandboxv1.GetBackgroundProcessResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.GetBackgroundProcess(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ListBackgroundProcesses(ctx context.Context, req *nodesandboxv1.ListBackgroundProcessesRequest) (*nodesandboxv1.ListBackgroundProcessesResponse, error) {
	var response *nodesandboxv1.ListBackgroundProcessesResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ListBackgroundProcesses(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) CancelBackgroundProcess(ctx context.Context, req *nodesandboxv1.CancelBackgroundProcessRequest) (*nodesandboxv1.CancelBackgroundProcessResponse, error) {
	var response *nodesandboxv1.CancelBackgroundProcessResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.CancelBackgroundProcess(backendCtx, req)
		return err
	})
	return response, err
}
