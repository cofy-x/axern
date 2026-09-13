package catalog

import (
	"context"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/grpc"
)

type EnvironmentCatalogClient interface {
	ListEnvironmentTemplates(context.Context, *catalogv1.ListEnvironmentTemplatesRequest, ...grpc.CallOption) (*catalogv1.ListEnvironmentTemplatesResponse, error)
	GetEnvironmentTemplate(context.Context, *catalogv1.GetEnvironmentTemplateRequest, ...grpc.CallOption) (*catalogv1.GetEnvironmentTemplateResponse, error)
}

type Control struct {
	client EnvironmentCatalogClient
}

func New(client EnvironmentCatalogClient) Control {
	return Control{client: client}
}

func (c Control) ListEnvironmentTemplates(ctx context.Context) (*catalogv1.ListEnvironmentTemplatesResponse, error) {
	return c.client.ListEnvironmentTemplates(ctx, &catalogv1.ListEnvironmentTemplatesRequest{})
}

func (c Control) GetEnvironmentTemplate(ctx context.Context, id string) (*catalogv1.GetEnvironmentTemplateResponse, error) {
	return c.client.GetEnvironmentTemplate(ctx, &catalogv1.GetEnvironmentTemplateRequest{ID: id})
}
