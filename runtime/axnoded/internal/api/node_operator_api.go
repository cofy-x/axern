package api

import (
	"context"
	"time"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type nodeOperatorServer struct {
	nodeoperatorv1.UnimplementedNodeOperatorServer
	svc     service.NodeOperatorService
	targets *AllocationTargetRegistry
}

func NewNodeOperatorServer(svc service.NodeOperatorService, targets *AllocationTargetRegistry) nodeoperatorv1.NodeOperatorServer {
	return &nodeOperatorServer{svc: svc, targets: targets}
}

func (s *nodeOperatorServer) ListSandboxes(ctx context.Context, req *nodeoperatorv1.ListSandboxesRequest) (*nodeoperatorv1.ListSandboxesResponse, error) {
	_ = req
	resp, err := s.svc.List(ctx, &runtimev1.ListContainersRequest{})
	if err != nil {
		return nil, err
	}
	out := &nodeoperatorv1.ListSandboxesResponse{Sandboxes: make([]*nodeoperatorv1.LocalSandbox, 0, len(resp.GetContainers()))}
	for _, container := range resp.GetContainers() {
		out.Sandboxes = append(out.Sandboxes, localSandboxFromContainer(container))
	}
	return out, nil
}

func (s *nodeOperatorServer) GetSandbox(ctx context.Context, req *nodeoperatorv1.GetSandboxRequest) (*nodeoperatorv1.GetSandboxResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	resp, err := s.svc.List(ctx, &runtimev1.ListContainersRequest{ID: s.targets.resolve(req.GetSandboxID())})
	if err != nil {
		return nil, err
	}
	if len(resp.GetContainers()) == 0 {
		return nil, grpcstatus.Errorf(codes.NotFound, "sandbox %q not found", req.GetSandboxID())
	}
	return &nodeoperatorv1.GetSandboxResponse{Sandbox: localSandboxFromContainer(resp.GetContainers()[0])}, nil
}

func (s *nodeOperatorServer) GetSandboxDiagnostics(ctx context.Context, req *nodeoperatorv1.GetSandboxDiagnosticsRequest) (*nodeoperatorv1.GetSandboxDiagnosticsResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	targetID := s.targets.resolve(req.GetSandboxID())
	diagnostics, err := s.svc.SandboxdDiagnostics(ctx, targetID, req.GetFull())
	if err != nil {
		return nil, err
	}
	response := localSandboxDiagnostics(req.GetSandboxID(), diagnostics)
	response.Memory = s.latestAllocationMemoryObservation(targetID)
	return response, nil
}

func (s *nodeOperatorServer) GetSandboxMemory(_ context.Context, req *nodeoperatorv1.GetSandboxMemoryRequest) (*nodeoperatorv1.GetSandboxMemoryResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	observation := s.latestAllocationMemoryObservation(s.targets.resolve(req.GetSandboxID()))
	if observation == nil {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "bounded memory observation for sandbox %q is unavailable", req.GetSandboxID())
	}
	return &nodeoperatorv1.GetSandboxMemoryResponse{Observation: observation}, nil
}

func (s *nodeOperatorServer) ExplainSandboxNetworkPolicy(ctx context.Context, req *nodeoperatorv1.ExplainSandboxNetworkPolicyRequest) (*nodeoperatorv1.ExplainSandboxNetworkPolicyResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	targetID := s.targets.resolve(req.GetSandboxID())
	listed, err := s.svc.List(ctx, &runtimev1.ListContainersRequest{ID: targetID})
	if err != nil {
		return nil, err
	}
	if len(listed.GetContainers()) == 0 {
		return nil, grpcstatus.Errorf(codes.NotFound, "sandbox %q not found", req.GetSandboxID())
	}
	return localNetworkPolicyDiagnostics(req.GetSandboxID(), s.svc.NetworkPolicyDiagnostics(ctx, targetID)), nil
}

