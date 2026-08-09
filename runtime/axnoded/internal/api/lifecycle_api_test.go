package api

import (
	"context"
	"strings"
	"testing"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
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
	releaseObservations  []*privatestoragev1.VolumeReleaseObservation
	workspacePreparation *commonv1.WorkspacePreparationFacts
	admittedDependencies []*capabilityv1.CapabilityDependency
}

func (f *fakeNodeLifecycleService) DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error {
	return nil
}

func (f *fakeNodeLifecycleService) Start(ctx context.Context, req *runtimev1.StartRequest) (*runtimev1.StartResponse, error) {
	_ = ctx
	f.startRequests = append(f.startRequests, req)
	return &runtimev1.StartResponse{
		Code: 0, ID: req.GetContainerID(), Message: "ok",
		AdmittedCapabilityDependencies: cloneCapabilityDependencies(f.admittedDependencies),
	}, nil
}

func (f *fakeNodeLifecycleService) WorkspacePreparation(string) *commonv1.WorkspacePreparationFacts {
	return f.workspacePreparation
}

func (f *fakeNodeLifecycleService) Delete(ctx context.Context, req *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error) {
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
	return &runtimev1.DeleteResponse{
		VolumeReleaseObservations: cloneVolumeReleaseObservations(f.releaseObservations),
	}, nil
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
				ID:       req.GetID(),
				State:    runtimev1.ContainerState_CONTAINER_EXITED,
				ExitCode: 23,
				Message:  "done",
			},
		},
	}, nil
}

