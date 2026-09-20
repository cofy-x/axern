package publicv1_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	node := service.NodeV1Handler()
	now := time.Now().UTC()
	admitTestNode(t, service, "node-registry-ref")

	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{NodeID: "node-registry-ref", NodeTarget: "127.0.0.1:25002", Summary: controldtest.ReadySummary(now)}); err != nil {
		t.Fatalf("ReportNode() error = %v", err)
	}

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

	secretID := secretResp.GetSecret().GetID()
	if _, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: secretID}); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteSecret(referenced) code = %v, want %v: %v", grpcstatus.Code(err), codes.FailedPrecondition, err)
	}
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: envResp.GetEnvironment().GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/true"}},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := public.DeleteEnvironment(context.Background(), &environmentv1.DeleteEnvironmentRequest{EnvironmentID: envResp.GetEnvironment().GetID()}); err != nil {
		t.Fatalf("DeleteEnvironment() error = %v", err)
	}
	if _, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: secretID}); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteSecret(active Run snapshot reference) code = %v, want %v: %v", grpcstatus.Code(err), codes.FailedPrecondition, err)
	}
	if _, err := node.BatchReportAllocationLifecycle(context.Background(), &nodev1.BatchReportAllocationLifecycleRequest{
		NodeID: "node-registry-ref",
		Observations: []*nodev1.AllocationLifecycleObservation{{
			AllocationID: runResp.GetRun().GetAllocationID(),
			State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
			ExitCode:     func() *int32 { value := int32(0); return &value }(),
			ObservedAt:   timestamppb.New(now.Add(time.Second)),
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationLifecycle() error = %v", err)
	}
	if _, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: secretID}); err != nil {
		t.Fatalf("DeleteSecret(after terminal Run) error = %v", err)
	}
}

func TestRequiredRunSecretReferenceEndsWithRunLifecycle(t *testing.T) {
	service := newTestService(t)
	defer service.Close()
	public := service.PublicV1Handler()
	node := service.NodeV1Handler()
	now := time.Now().UTC()
	admitTestNode(t, service, "node-secret-ref")

	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{NodeID: "node-secret-ref", NodeTarget: "127.0.0.1:25001", Summary: controldtest.ReadySummary(now)}); err != nil {
		t.Fatalf("ReportNode() error = %v", err)
	}
	secretResp, err := public.CreateSecret(context.Background(), &secretv1.CreateSecretRequest{
		Namespace: "default",
		Type:      secretv1.SecretType_SECRET_TYPE_OPAQUE,
		StringData: map[string]string{
			"token": "run-secret",
		},
	})
	if err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	optionalSecretResp, err := public.CreateSecret(context.Background(), &secretv1.CreateSecretRequest{
		Namespace:  "default",
		Type:       secretv1.SecretType_SECRET_TYPE_OPAQUE,
		StringData: map[string]string{"optional": "value"},
	})
	if err != nil {
		t.Fatalf("CreateSecret(optional) error = %v", err)
	}
	envResp, err := public.CreateEnvironment(context.Background(), &environmentv1.CreateEnvironmentRequest{
		Spec: &environmentv1.EnvironmentSpec{Namespace: "default", Image: &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"}},
	})
	if err != nil {
		t.Fatalf("CreateEnvironment() error = %v", err)
	}
	runResp, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: envResp.GetEnvironment().GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/true"},
			SecretEnv: []*commonv1.SecretEnvVar{{
				Name:     "TOKEN",
				SecretID: secretResp.GetSecret().GetID(),
				Key:      "token",
			}},
			SecretFiles: []*commonv1.SecretFile{{
				Path:     "/run/secrets/optional",
				SecretID: optionalSecretResp.GetSecret().GetID(),
				Key:      "optional",
				Optional: true,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	secretID := secretResp.GetSecret().GetID()
	if _, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: secretID}); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteSecret(active Run reference) code = %v, want %v: %v", grpcstatus.Code(err), codes.FailedPrecondition, err)
	}
	if _, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: optionalSecretResp.GetSecret().GetID()}); err != nil {
		t.Fatalf("DeleteSecret(optional active Run reference) error = %v", err)
	}
	if _, err := node.BatchReportAllocationLifecycle(context.Background(), &nodev1.BatchReportAllocationLifecycleRequest{
		NodeID: "node-secret-ref",
		Observations: []*nodev1.AllocationLifecycleObservation{{
			AllocationID: runResp.GetRun().GetAllocationID(),
			State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
			ExitCode:     func() *int32 { value := int32(0); return &value }(),
			ObservedAt:   timestamppb.New(now.Add(time.Second)),
		}},
	}); err != nil {
		t.Fatalf("BatchReportAllocationLifecycle() error = %v", err)
	}
	if _, err := public.DeleteSecret(context.Background(), &secretv1.DeleteSecretRequest{SecretID: secretID}); err != nil {
		t.Fatalf("DeleteSecret(after terminal Run) error = %v", err)
	}
}
