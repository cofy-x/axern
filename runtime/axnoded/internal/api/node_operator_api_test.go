package api

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type fakeNodeOperatorService struct {
	execRequests         []*runtimev1.ExecRequest
	statFileRequests     []*runtimev1.StatFileRequest
	listDirRequests      []*runtimev1.ListDirRequest
	waitRequests         []*runtimev1.WaitRequest
	listRequests         []*runtimev1.ListContainersRequest
	deleteRequests       []*runtimev1.DeleteRequest
	killRequests         []*runtimev1.KillRequest
	reportedAllocationID string
	reportedAttempt      int64
	reportedExitCode     int32
	reportedKnown        bool
	diagnosticsID        string
	diagnosticsFull      bool
	inventory            nodeinventory.NodeInventorySnapshot
	inventoryReady       bool
	networkPolicy        service.NetworkPolicyDiagnostics
}

func (f *fakeNodeOperatorService) ManagedAllocationAttempt(string) (int64, bool) { return 1, true }
func (f *fakeNodeOperatorService) NetworkPolicyDiagnostics(context.Context, string) service.NetworkPolicyDiagnostics {
	return f.networkPolicy
}

func (f *fakeNodeOperatorService) Run(context.Context) error      { return nil }
func (f *fakeNodeOperatorService) Shutdown(context.Context) error { return nil }
func (f *fakeNodeOperatorService) DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error {
	return nil
}
func (f *fakeNodeOperatorService) ReconcileAllocationCapabilities(context.Context, string) ([]*capabilityv1.CapabilityDependency, *capabilityv1.CapabilityConditionSet, error) {
	return nil, nil, nil
}
func (f *fakeNodeOperatorService) Start(context.Context, *runtimev1.StartRequest) (*runtimev1.StartResponse, error) {
	return nil, nil
}
func (f *fakeNodeOperatorService) Delete(ctx context.Context, req *runtimev1.DeleteRequest) (*runtimev1.DeleteResponse, error) {
	_ = ctx
	f.deleteRequests = append(f.deleteRequests, req)
	return &runtimev1.DeleteResponse{}, nil
}
func (f *fakeNodeOperatorService) ExecStream(service.ExecStreamServer) error { return nil }
func (f *fakeNodeOperatorService) Process(service.ProcessStreamServer) error { return nil }
func (f *fakeNodeOperatorService) ProcessImage(service.ProcessImageStreamServer) error {
	return nil
}
func (f *fakeNodeOperatorService) ProxyHTTP(service.HTTPProxyServer) error  { return nil }
func (f *fakeNodeOperatorService) Ready() bool                              { return true }
func (f *fakeNodeOperatorService) RuntimeStatuses() []service.RuntimeStatus { return nil }
func (f *fakeNodeOperatorService) NodeInventory() (nodeinventory.NodeInventorySnapshot, bool) {
	return f.inventory, f.inventoryReady
}
func (f *fakeNodeOperatorService) NetworkForSandbox(containerID string) (*service.SandboxNetwork, error) {
	return &service.SandboxNetwork{IP: "172.17.0.2", NetNSPath: "/var/run/netns/axctl-test"}, nil
}
func (f *fakeNodeOperatorService) SandboxdDiagnostics(ctx context.Context, containerID string, full bool) (service.SandboxdDiagnostics, error) {
	_ = ctx
	f.diagnosticsID = containerID
	f.diagnosticsFull = full
	return service.SandboxdDiagnostics{
		GeneratedAt: time.Unix(1710000002, 0).UTC(),
		Ready:       true,
		Detail:      "full",
		Status: service.SandboxdDiagnosticsStatus{
			DaemonPID:     42,
			UptimeSeconds: 3.5,
			SocketPath:    "/tmp/sandboxd.sock",
			UserState:     "running",
		},
		Capabilities: []string{"health", "status", "process"},
		Providers: []service.SandboxdProvider{
			{
				Name:         "process",
				State:        "available",
				Available:    true,
				Capabilities: []string{"process"},
				Dependencies: []service.SandboxdProviderDependency{{Name: "procfs", Available: true}},
			},
		},
		ProviderSummary: service.SandboxdProviderSummary{Total: 1, Available: 1},
		ProcessSummary:  service.SandboxdProcessSummary{Total: 2, Running: 1, Exited: 1},
		RawJSON:         `{"ready":true}`,
	}, nil
}