func TestNodeLifecycleCreateAllocationBridgesRequest(t *testing.T) {
	t.Parallel()

	const imageRef = "axern/python311-runtime:dev"
	fakeService := &fakeNodeLifecycleService{
		workspacePreparation: &commonv1.WorkspacePreparationFacts{
			PayloadFormat: "nydus",
			PayloadDigest: "sha256:payload",
			CacheHit:      true,
		},
		admittedDependencies: []*capabilityv1.CapabilityDependency{{
			Key: &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{
				Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
			}},
			SelectedEvidence: &capabilityv1.CapabilityEvidence{EvidenceID: "create-evidence"},
		}},
	}
	server := NewNodeLifecycleServer(fakeService, "node-a", NewAllocationTargetRegistry())

	resp, err := server.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID: "alloc-123",
		Attempt:      1,
		NodeID:       "node-a",
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			ImageDescriptor: imageRef,
			RuntimeClass:    "runsc",
			Namespace:       "default",
			ServiceID:       "svc-123",
			Argv:            []string{"/bin/sh", "-lc", "sleep 3600"},
			Cwd:             "/workspace",
			Env:             map[string]string{"A": "B"},
			Ports: []*commonv1.PortSpec{{
				Name:          "http",
				Protocol:      commonv1.PortProtocol_PORT_PROTOCOL_TCP,
				ContainerPort: 8080,
			}},
			Resources: &commonv1.ResourceSpec{
				Requests: &commonv1.ResourceQuantity{CpuMilli: 250, MemoryBytes: 134217728},
				Limits:   &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 268435456},
			},
			ImageMounts: []*nodelifecyclev1.ImageMount{{
				Image:  "example.com/axern/codex-tool:latest",
				Target: "/opt/axern/tools/codex",
			}},
			ExecutionProfile: &catalogv1.RuntimeExecutionProfile{
				RuntimeBaseline: &catalogv1.RuntimeBaselinePolicy{NoFileLimit: 2097152},
				Capabilities: &catalogv1.RuntimeCapabilityPolicy{
					AnnotationKey:  "custom-capabilities",
					IncludeAmbient: proto.Bool(false),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateAllocation() error = %v", err)
	}
	if resp.GetAllocationID() != "alloc-123" || resp.GetAttempt() != 1 {
		t.Fatalf("allocation response = %#v", resp)
	}
	if resp.GetWorkspacePreparation().GetPayloadFormat() != "nydus" || resp.GetWorkspacePreparation().GetPayloadDigest() != "sha256:payload" || !resp.GetWorkspacePreparation().GetCacheHit() {
		t.Fatalf("workspace preparation = %#v", resp.GetWorkspacePreparation())
	}
	if len(resp.GetAdmittedCapabilityDependencies()) != 1 || resp.GetAdmittedCapabilityDependencies()[0].GetSelectedEvidence().GetEvidenceID() != "create-evidence" {
		t.Fatalf("admitted capability dependencies = %#v", resp.GetAdmittedCapabilityDependencies())
	}
	if len(fakeService.startRequests) != 1 {
		t.Fatalf("start request count = %d, want 1", len(fakeService.startRequests))
	}
	startReq := fakeService.startRequests[0]
	if startReq.GetContainerID() != "alloc-123" {
		t.Fatalf("container id = %q, want alloc-123", startReq.GetContainerID())
	}
	if startReq.GetRuntimeTemplate().GetRootfs().GetImageUrl() != imageRef {
		t.Fatalf("image_ref = %q", startReq.GetRuntimeTemplate().GetRootfs().GetImageUrl())
	}
	if startReq.GetRuntimeTemplate().GetRuntimeEnvs()["A"] != "B" {
		t.Fatalf("runtime env = %#v, want key A", startReq.GetRuntimeTemplate().GetRuntimeEnvs())
	}
	if got := startReq.GetPorts(); len(got) != 1 || got[0] != "tcp:8080:8080" {
		t.Fatalf("ports = %#v, want tcp:8080:8080", got)
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
	if startReq.GetRuntimeTemplate().GetExecutionProfile().GetRuntimeBaseline().GetNoFileLimit() != 2097152 {
		t.Fatalf("execution profile nofile = %d, want 2097152", startReq.GetRuntimeTemplate().GetExecutionProfile().GetRuntimeBaseline().GetNoFileLimit())
	}
	if startReq.GetRuntimeTemplate().GetExecutionProfile().GetCapabilities().GetIncludeAmbient() {
		t.Fatal("execution profile include_ambient = true, want false")
	}
	if !strings.Contains(startReq.GetExtraConfig(), `"allocationAttempt":1`) {
		t.Fatalf("extra_config = %q, want allocationAttempt", startReq.GetExtraConfig())
	}
	if !strings.Contains(startReq.GetExtraConfig(), `"namespace":"default"`) || !strings.Contains(startReq.GetExtraConfig(), `"serviceId":"svc-123"`) {
		t.Fatalf("extra_config = %q, want service volume identity", startReq.GetExtraConfig())
	}
}

func TestNodeLifecycleCreateAllocationAllowsImageDefaultCommand(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{}
	server := NewNodeLifecycleServer(fakeService, "node-a", NewAllocationTargetRegistry())

	_, err := server.CreateAllocation(context.Background(), &nodelifecyclev1.CreateAllocationRequest{
		AllocationID: "alloc-image-default",
		Attempt:      1,
		NodeID:       "node-a",
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			ImageDescriptor: "docker.io/library/nginx:1.27",
			RuntimeClass:    "runsc",
			Cwd:             "/",
		},
	})
	if err != nil {
		t.Fatalf("CreateAllocation() error = %v", err)
	}
	if len(fakeService.startRequests) != 1 {
		t.Fatalf("start request count = %d, want 1", len(fakeService.startRequests))
	}
	if got := fakeService.startRequests[0].GetRuntimeTemplate().GetCommand(); len(got) != 0 {
		t.Fatalf("command = %#v, want empty so OCI image default command is preserved", got)
	}
}

func TestNodeLifecycleDeleteAllocationBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{}
	server := NewNodeLifecycleServer(fakeService, "node-a", NewAllocationTargetRegistry())

	fakeService.releaseObservations = []*privatestoragev1.VolumeReleaseObservation{{
		BindingID: "binding-1",
		Status:    storagev1.VolumeStatus_VOLUME_STATUS_DELETED,
	}}
	resp, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		Attempt:        1,
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	})
	if err != nil {
		t.Fatalf("DeleteAllocation() error = %v", err)
	}
	if got := resp.GetVolumeReleaseObservations(); len(got) != 1 || got[0].GetBindingID() != "binding-1" {
		t.Fatalf("release observations = %#v, want binding-1", got)
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
	_, err = server.GetAllocationStatus(context.Background(), &nodelifecyclev1.GetAllocationStatusRequest{
		AllocationID: "alloc-123",
		Attempt:      1,
		NodeID:       "node-a",
	})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetAllocationStatus() after delete code = %v, want not found", grpcstatus.Code(err))
	}
	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		Attempt:        1,
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	}); err != nil {
		t.Fatalf("second DeleteAllocation() error = %v, want nil after tombstone", err)
	}
	if len(fakeService.deleteRequests) != 1 {
		t.Fatalf("delete request count after tombstone = %d, want 1", len(fakeService.deleteRequests))
	}
}

