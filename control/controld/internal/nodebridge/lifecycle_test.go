package nodebridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestBridgeUsesSeparateLifecycleTimeouts(t *testing.T) {
	bridge := New(&captureLifecycleClient{}, Config{})
	if bridge.createTimeout != allocationkernel.CreateExecutionTimeout {
		t.Fatalf("create timeout = %s, want %s", bridge.createTimeout, allocationkernel.CreateExecutionTimeout)
	}
	if bridge.operationTimeout != allocationkernel.LifecycleOperationTimeout {
		t.Fatalf("operation timeout = %s, want %s", bridge.operationTimeout, allocationkernel.LifecycleOperationTimeout)
	}

	bridge = New(&captureLifecycleClient{}, Config{
		CreateTimeout:    3 * time.Minute,
		OperationTimeout: 15 * time.Second,
	})
	if bridge.createTimeout != 3*time.Minute || bridge.operationTimeout != 15*time.Second {
		t.Fatalf("configured timeouts = create:%s operation:%s", bridge.createTimeout, bridge.operationTimeout)
	}
}

func TestBuildCreateAllocationRequest(t *testing.T) {
	run := &runv1.Run{
		AllocationID: "alloc-a",
		Attempt:      2,
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sh"},
			Env:  map[string]string{"RUN": "true"},
			Resources: &commonv1.ResourceSpec{
				Requests: &commonv1.ResourceQuantity{CpuMilli: 100, MemoryBytes: 1024},
				Limits:   &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 2048},
			},
		},
	}
	env := &environmentv1.Environment{
		ID: "env-a",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest:      "sha256:abc",
				Annotations: map[string]string{"org.opencontainers.image.ref.name": "docker.io/library/python@sha256:abc"},
			},
			DefaultEnv: map[string]string{"BASE": "true"},
		},
	}
	req := buildCreateAllocationRequestFromParams(createAllocationRequestParams{
		AllocationID:   run.GetAllocationID(),
		Attempt:        run.GetAttempt(),
		Config:         run.GetConfig(),
		Environment:    env,
		NodeID:         "node-a",
		DefaultRuntime: DefaultRuntime,
	})
	if req.GetAllocationID() != "alloc-a" || req.GetAttempt() != 2 || req.GetNodeID() != "node-a" {
		t.Fatalf("unexpected allocation identity: %+v", req)
	}
	if req.GetConfig().GetEnvironmentID() != "env-a" {
		t.Fatalf("environment id = %q", req.GetConfig().GetEnvironmentID())
	}
	if req.GetConfig().GetImageDescriptor() != "docker.io/library/python@sha256:abc" {
		t.Fatalf("image descriptor = %q", req.GetConfig().GetImageDescriptor())
	}
	if req.GetConfig().GetEnv()["BASE"] != "true" || req.GetConfig().GetEnv()["RUN"] != "true" {
		t.Fatalf("env merge failed: %+v", req.GetConfig().GetEnv())
	}
}

func TestBuildResolvedExecutionConfigAppliesRuntimeDefaults(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-b",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDefaultArgv: []string{"/bin/image-default"},
			DefaultCwd:       "/workspace",
			RootfsReadonly:   true,
			DefaultEnv:       map[string]string{"BASE": "true"},
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:def",
			},
			Mounts: []*catalogv1.RuntimeMount{{
				Type:    "bind",
				Source:  "/data",
				Target:  "/mnt/data",
				Options: []string{"ro"},
			}},
			ExecutionProfile: &catalogv1.RuntimeExecutionProfile{
				RuntimeBaseline: &catalogv1.RuntimeBaselinePolicy{NoFileLimit: 2097152},
				Capabilities: &catalogv1.RuntimeCapabilityPolicy{
					AnnotationKey:  "custom-capabilities",
					IncludeAmbient: proto.Bool(false),
				},
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config:         &commonv1.ExecutionConfig{},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
	})
	if cfg.GetRuntimeClass() != "runsc" {
		t.Fatalf("runtime class = %q, want runsc", cfg.GetRuntimeClass())
	}
	if len(cfg.GetArgv()) != 0 {
		t.Fatalf("argv = %#v, want empty so image entrypoint/cmd is preserved", cfg.GetArgv())
	}
	if cfg.GetCwd() != "" {
		t.Fatalf("cwd = %q, want empty so image working dir is preserved", cfg.GetCwd())
	}
	if !cfg.GetRootfsReadonly() {
		t.Fatal("rootfs_readonly = false, want true")
	}
	if len(cfg.GetMounts()) != 1 || cfg.GetMounts()[0].GetTarget() != "/mnt/data" {
		t.Fatalf("mounts = %#v, want template mount propagated", cfg.GetMounts())
	}
	if cfg.GetExecutionProfile().GetRuntimeBaseline().GetNoFileLimit() != 2097152 {
		t.Fatalf("execution profile nofile = %d, want 2097152", cfg.GetExecutionProfile().GetRuntimeBaseline().GetNoFileLimit())
	}
	if cfg.GetExecutionProfile().GetCapabilities().GetIncludeAmbient() {
		t.Fatal("execution profile include_ambient = true, want false")
	}
}

