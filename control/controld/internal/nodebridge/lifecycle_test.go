package nodebridge

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest:      "sha256:abc",
				Annotations: map[string]string{"org.opencontainers.image.ref.name": "docker.io/library/python@sha256:abc"},
			},
			DefaultEnv: map[string]string{"BASE": "true"},
		},
	}
	req := buildCreateAllocationRequestFromParams(createAllocationRequestParams{
		AllocationID: run.GetAllocationID(),
		Config:       run.GetConfig(),
		Environment:  env,
		NodeID:       "node-a",
	})
	if req.GetAllocationID() != "alloc-a" || req.GetNodeID() != "node-a" {
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
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			ImageDefaultArgv: []string{"/bin/image-default"},
			DefaultCwd:       "/workspace",
			RootfsReadonly:   true,
			DefaultEnv:       map[string]string{"BASE": "true"},
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest: "sha256:def",
			},
			Mounts: []*environmentv1.EnvironmentMount{{
				Type:    "bind",
				Source:  "/data",
				Target:  "/mnt/data",
				Options: []string{"ro"},
			}},
			ExecutionProfile: &environmentv1.OciExecutionProfile{
				Baseline: &environmentv1.OciBaselinePolicy{NoFileLimit: 2097152},
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config:      &commonv1.ExecutionConfig{},
		Environment: env,
	})
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
	if cfg.GetExecutionProfile().GetBaseline().GetNoFileLimit() != 2097152 {
		t.Fatalf("execution profile nofile = %d, want 2097152", cfg.GetExecutionProfile().GetBaseline().GetNoFileLimit())
	}
}

func TestBuildResolvedExecutionConfigLeavesImageArgvEmpty(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-service-entrypoint",
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			ImageDefaultArgv: []string{"/bin/image-default"},
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest: "sha256:entrypoint",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config:      &commonv1.ExecutionConfig{},
		Environment: env,
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
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			DefaultCwd: "/workspace",
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest: "sha256:cwd",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"python3"},
		},
		Environment: env,
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
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			DefaultCwd: "/workspace",
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest: "sha256:cwd-explicit",
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"python3"},
			Cwd:  " /tmp ",
		},
		Environment: env,
	})
	if cfg.GetCwd() != "/tmp" {
		t.Fatalf("cwd = %q, want explicit cwd", cfg.GetCwd())
	}
}

func TestBuildResolvedExecutionConfigIncludesImageMounts(t *testing.T) {
	env := &environmentv1.Environment{
		ID: "env-image-mount",
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			ImageDescriptor: &environmentv1.OciImageDescriptor{
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
		Environment: env,
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
		ResolvedSpec: &environmentv1.ResolvedEnvironmentSpec{
			RootfsReadonly: true,
			ImageDescriptor: &environmentv1.OciImageDescriptor{
				Digest:      "sha256:image",
				Annotations: map[string]string{"org.opencontainers.image.ref.name": "index.docker.io/library/nginx:1.27"},
			},
		},
	}

	cfg := buildResolvedExecutionConfig(createAllocationRequestParams{
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sh", "-c", "sleep 60"},
		},
		Environment: env,
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
	if !cfg.GetRootfsReadonly() {
		t.Fatal("rootfs_readonly = false, want true")
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

func TestDeleteAllocationTreatsNodeNotFoundAsReleased(t *testing.T) {
	bridge := New(&captureLifecycleClient{deleteErr: grpcstatus.Error(codes.NotFound, "not found")}, Config{})
	if err := bridge.DeleteAllocation(context.Background(), "node-a:24010", "alloc-missing", "node-a", nil); err != nil {
		t.Fatalf("DeleteAllocation() error = %v, want nil for node not found", err)
	}
}

func TestDeleteAllocationUsesGraceTimeout(t *testing.T) {
	client := &captureLifecycleClient{}
	bridge := New(client, Config{})
	if err := bridge.DeleteAllocation(context.Background(), "node-a:24010", "alloc-a", "node-a", nil); err != nil {
		t.Fatalf("DeleteAllocation() error = %v", err)
	}
	if client.lastDelete.GetTimeoutSeconds() != 10 {
		t.Fatalf("delete timeout = %d, want 10", client.lastDelete.GetTimeoutSeconds())
	}
}

func TestAllocationDeletedUsesNodeStatus(t *testing.T) {
	bridge := New(&captureLifecycleClient{statusErr: grpcstatus.Error(codes.NotFound, "not found")}, Config{})
	deleted, err := bridge.AllocationDeleted(context.Background(), "node-a:24010", "alloc-a", "node-a")
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

func (c *captureLifecycleClient) GetAllocationLifecycle(context.Context, string, *privatenodev1.GetAllocationLifecycleRequest) (*privatenodev1.GetAllocationLifecycleResponse, error) {
	return &privatenodev1.GetAllocationLifecycleResponse{}, c.statusErr
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