func (f *fakeNodeOperatorService) SandboxCapabilityStatus(ctx context.Context, containerID string) (service.SandboxCapabilityStatus, error) {
	diagnostics, err := f.SandboxdDiagnostics(ctx, containerID, false)
	if err != nil {
		return service.SandboxCapabilityStatus{}, err
	}
	return service.SandboxCapabilityStatus{
		Ready:           diagnostics.Ready,
		Capabilities:    append([]string(nil), diagnostics.Capabilities...),
		ProviderSummary: service.SandboxCapabilityProviderSummary{Total: diagnostics.ProviderSummary.Total, Available: diagnostics.ProviderSummary.Available},
	}, nil
}
func (f *fakeNodeOperatorService) Stats(context.Context, *runtimev1.StatsRequest) (*runtimev1.StatsResponse, error) {
	return nil, nil
}
func (f *fakeNodeOperatorService) Kill(ctx context.Context, req *runtimev1.KillRequest) (*runtimev1.KillResponse, error) {
	_ = ctx
	f.killRequests = append(f.killRequests, req)
	return &runtimev1.KillResponse{}, nil
}
func (f *fakeNodeOperatorService) Checkpoint(context.Context, *runtimev1.CheckpointRequest) (*runtimev1.CheckpointResponse, error) {
	return nil, nil
}
func (f *fakeNodeOperatorService) Version(context.Context, *runtimev1.VersionRequest) (*runtimev1.VersionResponse, error) {
	return nil, nil
}
func (f *fakeNodeOperatorService) ReportAllocationStatus(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time) {
	_ = status
	_ = ready
	_ = readinessMessage
	_ = message
	_ = observedAt
	f.reportedAllocationID = allocationID
	f.reportedAttempt = attempt
	f.reportedExitCode = exitCode
	f.reportedKnown = exitCodeKnown
}

func (f *fakeNodeOperatorService) Exec(ctx context.Context, req *runtimev1.ExecRequest) (*runtimev1.ExecResponse, error) {
	_ = ctx
	f.execRequests = append(f.execRequests, req)
	return &runtimev1.ExecResponse{ExitCode: 0, Stdout: []byte("ok\n")}, nil
}

func (f *fakeNodeOperatorService) ExecImage(context.Context, *runtimev1.ExecImageRequest) (*runtimev1.ExecImageResponse, error) {
	return &runtimev1.ExecImageResponse{}, nil
}

func (f *fakeNodeOperatorService) StatFile(ctx context.Context, req *runtimev1.StatFileRequest) (*runtimev1.StatFileResponse, error) {
	_ = ctx
	f.statFileRequests = append(f.statFileRequests, req)
	return &runtimev1.StatFileResponse{Info: &filev1.SandboxFileInfo{
		Path:    req.GetPath(),
		Kind:    filev1.SandboxFileKind_SANDBOX_FILE_KIND_FILE,
		Size:    5,
		Mode:    0644,
		MtimeNs: 7,
	}}, nil
}

func (f *fakeNodeOperatorService) ListDir(ctx context.Context, req *runtimev1.ListDirRequest) (*runtimev1.ListDirResponse, error) {
	_ = ctx
	f.listDirRequests = append(f.listDirRequests, req)
	return &runtimev1.ListDirResponse{}, nil
}

func (f *fakeNodeOperatorService) ReadFile(context.Context, *runtimev1.ReadFileRequest) (*runtimev1.ReadFileResponse, error) {
	return &runtimev1.ReadFileResponse{}, nil
}

func (f *fakeNodeOperatorService) WriteFile(context.Context, *runtimev1.WriteFileRequest) (*runtimev1.WriteFileResponse, error) {
	return &runtimev1.WriteFileResponse{}, nil
}

func (f *fakeNodeOperatorService) Mkdir(context.Context, *runtimev1.MkdirRequest) (*runtimev1.MkdirResponse, error) {
	return &runtimev1.MkdirResponse{}, nil
}

func (f *fakeNodeOperatorService) Remove(context.Context, *runtimev1.RemoveRequest) (*runtimev1.RemoveResponse, error) {
	return &runtimev1.RemoveResponse{}, nil
}