func TestBuildResolvedExecutionConfigLeavesServiceArgvEmpty(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-service-entrypoint",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDefaultArgv: []string{"/bin/image-default"},
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:entrypoint",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config:         &commonv1.ExecutionConfig{},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
	})
	if len(cfg.GetArgv()) != 0 {
		t.Fatalf("argv = %#v, want empty so image entrypoint/cmd is preserved", cfg.GetArgv())
	}
	if cfg.GetCwd() != "" {
		t.Fatalf("cwd = %q, want empty so image working dir is preserved", cfg.GetCwd())
	}
}

func TestBuildResolvedExecutionConfigPreservesImageCwdWithExplicitArgv(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-image-cwd",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			DefaultCwd: "/workspace",
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:cwd",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"python3"},
		},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
	})
	if got := cfg.GetArgv(); len(got) != 1 || got[0] != "python3" {
		t.Fatalf("argv = %#v, want explicit command", got)
	}
	if cfg.GetCwd() != "" {
		t.Fatalf("cwd = %q, want empty so image working dir is preserved for explicit argv", cfg.GetCwd())
	}
}

func TestBuildResolvedExecutionConfigUsesExplicitCwd(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-explicit-cwd",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			DefaultCwd: "/workspace",
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:cwd-explicit",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"python3"},
			Cwd:  " /tmp ",
		},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
	})
	if cfg.GetCwd() != "/tmp" {
		t.Fatalf("cwd = %q, want explicit cwd", cfg.GetCwd())
	}
}

func TestBuildResolvedExecutionConfigIncludesServiceNodeVolumes(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-volume",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:volume",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		AllocationID:   "alloc-volume",
		Namespace:      "default",
		ServiceID:      "svc-volume",
		Config:         &commonv1.ExecutionConfig{},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
		NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{{
			ClaimID:       "default/svc-volume/data",
			BindingID:     "alloc-volume/data",
			VolumeID:      "data",
			BackendHandle: "data",
			Backend:       storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
			AccessMode:    storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
			Target:        "/var/lib/app",
			Readonly:      true,
			Options:       []string{"rbind", "nodev", "ro"},
			RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
				SupportsRunsc: true,
			},
		}},
	})
	volumes := cfg.GetNodeVolumes()
	if len(volumes) != 1 {
		t.Fatalf("node volumes = %#v, want one service node volume", volumes)
	}
	got := volumes[0]
	if got.GetBackend() != storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL || got.GetVolumeID() != "data" || got.GetTarget() != "/var/lib/app" {
		t.Fatalf("node volume = %#v, want local node volume", got)
	}
	if got.GetOptions()[0] != "rbind" || got.GetOptions()[len(got.GetOptions())-1] != "ro" {
		t.Fatalf("node volume options = %#v, want rbind ... ro", got.GetOptions())
	}
	if got.GetRuntimeCompatibility() == nil || !got.GetRuntimeCompatibility().GetSupportsRunsc() {
		t.Fatalf("runtime compatibility = %#v, want runsc support", got.GetRuntimeCompatibility())
	}
}

func TestBuildResolvedExecutionConfigIncludesImageMounts(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-image-mount",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:image-mount",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			ImageMounts: []*commonv1.ImageMount{{
				Image:  "example.com/axern/codex-tool:latest",
				Target: "/opt/axern/tools/codex",
			}},
		},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
	})
	mounts := cfg.GetImageMounts()
	if len(mounts) != 1 {
		t.Fatalf("image mounts = %#v, want one image mount", mounts)
	}
	got := mounts[0]
	if got.GetImage() != "example.com/axern/codex-tool:latest" || got.GetTarget() != "/opt/axern/tools/codex" || !got.GetReadonly() {
		t.Fatalf("image mount = %#v, want readonly codex tool mount", got)
	}
}

