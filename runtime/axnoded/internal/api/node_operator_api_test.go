package api

import (
	"context"
	"strings"
	"testing"
	"time"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type fakeNodeOperatorService struct {
	execRequests        []*runtimev1.ExecRequest
	waitRequests        []*runtimev1.WaitRequest
	listRequests        []*runtimev1.ListContainersRequest
	diagnosticsID       string
	diagnosticsFull     bool
	inventory           nodeinventory.NodeInventorySnapshot
	inventoryReady      bool
	networkPolicy       service.NetworkPolicyDiagnostics
	validateOperatorErr error
	validateInspectErr  error
	forceAction         string
	forceAllocationID   string
	forceReason         string
}

func (f *fakeNodeOperatorService) NetworkPolicyDiagnostics(context.Context, string) service.NetworkPolicyDiagnostics {
	return f.networkPolicy
}

func (f *fakeNodeOperatorService) ExecStream(service.ExecStreamServer) error { return nil }
func (f *fakeNodeOperatorService) NodeInventory() (nodeinventory.NodeInventorySnapshot, bool) {
	return f.inventory, f.inventoryReady
}
func (f *fakeNodeOperatorService) ValidateOperatorExecution(string) error {
	return f.validateOperatorErr
}
func (f *fakeNodeOperatorService) ValidateOperatorInspection(string) error {
	return f.validateInspectErr
}
func (f *fakeNodeOperatorService) ForceTerminateAllocation(_ context.Context, allocationID, reason string) error {
	f.forceAction = "terminate"
	f.forceAllocationID = allocationID
	f.forceReason = reason
	return nil
}
func (f *fakeNodeOperatorService) ForceCleanupAllocation(_ context.Context, allocationID, reason string, _ int64) error {
	f.forceAction = "cleanup"
	f.forceAllocationID = allocationID
	f.forceReason = reason
	return nil
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

func (f *fakeNodeOperatorService) Exec(ctx context.Context, req *runtimev1.ExecRequest) (*runtimev1.ExecResponse, error) {
	_ = ctx
	f.execRequests = append(f.execRequests, req)
	return &runtimev1.ExecResponse{ExitCode: 0, Stdout: []byte("ok\n")}, nil
}

func (f *fakeNodeOperatorService) Wait(ctx context.Context, req *runtimev1.WaitRequest) (*runtimev1.WaitResponse, error) {
	_ = ctx
	f.waitRequests = append(f.waitRequests, req)
	return &runtimev1.WaitResponse{ExitCode: func() *int32 { value := int32(23); return &value }(), Message: "done"}, nil
}

func (f *fakeNodeOperatorService) List(ctx context.Context, req *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error) {
	_ = ctx
	f.listRequests = append(f.listRequests, req)
	return &runtimev1.ListContainersResponse{
		Containers: []*runtimev1.ContainerStatus{
			{
				ID:         req.GetID(),
				State:      runtimev1.ContainerState_CONTAINER_EXITED,
				ExitCode:   func() *int32 { value := int32(23); return &value }(),
				Message:    "done",
				Pid:        321,
				StartedAt:  1710000000,
				FinishedAt: 1710000001,
			},
		},
	}, nil
}

func TestNodeOperatorListAllocationsBridgesList(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{})
	resp, err := server.ListAllocations(context.Background(), &nodeoperatorv1.ListAllocationsRequest{})
	if err != nil {
		t.Fatalf("ListAllocations() error = %v", err)
	}
	if len(resp.GetAllocations()) != 1 {
		t.Fatalf("allocation count = %d, want 1", len(resp.GetAllocations()))
	}
	if resp.GetAllocations()[0].GetPid() != 321 {
		t.Fatalf("pid = %d, want 321", resp.GetAllocations()[0].GetPid())
	}
}

func TestNodeOperatorListAllocationsExcludesUnownedRuntime(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{
		validateInspectErr: grpcstatus.Error(codes.FailedPrecondition, "no AllocationState"),
	})
	resp, err := server.ListAllocations(context.Background(), &nodeoperatorv1.ListAllocationsRequest{})
	if err != nil {
		t.Fatalf("ListAllocations() error = %v", err)
	}
	if len(resp.GetAllocations()) != 0 {
		t.Fatalf("allocation count = %d, want orphan runtime excluded", len(resp.GetAllocations()))
	}
}