func localNetworkPolicyDiagnostics(sandboxID string, diagnostics service.NetworkPolicyDiagnostics) *nodeoperatorv1.ExplainSandboxNetworkPolicyResponse {
	return &nodeoperatorv1.ExplainSandboxNetworkPolicyResponse{
		SandboxID:             sandboxID,
		Mode:                  localNetworkPolicyMode(diagnostics.Mode),
		Status:                localNetworkPolicyStatus(diagnostics.Status),
		CapabilityState:       localNetworkPolicyCapabilityState(diagnostics.CapabilityState),
		EnforcementHealthy:    diagnostics.EnforcementHealthy,
		ExactProof:            diagnostics.ExactProof,
		AllocationAttempt:     diagnostics.AllocationAttempt,
		ExecutionRevision:     diagnostics.ExecutionRevision,
		EnforcementRevision:   diagnostics.EnforcementRevision,
		DomainRuleCount:       diagnostics.DomainRuleCount,
		CidrRuleCount:         diagnostics.CIDRRuleCount,
		PortRangeCount:        diagnostics.PortRangeCount,
		TotalRuleCount:        diagnostics.TotalRuleCount,
		RecoveredAfterRestart: diagnostics.RecoveredAfterRestart,
	}
}

func localNetworkPolicyMode(mode service.NetworkPolicyMode) nodeoperatorv1.SandboxNetworkPolicyMode {
	switch mode {
	case service.NetworkPolicyModeUnrestricted:
		return nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_UNRESTRICTED
	case service.NetworkPolicyModeDNSDeny:
		return nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_DNS_DENY
	case service.NetworkPolicyModeStrict:
		return nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_STRICT
	default:
		return nodeoperatorv1.SandboxNetworkPolicyMode_SANDBOX_NETWORK_POLICY_MODE_UNSPECIFIED
	}
}

func localNetworkPolicyStatus(status service.NetworkPolicyStatus) nodeoperatorv1.SandboxNetworkPolicyStatus {
	switch status {
	case service.NetworkPolicyStatusOK:
		return nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_OK
	case service.NetworkPolicyStatusAbsent:
		return nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_ABSENT
	case service.NetworkPolicyStatusCapabilityUnavailable:
		return nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_CAPABILITY_UNAVAILABLE
	case service.NetworkPolicyStatusEnforcementUnhealthy:
		return nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_ENFORCEMENT_UNHEALTHY
	case service.NetworkPolicyStatusProofStale:
		return nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_PROOF_STALE
	default:
		return nodeoperatorv1.SandboxNetworkPolicyStatus_SANDBOX_NETWORK_POLICY_STATUS_UNSPECIFIED
	}
}

func localNetworkPolicyCapabilityState(state service.NetworkPolicyCapabilityState) nodeoperatorv1.SandboxNetworkPolicyCapabilityState {
	switch state {
	case service.NetworkPolicyCapabilityAvailable:
		return nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_AVAILABLE
	case service.NetworkPolicyCapabilityUnavailable:
		return nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_UNAVAILABLE
	case service.NetworkPolicyCapabilityUnknown:
		return nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_UNKNOWN
	case service.NetworkPolicyCapabilityNotRequired:
		return nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_NOT_REQUIRED
	default:
		return nodeoperatorv1.SandboxNetworkPolicyCapabilityState_SANDBOX_NETWORK_POLICY_CAPABILITY_STATE_UNSPECIFIED
	}
}

func (s *nodeOperatorServer) latestAllocationMemoryObservation(containerID string) *controlnodev1.AllocationMemoryObservation {
	snapshot, ready := s.svc.NodeInventory()
	if !ready {
		return nil
	}
	for _, observation := range snapshot.AllocationMemoryObservations {
		if observation != nil && observation.GetAllocationID() == containerID {
			return proto.Clone(observation).(*controlnodev1.AllocationMemoryObservation)
		}
	}
	return nil
}

func (s *nodeOperatorServer) DeleteSandbox(ctx context.Context, req *nodeoperatorv1.DeleteSandboxRequest) (*nodeoperatorv1.DeleteSandboxResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	if _, err := s.svc.Delete(ctx, &runtimev1.DeleteRequest{ID: s.targets.resolve(req.GetSandboxID()), Timeout: req.GetTimeoutSeconds()}); err != nil {
		return nil, err
	}
	s.targets.unbind(req.GetSandboxID())
	return &nodeoperatorv1.DeleteSandboxResponse{}, nil
}

func (s *nodeOperatorServer) KillSandbox(ctx context.Context, req *nodeoperatorv1.KillSandboxRequest) (*nodeoperatorv1.KillSandboxResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	if _, err := s.svc.Kill(ctx, &runtimev1.KillRequest{ID: s.targets.resolve(req.GetSandboxID()), Signal: req.GetSignal()}); err != nil {
		return nil, err
	}
	return &nodeoperatorv1.KillSandboxResponse{}, nil
}

