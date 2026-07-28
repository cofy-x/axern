package publicv1

import (
	"context"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ListRuntimeTemplates(ctx context.Context, req *catalogv1.ListRuntimeTemplatesRequest) (*catalogv1.ListRuntimeTemplatesResponse, error) {
	_ = ctx
	return &catalogv1.ListRuntimeTemplatesResponse{
		RuntimeTemplates: s.deps.Catalog.List(req),
	}, nil
}

func (s *Server) GetRuntimeTemplate(ctx context.Context, req *catalogv1.GetRuntimeTemplateRequest) (*catalogv1.GetRuntimeTemplateResponse, error) {
	_ = ctx
	id := strings.TrimSpace(req.GetID())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	template, ok := s.deps.Catalog.Get(id, req.GetVersion())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "runtime template %q not found", id)
	}
	return &catalogv1.GetRuntimeTemplateResponse{RuntimeTemplate: template}, nil
}

func (s *Server) ListAgentBundles(ctx context.Context, req *catalogv1.ListAgentBundlesRequest) (*catalogv1.ListAgentBundlesResponse, error) {
	_ = ctx
	return &catalogv1.ListAgentBundlesResponse{AgentBundles: s.deps.Catalog.ListAgentBundles(req)}, nil
}

func (s *Server) GetAgentBundle(ctx context.Context, req *catalogv1.GetAgentBundleRequest) (*catalogv1.GetAgentBundleResponse, error) {
	_ = ctx
	id := strings.TrimSpace(req.GetID())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	bundle, ok := s.deps.Catalog.GetAgentBundle(id, req.GetVersion())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "agent bundle %q not found", id)
	}
	return &catalogv1.GetAgentBundleResponse{AgentBundle: bundle}, nil
}