func TestNodeOperatorExecBridgesRequest(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService)

	resp, err := server.Exec(context.Background(), &nodeoperatorv1.ExecRequest{
		AllocationID: "sandbox-123",
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

func TestNodeOperatorForceTerminateCarriesReason(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService)

	_, err := server.ForceTerminateAllocation(context.Background(), &nodeoperatorv1.ForceTerminateAllocationRequest{
		AllocationID: "sandbox-123",
		Reason:       "incident recovery",
	})
	if err != nil {
		t.Fatalf("ForceTerminateAllocation() error = %v", err)
	}
	if fakeService.forceAction != "terminate" || fakeService.forceAllocationID != "sandbox-123" || fakeService.forceReason != "incident recovery" {
		t.Fatalf("force terminate request = action:%q allocation:%q reason:%q", fakeService.forceAction, fakeService.forceAllocationID, fakeService.forceReason)
	}
}

func TestNodeOperatorExecFailsClosedBeforeRuntimeCall(t *testing.T) {
	t.Parallel()
	fakeService := &fakeNodeOperatorService{validateOperatorErr: grpcstatus.Error(codes.FailedPrecondition, "runtime identity mismatch")}
	server := NewNodeOperatorServer(fakeService)
	_, err := server.Exec(context.Background(), &nodeoperatorv1.ExecRequest{
		AllocationID: "allocation-123",
		Spec:         &nodesandboxv1.ExecSpec{Argv: []string{"true"}},
	})
	if grpcstatus.Code(err) != codes.FailedPrecondition || len(fakeService.execRequests) != 0 {
		t.Fatalf("Exec() error=%v requests=%d", err, len(fakeService.execRequests))
	}
}

func TestNodeOperatorWaitReturnsExit(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService)

	resp, err := server.Wait(context.Background(), &nodeoperatorv1.WaitRequest{AllocationID: "sandbox-123"})
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if resp.GetState() != nodeoperatorv1.LocalAllocationState_LOCAL_ALLOCATION_STATE_EXITED {
		t.Fatalf("state = %v, want EXITED", resp.GetState())
	}
	if resp.ExitCode == nil || resp.GetExitCode() != 23 {
		t.Fatalf("wait response = %#v, want exit=23 known=true", resp)
	}
}

func TestNodeOperatorSandboxDiagnosticsBridgesSandboxdSnapshot(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{}
	server := NewNodeOperatorServer(fakeService)
	resp, err := server.GetAllocationDiagnostics(context.Background(), &nodeoperatorv1.GetAllocationDiagnosticsRequest{AllocationID: "sandbox-123", Full: true})
	if err != nil {
		t.Fatalf("GetAllocationDiagnostics() error = %v", err)
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
		CapabilityState: service.NetworkPolicyCapabilityAvailable, EnforcementHealthy: true, ExactBinding: true,
		EnforcementRevision: 11,
		DomainRuleCount:     3, CIDRRuleCount: 2, PortRangeCount: 4, TotalRuleCount: 5,
	}}
	server := NewNodeOperatorServer(fakeService)
	response, err := server.ExplainAllocationNetworkPolicy(context.Background(), &nodeoperatorv1.ExplainAllocationNetworkPolicyRequest{AllocationID: "sandbox-123"})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetMode() != nodeoperatorv1.AllocationNetworkPolicyMode_ALLOCATION_NETWORK_POLICY_MODE_STRICT ||
		response.GetStatus() != nodeoperatorv1.AllocationNetworkPolicyStatus_ALLOCATION_NETWORK_POLICY_STATUS_OK ||
		!response.GetExactBinding() || response.GetTotalRuleCount() != 5 {
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
		AllocationID: "allocation-123", LimitBytes: 512 << 20,
	}}
	fakeService := &fakeNodeOperatorService{inventory: inventory, inventoryReady: true}
	server := NewNodeOperatorServer(fakeService)

	resp, err := server.GetAllocationMemory(context.Background(), &nodeoperatorv1.GetAllocationMemoryRequest{AllocationID: "allocation-123"})
	if err != nil {
		t.Fatalf("GetAllocationMemory() error = %v", err)
	}
	if got := resp.GetObservation(); got.GetAllocationID() != "allocation-123" || got.GetLimitBytes() != 512<<20 {
		t.Fatalf("GetAllocationMemory() = %#v", got)
	}
}

func TestNodeOperatorSandboxMemoryFailsClosedWithoutFreshObservation(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{})
	_, err := server.GetAllocationMemory(context.Background(), &nodeoperatorv1.GetAllocationMemoryRequest{AllocationID: "sandbox-123"})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("GetAllocationMemory() code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}
}

func TestNodeOperatorSandboxDiagnosticsRequiresID(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{})
	_, err := server.GetAllocationDiagnostics(context.Background(), &nodeoperatorv1.GetAllocationDiagnosticsRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetAllocationDiagnostics() code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestNodeOperatorGetAllocationRequiresID(t *testing.T) {
	t.Parallel()

	server := NewNodeOperatorServer(&fakeNodeOperatorService{})
	_, err := server.GetAllocation(context.Background(), &nodeoperatorv1.GetAllocationRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetAllocation() code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestNodeOperatorGetAllocationFailsClosedBeforeRuntimeLookup(t *testing.T) {
	t.Parallel()

	fakeService := &fakeNodeOperatorService{
		validateInspectErr: grpcstatus.Error(codes.FailedPrecondition, "runtime identity mismatch"),
	}
	server := NewNodeOperatorServer(fakeService)
	_, err := server.GetAllocation(context.Background(), &nodeoperatorv1.GetAllocationRequest{AllocationID: "allocation-1"})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("GetAllocation() code = %v, want %v", grpcstatus.Code(err), codes.FailedPrecondition)
	}
	if len(fakeService.listRequests) != 0 {
		t.Fatalf("GetAllocation() reached runtime after failed identity validation: %d requests", len(fakeService.listRequests))
	}
}