func (f *fakeNodeOperatorService) Exists(context.Context, *runtimev1.ExistsRequest) (*runtimev1.ExistsResponse, error) {
	return &runtimev1.ExistsResponse{}, nil
}

func (f *fakeNodeOperatorService) Copy(context.Context, *runtimev1.CopyRequest) (*runtimev1.CopyResponse, error) {
	return &runtimev1.CopyResponse{}, nil
}

func (f *fakeNodeOperatorService) Move(context.Context, *runtimev1.MoveRequest) (*runtimev1.MoveResponse, error) {
	return &runtimev1.MoveResponse{}, nil
}

func (f *fakeNodeOperatorService) Chmod(context.Context, *runtimev1.ChmodRequest) (*runtimev1.ChmodResponse, error) {
	return &runtimev1.ChmodResponse{}, nil
}

func (f *fakeNodeOperatorService) Touch(context.Context, *runtimev1.TouchRequest) (*runtimev1.TouchResponse, error) {
	return &runtimev1.TouchResponse{}, nil
}

func (f *fakeNodeOperatorService) ComputerUseStatus(context.Context, *runtimev1.ComputerUseStatusRequest) (*runtimev1.ComputerUseStatusResponse, error) {
	return &runtimev1.ComputerUseStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) ComputerUseScreenshot(context.Context, *runtimev1.ComputerUseScreenshotRequest) (*runtimev1.ComputerUseScreenshotResponse, error) {
	return &runtimev1.ComputerUseScreenshotResponse{}, nil
}

func (f *fakeNodeOperatorService) ComputerUseDisplay(context.Context, *runtimev1.ComputerUseDisplayRequest) (*runtimev1.ComputerUseDisplayResponse, error) {
	return &runtimev1.ComputerUseDisplayResponse{}, nil
}

func (f *fakeNodeOperatorService) ComputerUseMouse(context.Context, *runtimev1.ComputerUseMouseRequest) (*runtimev1.ComputerUseMouseResponse, error) {
	return &runtimev1.ComputerUseMouseResponse{}, nil
}

