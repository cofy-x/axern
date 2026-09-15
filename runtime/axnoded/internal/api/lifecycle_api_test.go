package api

import (
	"context"
	"strings"
	"testing"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type fakeNodeLifecycleService struct {
	startRequests        []*runtimev1.StartRequest
	deleteRequests       []*runtimev1.DeleteRequest
	listRequests         []*runtimev1.ListContainersRequest
	deleted              map[string]bool
	keepDeletedVisible   bool
	deleteErr            error
	admittedDependencies []*capabilityv1.CapabilityRequirement
	startResponseID      string
	controlPlaneIDs      map[string]bool
}

func (f *fakeNodeLifecycleService) Start(ctx context.Context, req *runtimev1.StartRequest) (*runtimev1.StartResponse, error) {
	return f.StartControlPlaneAllocation(ctx, "", req)
}

func (f *fakeNodeLifecycleService) Delete(ctx context.Context, req *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error) {
	return f.DeleteControlPlaneAllocation(ctx, "", req)
}

func (f *fakeNodeLifecycleService) ReconcileAllocationCapabilities(context.Context, string) ([]*capabilityv1.CapabilityRequirement, *capabilityv1.CapabilityConditionSet, error) {
	return cloneCapabilityRequirements(f.admittedDependencies), nil, nil
}

func (f *fakeNodeLifecycleService) StartControlPlaneAllocation(ctx context.Context, _ string, req *runtimev1.StartRequest) (*runtimev1.StartResponse, error) {
	_ = ctx
	f.startRequests = append(f.startRequests, req)
	responseID := req.GetAllocationID()
	if f.startResponseID != "" {
		responseID = f.startResponseID
	}
	return &runtimev1.StartResponse{AllocationID: responseID}, nil
}

func TestNodeLifecycleCreateAllocationRejectsDifferentExecutionIdentity(t *testing.T) {
	t.Parallel()

	server := NewNodeLifecycleServer(&fakeNodeLifecycleService{startResponseID: "container-alias"}, "node-a")
	_, err := server.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID:             "alloc-123",
		NodeID:                   "node-a",
		ExecutionLeaseTtlSeconds: 30,
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			ImageDescriptor: "example.com/runtime:latest",
		},
	})
	if grpcstatus.Code(err) != codes.Internal {
		t.Fatalf("CreateAllocation() code = %v, want internal", grpcstatus.Code(err))
	}
}

func TestNodeLifecycleCreateAllocationRequiresLeaseOnlyForControlPlaneBinding(t *testing.T) {
	t.Parallel()

	localService := &fakeNodeLifecycleService{}
	localServer := NewLocalNodeLifecycleServer(localService, "node-a")
	if _, err := localServer.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID: "local-conformance",
		Config:       &nodelifecyclev1.ResolvedExecutionConfig{ImageDescriptor: "example.com/runtime:latest"},
	}); err != nil {
		t.Fatalf("local CreateAllocation() error = %v", err)
	}
	if _, err := localServer.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID:             "local-with-lease",
		ExecutionLeaseTtlSeconds: 30,
		Config:                   &nodelifecyclev1.ResolvedExecutionConfig{ImageDescriptor: "example.com/runtime:latest"},
	}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("local CreateAllocation() with lease code = %v, want invalid argument", grpcstatus.Code(err))
	}

	boundServer := NewNodeLifecycleServer(&fakeNodeLifecycleService{}, "node-a")
	if _, err := boundServer.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID: "remote-without-binding",
		Config:       &nodelifecyclev1.ResolvedExecutionConfig{ImageDescriptor: "example.com/runtime:latest"},
	}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("remote CreateAllocation() without binding code = %v, want invalid argument", grpcstatus.Code(err))
	}
	for _, ttl := range []int64{0, 31} {
		_, err := boundServer.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
			AllocationID:             "bound-invalid-lease",
			NodeID:                   "node-a",
			ExecutionLeaseTtlSeconds: ttl,
			Config:                   &nodelifecyclev1.ResolvedExecutionConfig{ImageDescriptor: "example.com/runtime:latest"},
		})
		if grpcstatus.Code(err) != codes.InvalidArgument {
			t.Fatalf("bound CreateAllocation() ttl %d code = %v, want invalid argument", ttl, grpcstatus.Code(err))
		}
	}
}