func (s *nodeOperatorServer) Exec(ctx context.Context, req *nodeoperatorv1.ExecRequest) (*nodeoperatorv1.ExecResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	if req.GetSpec() == nil || len(req.GetSpec().GetArgv()) == 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "spec.argv is required")
	}

	resp, err := s.svc.Exec(ctx, &runtimev1.ExecRequest{
		ID:      s.targets.resolve(req.GetSandboxID()),
		Command: append([]string(nil), req.GetSpec().GetArgv()...),
		Timeout: req.GetSpec().GetTimeoutSeconds(),
		Env:     cloneStringMap(req.GetSpec().GetEnv()),
		Cwd:     req.GetSpec().GetCwd(),
		User:    req.GetSpec().GetUser(),
	})
	if err != nil {
		return nil, err
	}
	return &nodeoperatorv1.ExecResponse{
		ExitCode:        resp.GetExitCode(),
		Stdout:          resp.GetStdout(),
		Stderr:          resp.GetStderr(),
		StdoutTruncated: resp.GetStdoutTruncated(),
		StderrTruncated: resp.GetStderrTruncated(),
	}, nil
}

func (s *nodeOperatorServer) ExecStream(stream nodeoperatorv1.NodeOperator_ExecStreamServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "initial open payload is required")
	}
	if open.GetSandboxID() == "" {
		return grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	if open.GetSpec() == nil || len(open.GetSpec().GetArgv()) == 0 {
		return grpcstatus.Error(codes.InvalidArgument, "spec.argv is required")
	}

	adapter := &nodeOperatorExecStreamAdapter{
		stream:    stream,
		first:     first,
		firstSent: false,
		targetID:  s.targets.resolve(open.GetSandboxID()),
	}
	return s.svc.ExecStream(adapter)
}

func (s *nodeOperatorServer) WaitSandbox(ctx context.Context, req *nodeoperatorv1.WaitSandboxRequest) (*nodeoperatorv1.WaitSandboxResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}

	resp, err := s.svc.Wait(ctx, &runtimev1.WaitRequest{ID: s.targets.resolve(req.GetSandboxID())})
	if err == nil {
		return &nodeoperatorv1.WaitSandboxResponse{
			State:         nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED,
			ExitCode:      resp.GetExitCode(),
			ExitCodeKnown: true,
			Message:       resp.GetMessage(),
		}, nil
	}

	if grpcstatus.Code(err) == codes.Unavailable && resp != nil {
		return &nodeoperatorv1.WaitSandboxResponse{
			State:         nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED,
			ExitCode:      resp.GetExitCode(),
			ExitCodeKnown: false,
			Message:       resp.GetMessage(),
		}, nil
	}
	return nil, err
}

func (s *nodeOperatorServer) ResolveSandboxNetwork(ctx context.Context, req *nodeoperatorv1.ResolveSandboxNetworkRequest) (*nodeoperatorv1.ResolveSandboxNetworkResponse, error) {
	if req.GetSandboxID() == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "sandbox_id is required")
	}
	targetID := s.targets.resolve(req.GetSandboxID())
	network, err := s.svc.NetworkForSandbox(targetID)
	if err != nil {
		return nil, err
	}
	return &nodeoperatorv1.ResolveSandboxNetworkResponse{
		SandboxID:    req.GetSandboxID(),
		Ip:           network.IP,
		NetnsPath:    network.NetNSPath,
		State:        nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_RUNNING,
		RuntimeClass: network.RuntimeClass,
	}, nil
}

type nodeOperatorExecStreamAdapter struct {
	stream    nodeoperatorv1.NodeOperator_ExecStreamServer
	first     *nodeoperatorv1.ExecStreamRequest
	firstSent bool
	targetID  string
}

func (a *nodeOperatorExecStreamAdapter) SetHeader(md metadata.MD) error {
	return a.stream.SetHeader(md)
}
func (a *nodeOperatorExecStreamAdapter) SendHeader(md metadata.MD) error {
	return a.stream.SendHeader(md)
}
func (a *nodeOperatorExecStreamAdapter) SetTrailer(md metadata.MD) { a.stream.SetTrailer(md) }
func (a *nodeOperatorExecStreamAdapter) Context() context.Context  { return a.stream.Context() }
func (a *nodeOperatorExecStreamAdapter) SendMsg(m any) error       { return a.stream.SendMsg(m) }
func (a *nodeOperatorExecStreamAdapter) RecvMsg(m any) error       { return a.stream.RecvMsg(m) }

