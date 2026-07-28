package secret

import (
	"context"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc"
)

type SecretClient interface {
	CreateSecret(context.Context, *secretv1.CreateSecretRequest, ...grpc.CallOption) (*secretv1.CreateSecretResponse, error)
	GetSecret(context.Context, *secretv1.GetSecretRequest, ...grpc.CallOption) (*secretv1.GetSecretResponse, error)
	ListSecrets(context.Context, *secretv1.ListSecretsRequest, ...grpc.CallOption) (*secretv1.ListSecretsResponse, error)
	DeleteSecret(context.Context, *secretv1.DeleteSecretRequest, ...grpc.CallOption) (*secretv1.DeleteSecretResponse, error)
}

type Control struct {
	client SecretClient
}

func New(client SecretClient) Control {
	return Control{client: client}
}

func (c Control) Create(ctx context.Context, req *secretv1.CreateSecretRequest) (*secretv1.CreateSecretResponse, error) {
	return c.client.CreateSecret(ctx, req)
}

func (c Control) Get(ctx context.Context, secretID string) (*secretv1.GetSecretResponse, error) {
	return c.client.GetSecret(ctx, &secretv1.GetSecretRequest{SecretID: secretID})
}

func (c Control) List(ctx context.Context, req *secretv1.ListSecretsRequest) (*secretv1.ListSecretsResponse, error) {
	return c.client.ListSecrets(ctx, req)
}

func (c Control) Delete(ctx context.Context, secretID string) (*secretv1.DeleteSecretResponse, error) {
	return c.client.DeleteSecret(ctx, &secretv1.DeleteSecretRequest{SecretID: secretID})
}
