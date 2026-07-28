package publicv1

import (
	"context"
	"strings"

	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) CreateSecret(ctx context.Context, req *secretv1.CreateSecretRequest) (*secretv1.CreateSecretResponse, error) {
	if s.deps.Secrets == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "secret control is not configured")
	}
	secret, err := s.deps.Secrets.Create(ctx, secretkernel.CreateParams{
		Namespace:  req.GetNamespace(),
		SecretType: req.GetType(),
		StringData: req.GetStringData(),
		Labels:     req.GetLabels(),
	}, s.deps.Now())
	if err != nil {
		return nil, err
	}
	return &secretv1.CreateSecretResponse{Secret: secret}, nil
}

func (s *Server) GetSecret(ctx context.Context, req *secretv1.GetSecretRequest) (*secretv1.GetSecretResponse, error) {
	if s.deps.Secrets == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "secret control is not configured")
	}
	id := strings.TrimSpace(req.GetSecretID())
	if id == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "secret_id is required")
	}
	secret, ok, err := s.deps.Secrets.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, grpcstatus.Errorf(codes.NotFound, "secret %q not found", id)
	}
	return &secretv1.GetSecretResponse{Secret: secret}, nil
}

func (s *Server) ListSecrets(ctx context.Context, req *secretv1.ListSecretsRequest) (*secretv1.ListSecretsResponse, error) {
	if s.deps.Secrets == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "secret control is not configured")
	}
	secrets, err := s.deps.Secrets.List(ctx, req.GetFilter())
	if err != nil {
		return nil, err
	}
	return &secretv1.ListSecretsResponse{Secrets: secrets}, nil
}

func (s *Server) DeleteSecret(ctx context.Context, req *secretv1.DeleteSecretRequest) (*secretv1.DeleteSecretResponse, error) {
	if s.deps.Secrets == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "secret control is not configured")
	}
	id := strings.TrimSpace(req.GetSecretID())
	if id == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "secret_id is required")
	}
	secret, ok, err := s.deps.Secrets.Delete(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, grpcstatus.Errorf(codes.NotFound, "secret %q not found", id)
	}
	return &secretv1.DeleteSecretResponse{Secret: secret}, nil
}
