package environment

import (
	"context"
	"fmt"
	"strings"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"google.golang.org/grpc"
)

type EnvironmentClient interface {
	CreateEnvironment(context.Context, *environmentv1.CreateEnvironmentRequest, ...grpc.CallOption) (*environmentv1.CreateEnvironmentResponse, error)
	GetEnvironment(context.Context, *environmentv1.GetEnvironmentRequest, ...grpc.CallOption) (*environmentv1.GetEnvironmentResponse, error)
	ListEnvironments(context.Context, *environmentv1.ListEnvironmentsRequest, ...grpc.CallOption) (*environmentv1.ListEnvironmentsResponse, error)
}

type DeleteClient interface {
	DeleteEnvironment(context.Context, *environmentv1.DeleteEnvironmentRequest, ...grpc.CallOption) (*environmentv1.DeleteEnvironmentResponse, error)
}

type Control struct {
	client       EnvironmentClient
	deleteClient DeleteClient
}

type CreateParams struct {
	Spec   *environmentv1.EnvironmentSpec
	Labels map[string]string
}

type ResolveParams struct {
	EnvironmentID string
	Spec          *environmentv1.EnvironmentSpec
}

func New(client EnvironmentClient) Control {
	return Control{client: client}
}

func NewWithDelete(client interface {
	EnvironmentClient
	DeleteClient
}) Control {
	return Control{client: client, deleteClient: client}
}

func (c Control) Create(ctx context.Context, params CreateParams) (*environmentv1.CreateEnvironmentResponse, error) {
	return c.client.CreateEnvironment(ctx, &environmentv1.CreateEnvironmentRequest{
		Spec:   params.Spec,
		Labels: params.Labels,
	})
}

func (c Control) Get(ctx context.Context, environmentID string) (*environmentv1.GetEnvironmentResponse, error) {
	return c.client.GetEnvironment(ctx, &environmentv1.GetEnvironmentRequest{EnvironmentID: environmentID})
}

func (c Control) List(ctx context.Context) (*environmentv1.ListEnvironmentsResponse, error) {
	return c.client.ListEnvironments(ctx, &environmentv1.ListEnvironmentsRequest{})
}

func (c Control) Delete(ctx context.Context, environmentID string) (*environmentv1.DeleteEnvironmentResponse, error) {
	if c.deleteClient == nil {
		return nil, fmt.Errorf("environment delete client is required")
	}
	return c.deleteClient.DeleteEnvironment(ctx, &environmentv1.DeleteEnvironmentRequest{EnvironmentID: environmentID})
}

func (c Control) ResolveID(ctx context.Context, params ResolveParams) (string, error) {
	environmentID := strings.TrimSpace(params.EnvironmentID)
	if environmentID != "" && params.Spec != nil {
		return "", fmt.Errorf("environment-id cannot be combined with template-id or image-ref")
	}
	if environmentID != "" {
		return environmentID, nil
	}
	if params.Spec == nil {
		return "", fmt.Errorf("environment-id, template-id, or image-ref is required")
	}
	resp, err := c.Create(ctx, CreateParams{Spec: params.Spec})
	if err != nil {
		return "", err
	}
	createdID := strings.TrimSpace(resp.GetEnvironment().GetID())
	if createdID == "" {
		return "", fmt.Errorf("created environment response did not include an environment id")
	}
	return createdID, nil
}