func (f *fakeNodeOperatorService) ComputerUseKeyboard(context.Context, *runtimev1.ComputerUseKeyboardRequest) (*runtimev1.ComputerUseKeyboardResponse, error) {
	return &runtimev1.ComputerUseKeyboardResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserStatus(context.Context, *runtimev1.BrowserStatusRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserOpen(context.Context, *runtimev1.BrowserOpenRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserClose(context.Context, *runtimev1.BrowserCloseRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserNavigate(context.Context, *runtimev1.BrowserNavigateRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserResize(context.Context, *runtimev1.BrowserResizeRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserClick(context.Context, *runtimev1.BrowserClickRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserType(context.Context, *runtimev1.BrowserTypeRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) BrowserWait(context.Context, *runtimev1.BrowserWaitRequest) (*runtimev1.BrowserStatusResponse, error) {
	return &runtimev1.BrowserStatusResponse{}, nil
}

func (f *fakeNodeOperatorService) UploadArchive(context.Context, *runtimev1.UploadArchiveRequest, io.Reader) (*runtimev1.UploadArchiveResponse, error) {
	return &runtimev1.UploadArchiveResponse{}, nil
}

func (f *fakeNodeOperatorService) DownloadArchive(context.Context, *runtimev1.DownloadArchiveRequest, io.Writer) (*runtimev1.DownloadArchiveResponse, error) {
	return &runtimev1.DownloadArchiveResponse{}, nil
}

func (f *fakeNodeOperatorService) Wait(ctx context.Context, req *runtimev1.WaitRequest) (*runtimev1.WaitResponse, error) {
	_ = ctx
	f.waitRequests = append(f.waitRequests, req)
	return &runtimev1.WaitResponse{Status: 0, ExitCode: 23, Message: "done"}, nil
}

func (f *fakeNodeOperatorService) List(ctx context.Context, req *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error) {
	_ = ctx
	f.listRequests = append(f.listRequests, req)
	return &runtimev1.ListContainersResponse{
		Containers: []*runtimev1.ContainerStatus{
			{
				ID:         req.GetID(),
				Runtime:    "runsc",
				State:      runtimev1.ContainerState_CONTAINER_EXITED,
				ExitCode:   23,
				Message:    "done",
				Pid:        321,
				StartedAt:  1710000000,
				FinishedAt: 1710000001,
			},
		},
	}, nil
}

func TestNodeOperatorListSandboxesBridgesList(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{}, NewAllocationTargetRegistry())
	resp, err := server.ListSandboxes(context.Background(), &nodeoperatorv1.ListSandboxesRequest{})
	if err != nil {
		t.Fatalf("ListSandboxes() error = %v", err)
	}
	if len(resp.GetSandboxes()) != 1 {
		t.Fatalf("sandbox count = %d, want 1", len(resp.GetSandboxes()))
	}
	if resp.GetSandboxes()[0].GetPid() != 321 {
		t.Fatalf("pid = %d, want 321", resp.GetSandboxes()[0].GetPid())
	}
}

func TestNodeOperatorExecBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService, NewAllocationTargetRegistry())

	resp, err := server.Exec(context.Background(), &nodeoperatorv1.ExecRequest{
		SandboxID: "sandbox-123",
		Spec: &nodesandboxv1.ExecSpec{
			Argv:           []string{"python", "-c", "print('ok')"},
			Env:            map[string]string{"A": "B"},
			Cwd:            "/workspace",
			User:           "axern",
			TimeoutSeconds: 9,
		},
	})
	if err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
	if resp.GetExitCode() != 0 || string(resp.GetStdout()) != "ok\n" {
		t.Fatalf("unexpected exec response = %#v", resp)
	}
	if len(fakeService.execRequests) != 1 {
		t.Fatalf("exec request count = %d, want 1", len(fakeService.execRequests))
	}
	got := fakeService.execRequests[0]
	if got.GetID() != "sandbox-123" || got.GetTimeout() != 9 || got.GetCwd() != "/workspace" {
		t.Fatalf("exec request = %#v", got)
	}
	if got.GetUser() != "axern" {
		t.Fatalf("exec request user = %q, want axern", got.GetUser())
	}
}

func TestNodeOperatorKillBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService, NewAllocationTargetRegistry())

	_, err := server.KillSandbox(context.Background(), &nodeoperatorv1.KillSandboxRequest{
		SandboxID: "sandbox-123",
		Signal:    "SIGKILL",
	})
	if err != nil {
		t.Fatalf("KillSandbox() error = %v", err)
	}
	if len(fakeService.killRequests) != 1 {
		t.Fatalf("kill request count = %d, want 1", len(fakeService.killRequests))
	}
	got := fakeService.killRequests[0]
	if got.GetID() != "sandbox-123" || got.GetSignal() != "SIGKILL" {
		t.Fatalf("kill request = %#v", got)
	}
}

func TestNodeOperatorWaitReturnsExit(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService, NewAllocationTargetRegistry())

	resp, err := server.WaitSandbox(context.Background(), &nodeoperatorv1.WaitSandboxRequest{SandboxID: "sandbox-123"})
	if err != nil {
		t.Fatalf("WaitSandbox() error = %v", err)
	}
	if resp.GetState() != nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED {
		t.Fatalf("state = %v, want EXITED", resp.GetState())
	}
	if !resp.GetExitCodeKnown() || resp.GetExitCode() != 23 {
		t.Fatalf("wait response = %#v, want exit=23 known=true", resp)
	}
}

func TestNodeOperatorSandboxDiagnosticsBridgesSandboxdSnapshot(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService, NewAllocationTargetRegistry())
	resp, err := server.GetSandboxDiagnostics(context.Background(), &nodeoperatorv1.GetSandboxDiagnosticsRequest{SandboxID: "sandbox-123", Full: true})
	if err != nil {
		t.Fatalf("GetSandboxDiagnostics() error = %v", err)
	}
	if fakeService.diagnosticsID != "sandbox-123" || !fakeService.diagnosticsFull {
		t.Fatalf("diagnostics bridge id=%q full=%v", fakeService.diagnosticsID, fakeService.diagnosticsFull)
	}
	if !resp.GetReady() || resp.GetDaemonPid() != 42 || resp.GetSocketPath() != "/tmp/sandboxd.sock" || resp.GetUserState() != "running" {
		t.Fatalf("diagnostics response = %#v", resp)
	}
	if resp.GetProviderSummary().GetTotal() != 1 || resp.GetProcessSummary().GetRunning() != 1 {
		t.Fatalf("diagnostics summaries = providers:%#v processes:%#v", resp.GetProviderSummary(), resp.GetProcessSummary())
	}
	if len(resp.GetProviders()) != 1 || resp.GetProviders()[0].GetName() != "process" || len(resp.GetProviders()[0].GetDependencies()) != 1 {
		t.Fatalf("diagnostics providers = %#v", resp.GetProviders())
	}
	if resp.GetRawJson() == "" || resp.GetGeneratedAt() == nil {
		t.Fatalf("diagnostics raw/generated missing: %#v", resp)
	}
}

