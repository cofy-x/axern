package api

import (
	"context"
	"maps"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) Exec(ctx context.Context, req *nodesandboxv1.ExecRequest) (*nodesandboxv1.ExecResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if req.GetSpec() == nil || len(req.GetSpec().GetArgv()) == 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "spec.argv is required")
	}

	resp, err := s.svc.Exec(ctx, &runtimev1.ExecRequest{
		ID:           target.targetID,
		Command:      append([]string(nil), req.GetSpec().GetArgv()...),
		Timeout:      req.GetSpec().GetTimeoutSeconds(),
		Env:          cloneStringMap(req.GetSpec().GetEnv()),
		Cwd:          req.GetSpec().GetCwd(),
		User:         req.GetSpec().GetUser(),
		ManagedProxy: convertManagedProxySpec(req.GetSpec().GetManagedProxy()),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ExecResponse{
		ExitCode:           resp.GetExitCode(),
		Stdout:             resp.GetStdout(),
		Stderr:             resp.GetStderr(),
		StdoutTruncated:    resp.GetStdoutTruncated(),
		StderrTruncated:    resp.GetStderrTruncated(),
		ManagedProxyReport: convertManagedProxyReport(resp.GetManagedProxyReport()),
	}, nil
}

func (s *nodeSandboxServer) ExecStream(stream nodesandboxv1.NodeSandbox_ExecStreamServer) error {
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
	// Force response headers after authentication so gateway clients can
	// distinguish an accepted stream from a rejected lease before forwarding
	// terminal input.
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}

	adapter := &execStreamAdapter{
		stream:    stream,
		first:     first,
		firstSent: false,
		targetID:  target.targetID,
	}
	return s.svc.ExecStream(adapter)
}

type execStreamAdapter struct {
	stream    nodesandboxv1.NodeSandbox_ExecStreamServer
	first     *nodesandboxv1.ExecStreamRequest
	firstSent bool
	targetID  string
}

func (a *execStreamAdapter) SetHeader(md metadata.MD) error  { return a.stream.SetHeader(md) }
func (a *execStreamAdapter) SendHeader(md metadata.MD) error { return a.stream.SendHeader(md) }
func (a *execStreamAdapter) SetTrailer(md metadata.MD)       { a.stream.SetTrailer(md) }
func (a *execStreamAdapter) Context() context.Context        { return a.stream.Context() }
func (a *execStreamAdapter) SendMsg(m any) error             { return a.stream.SendMsg(m) }
func (a *execStreamAdapter) RecvMsg(m any) error             { return a.stream.RecvMsg(m) }

func (a *execStreamAdapter) Recv() (*runtimev1.ExecStreamRequest, error) {
	if !a.firstSent {
		a.firstSent = true
		return convertExecStreamRequest(a.first, a.targetID)
	}
	req, err := a.stream.Recv()
	if err != nil {
		return nil, err
	}
	return convertExecStreamRequest(req, a.targetID)
}

func (a *execStreamAdapter) Send(resp *runtimev1.ExecStreamResponse) error {
	out, _ := convertExecStreamResponse(resp)
	return a.stream.Send(out)
}

func convertExecStreamRequest(in *nodesandboxv1.ExecStreamRequest, targetID string) (*runtimev1.ExecStreamRequest, error) {
	switch payload := in.GetPayload().(type) {
	case *nodesandboxv1.ExecStreamRequest_Open:
		return &runtimev1.ExecStreamRequest{
			Payload: &runtimev1.ExecStreamRequest_Open{
				Open: &runtimev1.ExecStreamOpen{
					ID:           targetID,
					Command:      append([]string(nil), payload.Open.GetSpec().GetArgv()...),
					Tty:          payload.Open.GetSpec().GetTty(),
					Timeout:      payload.Open.GetSpec().GetTimeoutSeconds(),
					Env:          cloneStringMap(payload.Open.GetSpec().GetEnv()),
					Cwd:          payload.Open.GetSpec().GetCwd(),
					User:         payload.Open.GetSpec().GetUser(),
					InitialSize:  convertNodeTerminalResize(payload.Open.GetInitialSize()),
					ManagedProxy: convertManagedProxySpec(payload.Open.GetSpec().GetManagedProxy()),
				},
			},
		}, nil
	case *nodesandboxv1.ExecStreamRequest_Stdin:
		return &runtimev1.ExecStreamRequest{Payload: &runtimev1.ExecStreamRequest_Stdin{Stdin: payload.Stdin}}, nil
	case *nodesandboxv1.ExecStreamRequest_Resize:
		return &runtimev1.ExecStreamRequest{
			Payload: &runtimev1.ExecStreamRequest_Resize{
				Resize: &runtimev1.TerminalResize{Cols: payload.Resize.GetCols(), Rows: payload.Resize.GetRows()},
			},
		}, nil
	case *nodesandboxv1.ExecStreamRequest_CloseStdin:
		return &runtimev1.ExecStreamRequest{Payload: &runtimev1.ExecStreamRequest_CloseStdin{CloseStdin: payload.CloseStdin}}, nil
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "unsupported exec stream payload")
	}
}

func convertNodeTerminalResize(in *nodesandboxv1.TerminalResize) *runtimev1.TerminalResize {
	if in == nil {
		return nil
	}
	return &runtimev1.TerminalResize{Cols: in.GetCols(), Rows: in.GetRows()}
}

func convertExecStreamResponse(in *runtimev1.ExecStreamResponse) (*nodesandboxv1.ExecStreamResponse, *nodesandboxv1.ExecExit) {
	switch payload := in.GetPayload().(type) {
	case *runtimev1.ExecStreamResponse_Stdout:
		return &nodesandboxv1.ExecStreamResponse{Payload: &nodesandboxv1.ExecStreamResponse_Stdout{Stdout: payload.Stdout}}, nil
	case *runtimev1.ExecStreamResponse_Stderr:
		return &nodesandboxv1.ExecStreamResponse{Payload: &nodesandboxv1.ExecStreamResponse_Stderr{Stderr: payload.Stderr}}, nil
	case *runtimev1.ExecStreamResponse_Exit:
		exit := &nodesandboxv1.ExecExit{
			ExitCode:           payload.Exit.GetExitCode(),
			Message:            payload.Exit.GetMessage(),
			ManagedProxyReport: convertManagedProxyReport(payload.Exit.GetManagedProxyReport()),
		}
		return &nodesandboxv1.ExecStreamResponse{Payload: &nodesandboxv1.ExecStreamResponse_Exit{Exit: exit}}, exit
	default:
		return &nodesandboxv1.ExecStreamResponse{}, nil
	}
}

func convertManagedProxySpec(in *nodesandboxv1.ManagedProxySpec) *runtimev1.ManagedProxySpec {
	if in == nil {
		return nil
	}
	return &runtimev1.ManagedProxySpec{
		Provider:            in.GetProvider(),
		UpstreamBaseUrl:     in.GetUpstreamBaseUrl(),
		UpstreamBearerToken: in.GetUpstreamBearerToken(),
	}
}

func convertManagedProxyReport(in *runtimev1.ManagedProxyReport) *nodesandboxv1.ManagedProxyReport {
	if in == nil {
		return nil
	}
	return &nodesandboxv1.ManagedProxyReport{
		Provider:      in.GetProvider(),
		RequestCount:  in.GetRequestCount(),
		ResponseCount: in.GetResponseCount(),
		ErrorCount:    in.GetErrorCount(),
		ReportJson:    append([]byte(nil), in.GetReportJson()...),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
