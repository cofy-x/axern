package catalog

import (
	"context"

	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"google.golang.org/grpc"
)

type RuntimeCatalogClient interface {
	ListRuntimeTemplates(context.Context, *catalogv1.ListRuntimeTemplatesRequest, ...grpc.CallOption) (*catalogv1.ListRuntimeTemplatesResponse, error)
	GetRuntimeTemplate(context.Context, *catalogv1.GetRuntimeTemplateRequest, ...grpc.CallOption) (*catalogv1.GetRuntimeTemplateResponse, error)
	ListAgentBundles(context.Context, *catalogv1.ListAgentBundlesRequest, ...grpc.CallOption) (*catalogv1.ListAgentBundlesResponse, error)
	GetAgentBundle(context.Context, *catalogv1.GetAgentBundleRequest, ...grpc.CallOption) (*catalogv1.GetAgentBundleResponse, error)
}

type Control struct {
	client RuntimeCatalogClient
}

func New(client RuntimeCatalogClient) Control {
	return Control{client: client}
}

func (c Control) ListRuntimeTemplates(ctx context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error) {
	return c.client.ListRuntimeTemplates(ctx, &catalogv1.ListRuntimeTemplatesRequest{})
}

func (c Control) GetRuntimeTemplate(ctx context.Context, id string) (*catalogv1.GetRuntimeTemplateResponse, error) {
	return c.client.GetRuntimeTemplate(ctx, &catalogv1.GetRuntimeTemplateRequest{ID: id})
}

func (c Control) ListAgentBundles(ctx context.Context) (*catalogv1.ListAgentBundlesResponse, error) {
	return c.client.ListAgentBundles(ctx, &catalogv1.ListAgentBundlesRequest{})
}

func (c Control) GetAgentBundle(ctx context.Context, id string) (*catalogv1.GetAgentBundleResponse, error) {
	return c.client.GetAgentBundle(ctx, &catalogv1.GetAgentBundleRequest{ID: id})
}