func TestBuildResolvedExecutionConfigForImageBackedEnvironment(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-image",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			RootfsReadonly: true,
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest:      "sha256:image",
				Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/nginx:1.27"},
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv:         []string{"/bin/sh", "-c", "sleep 60"},
			RuntimeClass: "runc",
		},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
	})
	if cfg.GetEnvironmentID() != "env-image" {
		t.Fatalf("environment id = %q, want env-image", cfg.GetEnvironmentID())
	}
	if cfg.GetImageDigest() != "sha256:image" {
		t.Fatalf("image digest = %q, want sha256:image", cfg.GetImageDigest())
	}
	if cfg.GetImageDescriptor() != "index.docker.io/library/nginx:1.27" {
		t.Fatalf("image descriptor = %q, want index.docker.io/library/nginx:1.27", cfg.GetImageDescriptor())
	}
	if cfg.GetRuntimeClass() != "runc" {
		t.Fatalf("runtime class = %q, want runc", cfg.GetRuntimeClass())
	}
	if !cfg.GetRootfsReadonly() {
		t.Fatal("rootfs_readonly = false, want true")
	}
}

func TestBuildResolvedExecutionConfigIncludesReadinessProbe(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-readiness",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:ready",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/service"},
		},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
		ReadinessProbe: &servicev1.ServiceProbe{
			Action: &servicev1.ServiceProbe_Http{
				Http: &servicev1.HttpProbe{
					Port:   8080,
					Path:   "/readyz",
					Scheme: servicev1.HttpProbeScheme_HTTP_PROBE_SCHEME_HTTPS,
				},
			},
			Period:           durationpb.New(5 * time.Second),
			Timeout:          durationpb.New(2 * time.Second),
			SuccessThreshold: 1,
			FailureThreshold: 1,
		},
	})

	if cfg.GetReadinessProbe() == nil || cfg.GetReadinessProbe().GetHttp() == nil {
		t.Fatalf("readiness probe = %#v, want resolved HTTP probe", cfg.GetReadinessProbe())
	}
	if cfg.GetReadinessProbe().GetHttp().GetPort() != 8080 || cfg.GetReadinessProbe().GetHttp().GetPath() != "/readyz" {
		t.Fatalf("unexpected readiness probe %#v", cfg.GetReadinessProbe())
	}
	if cfg.GetReadinessProbe().GetHttp().GetScheme().String() != "HTTP_PROBE_SCHEME_HTTPS" {
		t.Fatalf("unexpected readiness probe scheme %#v", cfg.GetReadinessProbe().GetHttp().GetScheme())
	}
}

func TestResolveExecutionSecretsReturnsContextualErrors(t *testing.T) {
	_, err := resolveExecutionSecrets(context.Background(), stubSecretResolver{}, stubSecretResolver{}, &commonv1.ExecutionConfig{
		SecretEnv: []*commonv1.SecretEnvVar{{Name: "TOKEN", SecretID: "sec-missing", Key: "token"}},
	}, &environmentv1.Environment{})
	if err == nil || !strings.Contains(err.Error(), `config.secret_env "TOKEN" references secret "sec-missing"`) {
		t.Fatalf("err = %v, want contextual secret_env message", err)
	}
}

func TestBuildResolvedExecutionConfigIncludesServiceIdentity(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-service",
		ResolvedTemplate: &catalogv1.RuntimeTemplate{
			ImageDescriptor: &catalogv1.OciImageDescriptor{
				Digest: "sha256:service",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config:         &commonv1.ExecutionConfig{Argv: []string{"/bin/service"}},
		Environment:    env,
		DefaultRuntime: DefaultRuntime,
		Namespace:      "default",
		ServiceID:      "svc-123",
	})
	if cfg.GetNamespace() != "default" || cfg.GetServiceID() != "svc-123" {
		t.Fatalf("service identity = namespace:%q service_id:%q, want default/svc-123", cfg.GetNamespace(), cfg.GetServiceID())
	}
}