func TestLocalNodeLifecycleCannotOperateControlPlaneAllocation(t *testing.T) {
	t.Parallel()
	service := &fakeNodeLifecycleService{controlPlaneIDs: map[string]bool{"alloc-bound": true}}
	server := NewLocalNodeLifecycleServer(service, "node-a")

	_, err := server.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID: "alloc-bound",
		Config:       &nodelifecyclev1.ResolvedExecutionConfig{ImageDescriptor: "example.com/runtime:latest"},
	})
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("CreateAllocation() code = %v, want permission denied", grpcstatus.Code(err))
	}
	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{AllocationID: "alloc-bound"}); grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("DeleteAllocation() code = %v, want permission denied", grpcstatus.Code(err))
	}
	if _, err := server.GetAllocationLifecycle(context.Background(), &nodelifecyclev1.GetAllocationLifecycleRequest{AllocationID: "alloc-bound"}); grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("GetAllocationLifecycle() code = %v, want permission denied", grpcstatus.Code(err))
	}
}

func (f *fakeNodeLifecycleService) DeleteControlPlaneAllocation(ctx context.Context, _ string, req *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error) {
	_ = ctx
	f.deleteRequests = append(f.deleteRequests, req)
	if f.deleteErr != nil {
		return nil, f.deleteErr
	}
	if !f.keepDeletedVisible {
		if f.deleted == nil {
			f.deleted = make(map[string]bool)
		}
		f.deleted[req.GetID()] = true
	}
	return &runtimev1.DeleteResponse{}, nil
}

func (f *fakeNodeLifecycleService) HasControlPlaneAllocation(allocationID, _ string) bool {
	return strings.TrimSpace(allocationID) != ""
}

func (f *fakeNodeLifecycleService) IsControlPlaneAllocation(allocationID string) bool {
	return f.controlPlaneIDs[strings.TrimSpace(allocationID)]
}

func (f *fakeNodeLifecycleService) List(ctx context.Context, req *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error) {
	_ = ctx
	f.listRequests = append(f.listRequests, req)
	if f.deleted[req.GetID()] {
		return nil, grpcstatus.Error(codes.NotFound, "container not found")
	}
	return &runtimev1.ListContainersResponse{
		Containers: []*runtimev1.ContainerStatus{
			{
				ID:             req.GetID(),
				State:          runtimev1.ContainerState_CONTAINER_EXITED,
				ExitCode:       func() *int32 { value := int32(23); return &value }(),
				Message:        "done",
				DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED,
			},
		},
	}, nil
}

