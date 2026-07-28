package namespace

import (
	"context"

	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	"google.golang.org/grpc"
)

type Client interface {
	CreateNamespace(context.Context, *namespacev1.CreateNamespaceRequest, ...grpc.CallOption) (*namespacev1.CreateNamespaceResponse, error)
	GetNamespace(context.Context, *namespacev1.GetNamespaceRequest, ...grpc.CallOption) (*namespacev1.GetNamespaceResponse, error)
	ListNamespaces(context.Context, *namespacev1.ListNamespacesRequest, ...grpc.CallOption) (*namespacev1.ListNamespacesResponse, error)
	DeleteNamespace(context.Context, *namespacev1.DeleteNamespaceRequest, ...grpc.CallOption) (*namespacev1.DeleteNamespaceResponse, error)
}

type Control struct {
	client Client
}

func New(client Client) Control {
	return Control{client: client}
}

func (c Control) Create(ctx context.Context, namespace string) (*namespacev1.CreateNamespaceResponse, error) {
	return c.client.CreateNamespace(ctx, &namespacev1.CreateNamespaceRequest{Namespace: namespace})
}

func (c Control) Get(ctx context.Context, namespace string) (*namespacev1.GetNamespaceResponse, error) {
	return c.client.GetNamespace(ctx, &namespacev1.GetNamespaceRequest{Namespace: namespace})
}

func (c Control) List(ctx context.Context) (*namespacev1.ListNamespacesResponse, error) {
	return c.client.ListNamespaces(ctx, &namespacev1.ListNamespacesRequest{})
}

func (c Control) Delete(ctx context.Context, namespace string) (*namespacev1.DeleteNamespaceResponse, error) {
	return c.client.DeleteNamespace(ctx, &namespacev1.DeleteNamespaceRequest{Namespace: namespace})
}
