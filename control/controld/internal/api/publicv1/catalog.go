package publicv1

import (
	"context"
	"strings"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) ListEnvironmentTemplates(ctx context.Context, req *catalogv1.ListEnvironmentTemplatesRequest) (*catalogv1.ListEnvironmentTemplatesResponse, error) {
	_ = ctx
	return &catalogv1.ListEnvironmentTemplatesResponse{
		EnvironmentTemplates: s.deps.Catalog.List(req),
	}, nil
}

func (s *Server) GetEnvironmentTemplate(ctx context.Context, req *catalogv1.GetEnvironmentTemplateRequest) (*catalogv1.GetEnvironmentTemplateResponse, error) {
	_ = ctx
	id := strings.TrimSpace(req.GetID())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	template, ok := s.deps.Catalog.Get(id, req.GetVersion())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "environment template %q not found", id)
	}
	return &catalogv1.GetEnvironmentTemplateResponse{EnvironmentTemplate: template}, nil
}