func TestNodeLifecycleDeleteAllocationFailsWhenTargetStillExists(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{keepDeletedVisible: true}
	server := NewNodeLifecycleServer(fakeService, "node-a", NewAllocationTargetRegistry())

	_, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		Attempt:        1,
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
	targets := NewAllocationTargetRegistry()
	targets.bind("alloc-123", "axctl-runtime-id")
	server := NewNodeLifecycleServer(fakeService, "node-a", targets)

	if _, err := server.DeleteAllocation(context.Background(), &nodelifecyclev1.DeleteAllocationRequest{
		AllocationID:   "alloc-123",
		Attempt:        1,
		NodeID:         "node-a",
		TimeoutSeconds: 9,
	}); err != nil {
		t.Fatalf("DeleteAllocation() error = %v, want nil for missing runtime target", err)
	}
	if len(fakeService.listRequests) != 0 {
		t.Fatalf("delete confirmation list requests = %#v, want none after runtime not found", fakeService.listRequests)
	}
	if got := targets.resolve("alloc-123"); got != "alloc-123" {
		t.Fatalf("target resolve after delete = %q, want allocation id after unbind", got)
	}
	_, err := server.GetAllocationStatus(context.Background(), &nodelifecyclev1.GetAllocationStatusRequest{
		AllocationID: "alloc-123",
		Attempt:      1,
		NodeID:       "node-a",
	})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetAllocationStatus() after missing target delete code = %v, want not found", grpcstatus.Code(err))
	}
}

