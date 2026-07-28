package api

import (
	"context"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) Process(stream nodesandboxv1.NodeSandbox_ProcessServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "initial open payload is required")
	}
	target, err := s.validateDirectAuth(stream.Context(), open.GetAllocationID(), open.GetAttempt(), open.GetExecutionLeaseToken())
	if err != nil {
		return err
	}
	if open.GetSpec() == nil || len(open.GetSpec().GetArgv()) == 0 {
		return grpcstatus.Error(codes.InvalidArgument, "spec.argv is required")
	}
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}
	return s.svc.Process(&processAdapter{
		stream:    stream,
		first:     first,
		firstSent: false,
		targetID:  target.targetID,
	})
}

type processAdapter struct {
	stream    nodesandboxv1.NodeSandbox_ProcessServer
	first     *nodesandboxv1.ProcessRequest
	firstSent bool
	targetID  string
}

func (a *processAdapter) Context() context.Context { return a.stream.Context() }

func (a *processAdapter) Recv() (*runtimev1.ProcessRequest, error) {
	if !a.firstSent {
		a.firstSent = true
		return convertProcessRequest(a.first, a.targetID)
	}
	req, err := a.stream.Recv()
	if err != nil {
		return nil, err
	}
	return convertProcessRequest(req, a.targetID)
}

func (a *processAdapter) Send(resp *runtimev1.ProcessResponse) error {
	return a.stream.Send(convertProcessResponse(resp))
}

func convertProcessRequest(in *nodesandboxv1.ProcessRequest, targetID string) (*runtimev1.ProcessRequest, error) {
	switch payload := in.GetPayload().(type) {
	case *nodesandboxv1.ProcessRequest_Open:
		return &runtimev1.ProcessRequest{
			Payload: &runtimev1.ProcessRequest_Open{
				Open: &runtimev1.ProcessOpen{
					ID:           targetID,
					Command:      append([]string(nil), payload.Open.GetSpec().GetArgv()...),
					Tty:          payload.Open.GetSpec().GetTty(),
					Timeout:      payload.Open.GetSpec().GetTimeoutSeconds(),
					Env:          cloneStringMap(payload.Open.GetSpec().GetEnv()),
					Cwd:          payload.Open.GetSpec().GetCwd(),
					User:         payload.Open.GetSpec().GetUser(),
					ManagedProxy: convertManagedProxySpec(payload.Open.GetSpec().GetManagedProxy()),
				},
			},
		}, nil
	case *nodesandboxv1.ProcessRequest_Stdin:
		return &runtimev1.ProcessRequest{Payload: &runtimev1.ProcessRequest_Stdin{Stdin: payload.Stdin}}, nil
	case *nodesandboxv1.ProcessRequest_Resize:
		return &runtimev1.ProcessRequest{Payload: &runtimev1.ProcessRequest_Resize{Resize: &runtimev1.TerminalResize{Cols: payload.Resize.GetCols(), Rows: payload.Resize.GetRows()}}}, nil
	case *nodesandboxv1.ProcessRequest_CloseStdin:
		return &runtimev1.ProcessRequest{Payload: &runtimev1.ProcessRequest_CloseStdin{CloseStdin: payload.CloseStdin}}, nil
	case *nodesandboxv1.ProcessRequest_Signal:
		return &runtimev1.ProcessRequest{Payload: &runtimev1.ProcessRequest_Signal{Signal: &runtimev1.ProcessSignal{Signal: payload.Signal.GetSignal()}}}, nil
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "unsupported process payload")
	}
}

func convertProcessResponse(in *runtimev1.ProcessResponse) *nodesandboxv1.ProcessResponse {
	switch payload := in.GetPayload().(type) {
	case *runtimev1.ProcessResponse_Stdout:
		return &nodesandboxv1.ProcessResponse{Payload: &nodesandboxv1.ProcessResponse_Stdout{Stdout: payload.Stdout}}
	case *runtimev1.ProcessResponse_Stderr:
		return &nodesandboxv1.ProcessResponse{Payload: &nodesandboxv1.ProcessResponse_Stderr{Stderr: payload.Stderr}}
	case *runtimev1.ProcessResponse_Exit:
		return &nodesandboxv1.ProcessResponse{Payload: &nodesandboxv1.ProcessResponse_Exit{Exit: &nodesandboxv1.ExecExit{
			ExitCode:           payload.Exit.GetExitCode(),
			Message:            payload.Exit.GetMessage(),
			ManagedProxyReport: convertManagedProxyReport(payload.Exit.GetManagedProxyReport()),
		}}}
	case *runtimev1.ProcessResponse_Ready:
		return &nodesandboxv1.ProcessResponse{Payload: &nodesandboxv1.ProcessResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}}}
	default:
		return &nodesandboxv1.ProcessResponse{}
	}
}