func TestFormatCreateAllocationErrorExplainsReadonlyRootfsTarget(t *testing.T) {
	cause := errors.New(`rpc error: code = Internal desc = node start failed: Failed to validate mount targets: mount target "/var/lib/app" does not exist in readonly rootfs`)
	err := formatCreateAllocationError(cause)
	if err == nil {
		t.Fatal("formatCreateAllocationError() = nil, want explanation")
	}
	if got := err.Error(); got != `mount target "/var/lib/app" does not exist in the readonly image rootfs; use an existing image path or disable readonly rootfs` {
		t.Fatalf("formatted error = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("formatted error should unwrap original cause")
	}
}

func TestBuildCreateAllocationRequestFromInputIncludesServiceIdentity(t *testing.T) {
	req := buildCreateAllocationRequestFromParams(createAllocationRequestParams{
		AllocationID:   "alloc-service",
		Attempt:        1,
		Config:         &commonv1.ExecutionConfig{Argv: []string{"/bin/service"}},
		Environment:    &environmentv1.Environment{ID: "env-service", ResolvedTemplate: &catalogv1.RuntimeTemplate{ImageDescriptor: &catalogv1.OciImageDescriptor{Digest: "sha256:svc"}}},
		NodeID:         "node-a",
		DefaultRuntime: DefaultRuntime,
		Namespace:      "default",
		ServiceID:      "svc-456",
	})
	if req.GetConfig().GetNamespace() != "default" || req.GetConfig().GetServiceID() != "svc-456" {
		t.Fatalf("wire config identity = namespace:%q service_id:%q, want default/svc-456", req.GetConfig().GetNamespace(), req.GetConfig().GetServiceID())
	}
}

func TestBuildCreateAllocationRequestResolvesServiceIdentityThroughBridge(t *testing.T) {
	client := &captureLifecycleClient{}
	bridge := New(client, Config{DefaultRuntime: DefaultRuntime})
	result, err := bridge.CreateResolvedAllocation(context.Background(), servicekernel.CreateResolvedAllocationRequest{
		Target:       "127.0.0.1:25000",
		Namespace:    "default",
		ServiceID:    "svc-bridge",
		AllocationID: "alloc-bridge",
		Attempt:      1,
		Config:       &commonv1.ExecutionConfig{Argv: []string{"/bin/service"}},
		Environment: &environmentv1.Environment{
			ID: "env-bridge",
			ResolvedTemplate: &catalogv1.RuntimeTemplate{
				ImageDescriptor: &catalogv1.OciImageDescriptor{Digest: "sha256:bridge"},
			},
		},
		NodeID: "node-a",
	})
	if err != nil {
		t.Fatalf("CreateResolvedAllocation() error = %v", err)
	}
	if client.lastCreate == nil {
		t.Fatal("last create request = nil, want captured request")
	}
	if client.lastCreate.GetConfig().GetNamespace() != "default" || client.lastCreate.GetConfig().GetServiceID() != "svc-bridge" {
		t.Fatalf("captured config identity = namespace:%q service_id:%q, want default/svc-bridge", client.lastCreate.GetConfig().GetNamespace(), client.lastCreate.GetConfig().GetServiceID())
	}
	if result.WorkspacePreparation.GetPayloadFormat() != "nydus" {
		t.Fatalf("workspace preparation = %#v", result.WorkspacePreparation)
	}
	if len(result.AdmittedCapabilityDependencies) != 1 || result.AdmittedCapabilityDependencies[0].GetSelectedEvidence().GetEvidenceID() != "create-evidence" {
		t.Fatalf("admitted capability dependencies = %#v", result.AdmittedCapabilityDependencies)
	}
}

func TestDeleteAllocationTreatsNodeNotFoundAsReleased(t *testing.T) {
	bridge := New(&captureLifecycleClient{deleteErr: grpcstatus.Error(codes.NotFound, "not found")}, Config{})
	if err := bridge.DeleteAllocation(context.Background(), "node-a:24010", "alloc-missing", 1, "node-a"); err != nil {
		t.Fatalf("DeleteAllocation() error = %v, want nil for node not found", err)
	}
}

func TestDeleteAllocationUsesGraceTimeout(t *testing.T) {
	client := &captureLifecycleClient{}
	bridge := New(client, Config{})
	if err := bridge.DeleteAllocation(context.Background(), "node-a:24010", "alloc-a", 2, "node-a"); err != nil {
		t.Fatalf("DeleteAllocation() error = %v", err)
	}
	if client.lastDelete.GetTimeoutSeconds() != 10 {
		t.Fatalf("delete timeout = %d, want 10", client.lastDelete.GetTimeoutSeconds())
	}
}

func TestDeleteResolvedAllocationReturnsReleaseObservations(t *testing.T) {
	client := &captureLifecycleClient{
		deleteResp: &privatenodev1.DeleteAllocationResponse{
			VolumeReleaseObservations: []*privatestoragev1.VolumeReleaseObservation{{
				BindingID: "binding-1",
				Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
			}},
		},
	}
	bridge := New(client, Config{})
	observations, err := bridge.DeleteResolvedAllocation(context.Background(), "node-a:24010", "alloc-a", 2, "node-a")
	if err != nil {
		t.Fatalf("DeleteResolvedAllocation() error = %v", err)
	}
	if len(observations) != 1 || observations[0].GetBindingID() != "binding-1" {
		t.Fatalf("observations = %#v, want binding-1", observations)
	}
}

func TestAllocationDeletedUsesNodeStatus(t *testing.T) {
	bridge := New(&captureLifecycleClient{statusErr: grpcstatus.Error(codes.NotFound, "not found")}, Config{})
	deleted, err := bridge.AllocationDeleted(context.Background(), "node-a:24010", "alloc-a", 1, "node-a")
	if err != nil {
		t.Fatalf("AllocationDeleted() error = %v", err)
	}
	if !deleted {
		t.Fatal("AllocationDeleted() = false, want true for node not found")
	}
}

type stubSecretResolver struct{}

func (stubSecretResolver) Resolve(context.Context, string) (*secretkernel.ResolvedSecret, bool, error) {
	return nil, false, nil
}

func (stubSecretResolver) ResolveDockerConfigJSON(context.Context, string) (string, bool, error) {
	return "", false, nil
}

type captureLifecycleClient struct {
	lastCreate *privatenodev1.CreateAllocationRequest
	lastDelete *privatenodev1.DeleteAllocationRequest
	deleteResp *privatenodev1.DeleteAllocationResponse
	deleteErr  error
	statusErr  error
}

func (c *captureLifecycleClient) CreateAllocation(_ context.Context, _ string, req *privatenodev1.CreateAllocationRequest) (*privatenodev1.CreateAllocationResponse, error) {
	c.lastCreate = protoCloneCreateAllocationRequest(req)
	return &privatenodev1.CreateAllocationResponse{
		AllocationID: req.GetAllocationID(),
		Attempt:      req.GetAttempt(),
		WorkspacePreparation: &commonv1.WorkspacePreparationFacts{
			PayloadFormat: "nydus",
		},
		AdmittedCapabilityDependencies: []*capabilityv1.CapabilityDependency{{
			Key:              &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT}},
			SelectedEvidence: &capabilityv1.CapabilityEvidence{EvidenceID: "create-evidence"},
		}},
	}, nil
}