func TestAllocationRuntimeIDUsesOnlyStaticExecutionTemplate(t *testing.T) {
	base := &nodelifecyclev1.CreateAllocationRequest{
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			EnvironmentID:   "env-a",
			ImageDescriptor: "image-a",
			RuntimeClass:    "runsc",
			Argv:            []string{"/bin/app"},
			Namespace:       "default",
			ServiceID:       "svc-a",
			NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{{
				ClaimID:  "default/svc-a/data",
				VolumeID: "data",
				Backend:  storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
				Target:   "/data",
			}},
		},
	}
	other := &nodelifecyclev1.CreateAllocationRequest{
		Config: &nodelifecyclev1.ResolvedExecutionConfig{
			EnvironmentID:   "env-a",
			ImageDescriptor: "image-a",
			RuntimeClass:    "runsc",
			Argv:            []string{"/bin/app"},
			Namespace:       "default",
			ServiceID:       "svc-b",
			NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{{
				ClaimID:  "default/svc-b/data",
				VolumeID: "data",
				Backend:  storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
				Target:   "/data",
			}},
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
	if baseStart.GetRuntimeTemplate().GetID() != otherStart.GetRuntimeTemplate().GetID() {
		t.Fatal("request identity and dynamic volumes must not partition the runtime template cache")
	}

	other.GetConfig().Argv = []string{"/bin/other"}
	otherStart, err = allocationStartRequest(other)
	if err != nil {
		t.Fatalf("allocationStartRequest(other static config) error = %v", err)
	}
	if baseStart.GetRuntimeTemplate().GetID() == otherStart.GetRuntimeTemplate().GetID() {
		t.Fatal("static execution config must partition the runtime template cache")
	}
}

func TestStableRuntimeTemplateIDFingerprintsStaticTemplate(t *testing.T) {
	base := &runtimev1.RuntimeTemplate{
		ID:          "ignored",
		Sandbox:     "runsc",
		Command:     []string{"/bin/app"},
		Cwd:         "/workspace",
		RuntimeEnvs: map[string]string{"B": "2", "A": "1"},
		Rootfs: &runtimev1.RootfsConfig{
			Type:     runtimev1.RootfsSrcType_IMAGE,
			Source:   &runtimev1.RootfsConfig_ImageUrl{ImageUrl: "registry/app@sha256:abc"},
			Readonly: true,
		},
	}
	baseID := stableRuntimeTemplateID(base)
	if baseID == "" {
		t.Fatal("stable runtime template id must not be empty")
	}

	reordered := proto.Clone(base).(*runtimev1.RuntimeTemplate)
	reordered.ID = "another-id"
	reordered.RuntimeEnvs = map[string]string{"A": "1", "B": "2"}
	if got := stableRuntimeTemplateID(reordered); got != baseID {
		t.Fatalf("map order and existing id must not affect fingerprint: got %q, want %q", got, baseID)
	}

	tests := map[string]func(*runtimev1.RuntimeTemplate){
		"runtime": func(template *runtimev1.RuntimeTemplate) { template.Sandbox = "runc" },
		"command": func(template *runtimev1.RuntimeTemplate) { template.Command = []string{"/bin/other"} },
		"cwd":     func(template *runtimev1.RuntimeTemplate) { template.Cwd = "/app" },
		"environment": func(template *runtimev1.RuntimeTemplate) {
			template.RuntimeEnvs["A"] = "changed"
		},
		"rootfs": func(template *runtimev1.RuntimeTemplate) {
			template.Rootfs.Source = &runtimev1.RootfsConfig_ImageUrl{ImageUrl: "registry/app@sha256:def"}
		},
		"rootfs readonly": func(template *runtimev1.RuntimeTemplate) { template.Rootfs.Readonly = false },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := proto.Clone(base).(*runtimev1.RuntimeTemplate)
			mutate(candidate)
			if got := stableRuntimeTemplateID(candidate); got == baseID {
				t.Fatalf("static template change must alter fingerprint: %q", got)
			}
		})
	}
}

func TestStableRuntimeTemplateIDExcludesS3Credentials(t *testing.T) {
	base := &runtimev1.RuntimeTemplate{
		Sandbox: "runsc",
		Rootfs: &runtimev1.RootfsConfig{
			Type: runtimev1.RootfsSrcType_S3,
			Source: &runtimev1.RootfsConfig_S3Config{S3Config: &runtimev1.S3Config{
				Endpoint:        "s3.example",
				Bucket:          "rootfs",
				Object:          "image.tar",
				AccessKeyID:     "first-key",
				AccessKeySecret: "first-secret",
			}},
		},
	}
	baseID := stableRuntimeTemplateID(base)
	rotated := proto.Clone(base).(*runtimev1.RuntimeTemplate)
	rotated.GetRootfs().GetS3Config().AccessKeyID = "rotated-key"
	rotated.GetRootfs().GetS3Config().AccessKeySecret = "rotated-secret"
	if got := stableRuntimeTemplateID(rotated); got != baseID {
		t.Fatalf("credential rotation must not partition the runtime template cache: got %q, want %q", got, baseID)
	}
	rotated.GetRootfs().GetS3Config().Object = "other.tar"
	if got := stableRuntimeTemplateID(rotated); got == baseID {
		t.Fatalf("S3 object identity must partition the runtime template cache: %q", got)
	}
}

func TestNodeLifecycleGetAllocationStatusMapsState(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeLifecycleService{}
	server := NewNodeLifecycleServer(fakeService, "node-a", NewAllocationTargetRegistry())

	resp, err := server.GetAllocationStatus(context.Background(), &nodelifecyclev1.GetAllocationStatusRequest{AllocationID: "alloc-123", Attempt: 1, NodeID: "node-a"})
	if err != nil {
		t.Fatalf("GetAllocationStatus() error = %v", err)
	}
	if resp.GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("status = %v, want EXITED", resp.GetStatus())
	}
	if !resp.GetExitCodeKnown() || resp.GetExitCode() != 23 {
		t.Fatalf("response = %#v", resp)
	}
}
