package publicv1_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestCreateSecretRejectsOversizedPayload(t *testing.T) {
	service := newTestService(t)
	defer service.Close()

	_, err := service.PublicV1Handler().CreateSecret(context.Background(), &secretv1.CreateSecretRequest{
		Namespace: "default",
		Type:      secretv1.SecretType_SECRET_TYPE_OPAQUE,
		StringData: map[string]string{
			"token": strings.Repeat("x", 64<<10),
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateSecret() code = %v, want %v: %v", grpcstatus.Code(err), codes.InvalidArgument, err)
	}
}

func TestSecretCRUDRedactsValues(t *testing.T) {
	service := newTestService(t)
	defer service.Close()
	public := service.PublicV1Handler()

	createResp, err := public.CreateSecret(context.Background(), &secretv1.CreateSecretRequest{
		Namespace: "default",
		Type:      secretv1.SecretType_SECRET_TYPE_OPAQUE,
		StringData: map[string]string{
			"token": "super-secret",
			"user":  "alice",
		},
	})
	if err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	secretID := createResp.GetSecret().GetID()
	if len(createResp.GetSecret().GetDataKeys()) != 2 {
		t.Fatalf("data keys = %#v, want 2 keys", createResp.GetSecret().GetDataKeys())
	}

	getResp, err := public.GetSecret(context.Background(), &secretv1.GetSecretRequest{SecretID: secretID})
	if err != nil {
		t.Fatalf("GetSecret() error = %v", err)
	}
	if got := getResp.GetSecret().ProtoReflect().Get(getResp.GetSecret().ProtoReflect().Descriptor().Fields().ByName("data_keys")); !got.IsValid() {
		t.Fatal("expected data_keys to be present")
	}
	if getResp.GetSecret().GetType() != secretv1.SecretType_SECRET_TYPE_OPAQUE {
		t.Fatalf("type = %v, want opaque", getResp.GetSecret().GetType())
	}

	listResp, err := public.ListSecrets(context.Background(), &secretv1.ListSecretsRequest{})
	if err != nil {
		t.Fatalf("ListSecrets() error = %v", err)
	}
	if len(listResp.GetSecrets()) != 1 {
		t.Fatalf("secrets = %d, want 1", len(listResp.GetSecrets()))
	}

	deleteResp, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: secretID})
	if err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}
	if deleteResp.GetSecret().GetID() != secretID {
		t.Fatalf("deleted secret id = %q, want %q", deleteResp.GetSecret().GetID(), secretID)
	}
	_, err = public.GetSecret(context.Background(), &secretv1.GetSecretRequest{SecretID: secretID})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetSecret(after delete) code = %v, want %v", grpcstatus.Code(err), codes.NotFound)
	}
}

func TestCreateEnvironmentWithRegistryCredentialUsesDockerConfigSecret(t *testing.T) {
	resolver := controldtest.NewFakeImageResolver()
	service := newTestServiceWithImageResolver(t, resolver)
	defer service.Close()
	public := service.PublicV1Handler()

	secretResp, err := public.CreateSecret(context.Background(), &secretv1.CreateSecretRequest{
		Namespace: "default",
		Type:      secretv1.SecretType_SECRET_TYPE_DOCKER_CONFIG_JSON,
		StringData: map[string]string{
			".dockerconfigjson": `{"auths":{"registry.example.com":{"auth":"YWJjZA=="}}}`,
		},
	})
	if err != nil {
		t.Fatalf("CreateSecret(docker-config-json) error = %v", err)
	}

	envResp, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{
			Namespace: "default",
			Image: &environmentv1.EnvironmentImageSource{
				Ref:                  "docker.io/library/nginx:1.27",
				RegistryCredentialID: secretResp.GetSecret().GetID(),
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	if got := envResp.GetEnvironment().GetSpec().GetImage().GetRegistryCredentialID(); got != secretResp.GetSecret().GetID() {
		t.Fatalf("registry_credential_id = %q, want %q", got, secretResp.GetSecret().GetID())
	}
	if resolver.LastOptions.DockerConfigJSON == "" {
		t.Fatal("resolver docker config json = empty, want secret payload to be passed")
	}
}