func (a *nodeOperatorExecStreamAdapter) Recv() (*runtimev1.ExecStreamRequest, error) {
	if !a.firstSent {
		a.firstSent = true
		return convertNodeOperatorExecStreamRequest(a.first, a.targetID)
	}
	req, err := a.stream.Recv()
	if err != nil {
		return nil, err
	}
	return convertNodeOperatorExecStreamRequest(req, a.targetID)
}

func (a *nodeOperatorExecStreamAdapter) Send(resp *runtimev1.ExecStreamResponse) error {
	return a.stream.Send(convertNodeOperatorExecStreamResponse(resp))
}

func convertNodeOperatorExecStreamRequest(in *nodeoperatorv1.ExecStreamRequest, targetID string) (*runtimev1.ExecStreamRequest, error) {
	switch payload := in.GetPayload().(type) {
	case *nodeoperatorv1.ExecStreamRequest_Open:
		return &runtimev1.ExecStreamRequest{
			Payload: &runtimev1.ExecStreamRequest_Open{
				Open: &runtimev1.ExecStreamOpen{
					ID:          targetID,
					Command:     append([]string(nil), payload.Open.GetSpec().GetArgv()...),
					Tty:         payload.Open.GetSpec().GetTty(),
					Timeout:     payload.Open.GetSpec().GetTimeoutSeconds(),
					Env:         cloneStringMap(payload.Open.GetSpec().GetEnv()),
					Cwd:         payload.Open.GetSpec().GetCwd(),
					User:        payload.Open.GetSpec().GetUser(),
					InitialSize: convertNodeOperatorTerminalResize(payload.Open.GetInitialSize()),
				},
			},
		}, nil
	case *nodeoperatorv1.ExecStreamRequest_Stdin:
		return &runtimev1.ExecStreamRequest{Payload: &runtimev1.ExecStreamRequest_Stdin{Stdin: payload.Stdin}}, nil
	case *nodeoperatorv1.ExecStreamRequest_Resize:
		return &runtimev1.ExecStreamRequest{
			Payload: &runtimev1.ExecStreamRequest_Resize{
				Resize: &runtimev1.TerminalResize{Cols: payload.Resize.GetCols(), Rows: payload.Resize.GetRows()},
			},
		}, nil
	case *nodeoperatorv1.ExecStreamRequest_CloseStdin:
		return &runtimev1.ExecStreamRequest{Payload: &runtimev1.ExecStreamRequest_CloseStdin{CloseStdin: payload.CloseStdin}}, nil
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "unsupported exec stream payload")
	}
}

func convertNodeOperatorTerminalResize(in *nodeoperatorv1.TerminalResize) *runtimev1.TerminalResize {
	if in == nil {
		return nil
	}
	return &runtimev1.TerminalResize{Cols: in.GetCols(), Rows: in.GetRows()}
}

func convertNodeOperatorExecStreamResponse(in *runtimev1.ExecStreamResponse) *nodeoperatorv1.ExecStreamResponse {
	switch payload := in.GetPayload().(type) {
	case *runtimev1.ExecStreamResponse_Stdout:
		return &nodeoperatorv1.ExecStreamResponse{Payload: &nodeoperatorv1.ExecStreamResponse_Stdout{Stdout: payload.Stdout}}
	case *runtimev1.ExecStreamResponse_Stderr:
		return &nodeoperatorv1.ExecStreamResponse{Payload: &nodeoperatorv1.ExecStreamResponse_Stderr{Stderr: payload.Stderr}}
	case *runtimev1.ExecStreamResponse_Exit:
		return &nodeoperatorv1.ExecStreamResponse{Payload: &nodeoperatorv1.ExecStreamResponse_Exit{Exit: &nodeoperatorv1.ExecExit{
			ExitCode: payload.Exit.GetExitCode(),
			Message:  payload.Exit.GetMessage(),
		}}}
	default:
		return &nodeoperatorv1.ExecStreamResponse{}
	}
}