func TestNodeOperatorNetworkPolicyDiagnosticsAreBoundedAndPrivacySafe(t *testing.T) {
	t.Parallel()
	fakeService := &fakeNodeOperatorService{networkPolicy: service.NetworkPolicyDiagnostics{
		Mode: service.NetworkPolicyModeStrict, Status: service.NetworkPolicyStatusOK,
		CapabilityState: service.NetworkPolicyCapabilityAvailable, EnforcementHealthy: true, ExactProof: true,
		AllocationAttempt: 2, ExecutionRevision: 7, EnforcementRevision: 11,
		DomainRuleCount: 3, CIDRRuleCount: 2, PortRangeCount: 4, TotalRuleCount: 5, RecoveredAfterRestart: true,
	}}
	server := NewNodeOperatorServer(fakeService, NewAllocationTargetRegistry())
	response, err := server.ExplainSandboxNetworkPolicy(context.Background(), &nodeoperatorv1.ExplainSandboxNetworkPolicyRequest{SandboxID: "sandbox-123"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetMode() != nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_STRICT ||
		response.GetStatus() != nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_OK ||
		!response.GetExactProof() || response.GetTotalRuleCount() != 5 || !response.GetRecoveredAfterRestart() {
		t.Fatalf("network policy diagnostics = %#v", response)
	}
	for index := range response.ProtoReflect().Descriptor().Fields().Len() {
		name := string(response.ProtoReflect().Descriptor().Fields().Get(index).Name())
		for _, forbidden := range []string{"domain_name", "host", "sni", "remote_ip", "cidr_value", "policy_digest", "raw"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("privacy-sensitive field %q entered operator diagnostics", name)
			}
		}
	}
}

func TestNodeOperatorSandboxMemoryReturnsLatestResolvedObservation(t *testing.T) {
	t.Parallel()

	inventory := nodeinventory.NewSnapshot()
	inventory.AllocationMemoryObservations = []*controlnodev1.AllocationMemoryObservation{{
		AllocationID: "container-123", Attempt: 2, Revision: 7, LimitBytes: 512 << 20,
	}}
	fakeService := &fakeNodeOperatorService{inventory: inventory, inventoryReady: true}
	targets := NewAllocationTargetRegistry()
	targets.bind("allocation-123", "container-123")
	server := NewNodeOperatorServer(fakeService, targets)

	resp, err := server.GetSandboxMemory(context.Background(), &nodeoperatorv1.GetSandboxMemoryRequest{SandboxID: "allocation-123"})
	if err != nil {
		t.Fatalf("GetSandboxMemory() error = %v", err)
	}
	if got := resp.GetObservation(); got.GetAllocationID() != "container-123" || got.GetRevision() != 7 || got.GetLimitBytes() != 512<<20 {
		t.Fatalf("GetSandboxMemory() = %#v", got)
	}
}

func TestNodeOperatorSandboxMemoryFailsClosedWithoutFreshObservation(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{}, NewAllocationTargetRegistry())
	_, err := server.GetSandboxMemory(context.Background(), &nodeoperatorv1.GetSandboxMemoryRequest{SandboxID: "sandbox-123"})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("GetSandboxMemory() code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}
}

func TestNodeOperatorSandboxDiagnosticsRequiresID(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{}, NewAllocationTargetRegistry())
	_, err := server.GetSandboxDiagnostics(context.Background(), &nodeoperatorv1.GetSandboxDiagnosticsRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetSandboxDiagnostics() code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestNodeOperatorGetSandboxRequiresID(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{}, NewAllocationTargetRegistry())
	_, err := server.GetSandbox(context.Background(), &nodeoperatorv1.GetSandboxRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetSandbox() code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}