func TestNodeLifecycleCreateAllocationBridgesRequest(t *testing.T) {
	t.Parallel()

	const imageRef = "axern/python311-runtime:dev"
	fakeService := &fakeNodeLifecycleService{
		admittedDependencies: []*capabilityv1.CapabilityRequirement{{
			Key: &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{
				Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
			}},
		}},
	}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	resp, err := server.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID:             "alloc-123",
		NodeID:                   "node-a",
		ExecutionLeaseTtlSeconds: 30,
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			ImageDescriptor: imageRef,
			Argv:            []string{"/bin/sh", "-lc", "sleep 3600"},
			Cwd:             "/workspace",
			Env:             map[string]string{"A": "B"},
			Resources: &commonv1.ResourceSpec{
				Requests: &commonv1.ResourceQuantity{CpuMilli: 250, MemoryBytes: 134217728},
				Limits:   &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 268435456},
			},
			Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, EgressPolicy: &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: []string{"github.com"}}}}},
			ImageMounts: []*nodelifecyclev1.ImageMount{{
				Image:  "example.com/axern/codex-tool:latest",
				Target: "/opt/axern/tools/codex",
			}},
			SecretEnv: []*nodelifecyclev1.ResolvedSecretEnvVar{{Name: "TOKEN", Value: "secret-value"}},
			SecretFiles: []*nodelifecyclev1.ResolvedSecretFile{{
				Path: "/run/secrets/key", Content: []byte("secret-content"), Mode: 0o400,
			}},
			RegistryCredential: &nodelifecyclev1.RegistryCredential{DockerConfigJson: `{"auths":{}}`},
			ExecutionProfile: &environmentv1.OciExecutionProfile{
				Baseline: &environmentv1.OciBaselinePolicy{NoFileLimit: 2097152},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateAllocation() error = %v", err)
	}
	if resp.GetAllocationID() != "alloc-123" {
		t.Fatalf("allocation response = %#v", resp)
	}
	if len(fakeService.startRequests) != 1 {
		t.Fatalf("start request count = %d, want 1", len(fakeService.startRequests))
	}
	startReq := fakeService.startRequests[0]
	if startReq.GetAllocationID() != "alloc-123" {
		t.Fatalf("container id = %q, want alloc-123", startReq.GetAllocationID())
	}
	if got := startReq.GetNetwork().GetEgressPolicy().GetDnsDeny().GetDeniedDomains(); len(got) != 1 || got[0] != "github.com" {
		t.Fatalf("egress policy was not preserved: %#v", startReq.GetNetwork().GetEgressPolicy())
	}
	if startReq.GetEnvironment().GetRootfs().GetImageUrl() != imageRef {
		t.Fatalf("image_ref = %q", startReq.GetEnvironment().GetRootfs().GetImageUrl())
	}
	if startReq.GetEnvironment().GetEnv()["A"] != "B" {
		t.Fatalf("runtime env = %#v, want key A", startReq.GetEnvironment().GetEnv())
	}
	if startReq.GetResources().GetRequests().GetCpuMilli() != 250 {
		t.Fatalf("resources = %#v, want request CPU 250", startReq.GetResources())
	}
	if startReq.GetResources().GetLimits().GetMemoryBytes() != 268435456 {
		t.Fatalf("resources = %#v, want memory limit", startReq.GetResources())
	}
	if got := startReq.GetImageMounts(); len(got) != 1 || got[0].GetImage() != "example.com/axern/codex-tool:latest" || got[0].GetTarget() != "/opt/axern/tools/codex" || !got[0].GetReadonly() {
		t.Fatalf("image mounts = %#v, want readonly codex tool mount", got)
	}
	if got := startReq.GetSecretEnv(); len(got) != 1 || got[0].GetName() != "TOKEN" || got[0].GetValue() != "secret-value" {
		t.Fatalf("resolved secret env was not preserved: %#v", got)
	}
	if got := startReq.GetSecretFiles(); len(got) != 1 || got[0].GetPath() != "/run/secrets/key" || string(got[0].GetContent()) != "secret-content" || got[0].GetMode() != 0o400 {
		t.Fatalf("resolved secret file was not preserved: %#v", got)
	}
	if got := startReq.GetRegistryCredential().GetDockerConfigJson(); got != `{"auths":{}}` {
		t.Fatalf("registry credential was not preserved")
	}
	if startReq.GetEnvironment().GetExecutionProfile().GetBaseline().GetNoFileLimit() != 2097152 {
		t.Fatalf("execution profile nofile = %d, want 2097152", startReq.GetEnvironment().GetExecutionProfile().GetBaseline().GetNoFileLimit())
	}
}

func TestNodeLifecycleCreateAllocationAllowsImageDefaultCommand(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	_, err := server.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID:             "alloc-image-default",
		NodeID:                   "node-a",
		ExecutionLeaseTtlSeconds: 30,
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			ImageDescriptor: "docker.io/library/nginx:1.27",
			Cwd:             "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateAllocation() error = %v", err)
	}
	if len(fakeService.startRequests) != 1 {
		t.Fatalf("start request count = %d, want 1", len(fakeService.startRequests))
	}
	if got := fakeService.startRequests[0].GetEnvironment().GetArgv(); len(got) != 0 {
		t.Fatalf("command = %#v, want empty so OCI image default command is preserved", got)
	}
}

func TestNodeLifecycleDeleteAllocationBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	_, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	})
	if err != nil {
		t.Fatalf("DeleteAllocation() error = %v", err)
	}
	if len(fakeService.deleteRequests) != 1 {
		t.Fatalf("delete request count = %d, want 1", len(fakeService.deleteRequests))
	}
	if fakeService.deleteRequests[0].GetID() != "alloc-123" || fakeService.deleteRequests[0].GetTimeout() != 9 {
		t.Fatalf("delete request = %#v", fakeService.deleteRequests[0])
	}
	if len(fakeService.listRequests) != 1 || fakeService.listRequests[0].GetID() != "alloc-123" {
		t.Fatalf("delete confirmation list requests = %#v, want alloc-123", fakeService.listRequests)
	}
	_, err = server.GetAllocationLifecycle(context.Background(), &nodelifecyclev1.GetAllocationLifecycleRequest{
		AllocationID: "alloc-123",
		NodeID:       "node-a",
	})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetAllocationLifecycle() after delete code = %v, want not found", grpcstatus.Code(err))
	}
	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	}); err != nil {
		t.Fatalf("second DeleteAllocation() error = %v, want idempotent success", err)
	}
	if len(fakeService.deleteRequests) != 2 {
		t.Fatalf("delete request count = %d, want one authoritative cleanup attempt per request", len(fakeService.deleteRequests))
	}
}

func TestNodeLifecycleDeleteAllocationFailsWhenTargetStillExists(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{keepDeletedVisible: true}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	_, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	})
	if grpcstatus.Code(err) != codes.Unavailable {
		t.Fatalf("DeleteAllocation() code = %v, want unavailable", grpcstatus.Code(err))
	}
}

func TestNodeLifecycleDeleteAllocationIsIdempotentWhenRuntimeTargetIsMissing(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{deleteErr: grpcstatus.Error(codes.NotFound, "not found")}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	}); err != nil {
		t.Fatalf("DeleteAllocation() error = %v, want nil for missing runtime target", err)
	}
	if len(fakeService.listRequests) != 0 {
		t.Fatalf("delete confirmation list requests = %#v, want none after runtime not found", fakeService.listRequests)
	}
	if got := fakeService.deleteRequests[0].GetID(); got != "alloc-123" {
		t.Fatalf("delete target = %q, want allocation id", got)
	}
}

func TestNodeLifecycleDeleteAllocationIsIdempotentWhenDurableAllocationIsAbsent(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{deleteErr: grpcstatus.Error(codes.NotFound, "not found")}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-released-before-restart",
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	}); err != nil {
		t.Fatalf("DeleteAllocation() error = %v, want idempotent success for absent allocation", err)
	}
	if len(fakeService.deleteRequests) != 1 {
		t.Fatalf("delete request count = %d, want cleanup attempt", len(fakeService.deleteRequests))
	}
	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID: "alloc-released-before-restart",
		NodeID:       "node-a",
	}); err != nil {
		t.Fatalf("second DeleteAllocation() error = %v, want idempotent success", err)
	}
	if len(fakeService.deleteRequests) != 2 {
		t.Fatalf("delete request count = %d, want one authoritative cleanup attempt per request", len(fakeService.deleteRequests))
	}
}

func TestNodeLifecycleGetAllocationLifecycleReturnsNotFoundWhenRuntimeAllocationIsAbsent(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{deleted: map[string]bool{"alloc-released-before-restart": true}}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	_, err := server.GetAllocationLifecycle(context.Background(), &nodelifecyclev1.GetAllocationLifecycleRequest{
		AllocationID: "alloc-released-before-restart",
		NodeID:       "node-a",
	})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetAllocationLifecycle() code = %v, want not found", grpcstatus.Code(err))
	}
	if len(fakeService.listRequests) != 1 {
		t.Fatalf("list request count = %d, want one authoritative runtime lookup", len(fakeService.listRequests))
	}
}