func (c *captureLifecycleClient) DeleteAllocation(_ context.Context, _ string, req *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error) {
	c.lastDelete = proto.Clone(req).(*privatenodev1.DeleteAllocationRequest)
	if c.deleteErr != nil {
		return nil, c.deleteErr
	}
	if c.deleteResp != nil {
		return proto.Clone(c.deleteResp).(*privatenodev1.DeleteAllocationResponse), nil
	}
	return &privatenodev1.DeleteAllocationResponse{}, nil
}

func (c *captureLifecycleClient) DeleteVolume(context.Context, string, *privatenodev1.DeleteVolumeRequest) (*privatenodev1.DeleteVolumeResponse, error) {
	return &privatenodev1.DeleteVolumeResponse{}, nil
}

func (c *captureLifecycleClient) GetAllocationStatus(context.Context, string, *privatenodev1.GetAllocationStatusRequest) (*privatenodev1.GetAllocationStatusResponse, error) {
	return &privatenodev1.GetAllocationStatusResponse{}, c.statusErr
}

func (c *captureLifecycleClient) Close() error {
	return nil
}

func protoCloneCreateAllocationRequest(in *privatenodev1.CreateAllocationRequest) *privatenodev1.CreateAllocationRequest {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*privatenodev1.CreateAllocationRequest)
}