func localSandboxFromContainer(container *runtimev1.ContainerStatus) *nodeoperatorv1.LocalSandbox {
	if container == nil {
		return nil
	}
	return &nodeoperatorv1.LocalSandbox{
		SandboxID:     container.GetID(),
		RuntimeClass:  container.GetRuntime(),
		State:         localSandboxStateFromContainer(container.GetState()),
		ExitCode:      container.GetExitCode(),
		ExitCodeKnown: container.GetState() == runtimev1.ContainerState_CONTAINER_EXITED,
		Message:       container.GetMessage(),
		Pid:           container.GetPid(),
		StartedAt:     timestampFromUnixSeconds(container.GetStartedAt()),
		FinishedAt:    timestampFromUnixSeconds(container.GetFinishedAt()),
	}
}

func localSandboxDiagnostics(sandboxID string, diagnostics service.SandboxdDiagnostics) *nodeoperatorv1.GetSandboxDiagnosticsResponse {
	var generatedAt *timestamppb.Timestamp
	if !diagnostics.GeneratedAt.IsZero() {
		generatedAt = timestamppb.New(diagnostics.GeneratedAt)
	}
	return &nodeoperatorv1.GetSandboxDiagnosticsResponse{
		SandboxID:       sandboxID,
		Ready:           diagnostics.Ready,
		Detail:          diagnostics.Detail,
		GeneratedAt:     generatedAt,
		DaemonPid:       int32(diagnostics.Status.DaemonPID),
		UptimeSeconds:   diagnostics.Status.UptimeSeconds,
		SocketPath:      diagnostics.Status.SocketPath,
		UserState:       diagnostics.Status.UserState,
		Capabilities:    append([]string(nil), diagnostics.Capabilities...),
		Providers:       localSandboxDiagnosticProviders(diagnostics.Providers),
		ProviderSummary: localSandboxDiagnosticProviderSummary(diagnostics.ProviderSummary),
		ProcessSummary:  localSandboxDiagnosticProcessSummary(diagnostics.ProcessSummary),
		RawJson:         diagnostics.RawJSON,
	}
}

func localSandboxDiagnosticProviders(items []service.SandboxdProvider) []*nodeoperatorv1.SandboxdProvider {
	out := make([]*nodeoperatorv1.SandboxdProvider, 0, len(items))
	for _, item := range items {
		out = append(out, &nodeoperatorv1.SandboxdProvider{
			Name:         item.Name,
			State:        item.State,
			Available:    item.Available,
			Capabilities: append([]string(nil), item.Capabilities...),
			Backend:      item.Backend,
			Command:      item.Command,
			Reason:       item.Reason,
			LastError:    item.LastError,
			Dependencies: localSandboxDiagnosticProviderDependencies(item.Dependencies),
		})
	}
	return out
}

func localSandboxDiagnosticProviderDependencies(items []service.SandboxdProviderDependency) []*nodeoperatorv1.SandboxdProviderDependency {
	out := make([]*nodeoperatorv1.SandboxdProviderDependency, 0, len(items))
	for _, item := range items {
		out = append(out, &nodeoperatorv1.SandboxdProviderDependency{Name: item.Name, Available: item.Available, Reason: item.Reason})
	}
	return out
}

func localSandboxDiagnosticProviderSummary(summary service.SandboxdProviderSummary) *nodeoperatorv1.SandboxdProviderSummary {
	return &nodeoperatorv1.SandboxdProviderSummary{
		Total:       int32(summary.Total),
		Available:   int32(summary.Available),
		Degraded:    int32(summary.Degraded),
		Unavailable: int32(summary.Unavailable),
	}
}

func localSandboxDiagnosticProcessSummary(summary service.SandboxdProcessSummary) *nodeoperatorv1.SandboxdProcessSummary {
	return &nodeoperatorv1.SandboxdProcessSummary{
		Total:    int32(summary.Total),
		Starting: int32(summary.Starting),
		Running:  int32(summary.Running),
		Exited:   int32(summary.Exited),
		Failed:   int32(summary.Failed),
	}
}

func localSandboxStateFromContainer(state runtimev1.ContainerState) nodeoperatorv1.LocalSandboxState {
	switch state {
	case runtimev1.ContainerState_CONTAINER_RUNNING:
		return nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_RUNNING
	case runtimev1.ContainerState_CONTAINER_EXITED:
		return nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_EXITED
	default:
		return nodeoperatorv1.LocalSandboxState_LOCAL_SANDBOX_STATE_UNKNOWN
	}
}

func timestampFromUnixSeconds(sec int64) *timestamppb.Timestamp {
	if sec <= 0 {
		return nil
	}
	return timestamppb.New(time.Unix(sec, 0).UTC())
}