func TestAllocationEnvironmentIDUsesOnlyStaticExecutionTemplate(t *testing.T) {
	base := &nodelifecyclev1.CreateAllocationRequest{
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			EnvironmentID:   "env-a",
			ImageDescriptor: "image-a",
			Argv:            []string{"/bin/app"},
		},
	}
	other := &nodelifecyclev1.CreateAllocationRequest{
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			EnvironmentID:   "env-a",
			ImageDescriptor: "image-a",
			Argv:            []string{"/bin/app"},
		},
	}
	baseStart, err := allocationStartRequest(base)
	if err != nil {
		t.Fatalf("allocationStartRequest(base) error = %v", err)
	}
	otherStart, err := allocationStartRequest(other)
	if err != nil {
		t.Fatalf("allocationStartRequest(other) error = %v", err)
	}
	if baseStart.GetEnvironment().GetID() != otherStart.GetEnvironment().GetID() {
		t.Fatal("request identity must not partition the environment template cache")
	}

	other.GetConfig().Argv = []string{"/bin/other"}
	otherStart, err = allocationStartRequest(other)
	if err != nil {
		t.Fatalf("allocationStartRequest(other static config) error = %v", err)
	}
	if baseStart.GetEnvironment().GetID() == otherStart.GetEnvironment().GetID() {
		t.Fatal("static execution config must partition the environment template cache")
	}
}

func TestStableResolvedEnvironmentIDFingerprintsStaticTemplate(t *testing.T) {
	base := &runtimev1.ResolvedEnvironment{
		ID:   "ignored",
		Argv: []string{"/bin/app"},
		Cwd:  "/workspace",
		Env:  map[string]string{"B": "2", "A": "1"},
		Rootfs: &runtimev1.RootfsConfig{
			Type:     runtimev1.RootfsSrcType_IMAGE,
			Source:   &runtimev1.RootfsConfig_ImageUrl{ImageUrl: "registry/app@sha256:abc"},
			Readonly: true,
		},
	}
	baseID := stableResolvedEnvironmentID(base)
	if baseID == "" {
		t.Fatal("stable environment template id must not be empty")
	}

	reordered := proto.Clone(base).(*runtimev1.ResolvedEnvironment)
	reordered.ID = "another-id"
	reordered.Env = map[string]string{"A": "1", "B": "2"}
	if got := stableResolvedEnvironmentID(reordered); got != baseID {
		t.Fatalf("map order and existing id must not affect fingerprint: got %q, want %q", got, baseID)
	}

	tests := map[string]func(*runtimev1.ResolvedEnvironment){
		"command": func(template *runtimev1.ResolvedEnvironment) { template.Argv = []string{"/bin/other"} },
		"cwd":     func(template *runtimev1.ResolvedEnvironment) { template.Cwd = "/app" },
		"environment": func(template *runtimev1.ResolvedEnvironment) {
			template.Env["A"] = "changed"
		},
		"rootfs": func(template *runtimev1.ResolvedEnvironment) {
			template.Rootfs.Source = &runtimev1.RootfsConfig_ImageUrl{ImageUrl: "registry/app@sha256:def"}
		},
		"rootfs readonly": func(template *runtimev1.ResolvedEnvironment) { template.Rootfs.Readonly = false },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := proto.Clone(base).(*runtimev1.ResolvedEnvironment)
			mutate(candidate)
			if got := stableResolvedEnvironmentID(candidate); got == baseID {
				t.Fatalf("static template change must alter fingerprint: %q", got)
			}
		})
	}
}

func TestNodeLifecycleGetAllocationLifecycleMapsState(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{}
	server := NewNodeLifecycleServer(fakeService, "node-a")

	resp, err := server.GetAllocationLifecycle(context.Background(), &nodelifecyclev1.GetAllocationLifecycleRequest{AllocationID: "alloc-123", NodeID: "node-a"})
	if err != nil {
		t.Fatalf("GetAllocationLifecycle() error = %v", err)
	}
	if resp.GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
		t.Fatalf("status = %v, want EXITED", resp.GetState())
	}
	if resp.ExitCode == nil || resp.GetExitCode() != 23 {
		t.Fatalf("response = %#v", resp)
	}
	if resp.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED {
		t.Fatalf("diagnostic_code = %v, want MEMORY_LIMIT_EXCEEDED", resp.GetDiagnosticCode())
	}
}
