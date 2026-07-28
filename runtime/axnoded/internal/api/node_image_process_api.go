package api

import (
	"context"
	"strings"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) ExecImage(ctx context.Context, req *nodesandboxv1.ExecImageRequest) (*nodesandboxv1.ExecImageResponse, error) {
	target, err := s.validateDirectAuth(ctx, req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return nil, err
	}
	if err := validateImageProcessSpec(req.GetSpec()); err != nil {
		return nil, err
	}

	resp, err := s.svc.ExecImage(ctx, &runtimev1.ExecImageRequest{
		ID:   target.targetID,
		Spec: convertImageProcessSpec(req.GetSpec()),
	})
	if err != nil {
		return nil, err
	}
	return &nodesandboxv1.ExecImageResponse{
		ExitCode:           resp.GetExitCode(),
		Stdout:             resp.GetStdout(),
		Stderr:             resp.GetStderr(),
		StdoutTruncated:    resp.GetStdoutTruncated(),
		StderrTruncated:    resp.GetStderrTruncated(),
		ManagedProxyReport: convertManagedProxyReport(resp.GetManagedProxyReport()),
	}, nil
}

func (s *nodeSandboxServer) ProcessImage(stream nodesandboxv1.NodeSandbox_ProcessImageServer) error {
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
	if err := validateImageProcessSpec(open.GetSpec()); err != nil {
		return err
	}
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}
	return s.svc.ProcessImage(&processImageAdapter{
		stream:    stream,
		first:     first,
		firstSent: false,
		targetID:  target.targetID,
	})
}

type processImageAdapter struct {
	stream    nodesandboxv1.NodeSandbox_ProcessImageServer
	first     *nodesandboxv1.ProcessImageRequest
	firstSent bool
	targetID  string
}

func (a *processImageAdapter) Context() context.Context { return a.stream.Context() }

func (a *processImageAdapter) Recv() (*runtimev1.ProcessImageRequest, error) {
	if !a.firstSent {
		a.firstSent = true
		return convertProcessImageRequest(a.first, a.targetID)
	}
	req, err := a.stream.Recv()
	if err != nil {
		return nil, err
	}
	return convertProcessImageRequest(req, a.targetID)
}

func (a *processImageAdapter) Send(resp *runtimev1.ProcessImageResponse) error {
	return a.stream.Send(convertProcessImageResponse(resp))
}

func validateImageProcessSpec(spec *nodesandboxv1.ImageProcessSpec) error {
	switch {
	case spec == nil:
		return grpcstatus.Error(codes.InvalidArgument, "spec is required")
	case strings.TrimSpace(spec.GetImage()) == "":
		return grpcstatus.Error(codes.InvalidArgument, "spec.image is required")
	case len(spec.GetArgv()) == 0:
		return grpcstatus.Error(codes.InvalidArgument, "spec.argv is required")
	default:
		return nil
	}
}

func convertImageProcessSpec(in *nodesandboxv1.ImageProcessSpec) *runtimev1.ImageProcessSpec {
	if in == nil {
		return nil
	}
	mounts := make([]*runtimev1.ImageProcessMount, 0, len(in.GetMounts()))
	for _, mount := range in.GetMounts() {
		if mount == nil {
			continue
		}
		mounts = append(mounts, &runtimev1.ImageProcessMount{
			SandboxPath: mount.GetSandboxPath(),
			TargetPath:  mount.GetTargetPath(),
			Readonly:    mount.GetReadonly(),
			Options:     append([]string(nil), mount.GetOptions()...),
		})
	}
	return &runtimev1.ImageProcessSpec{
		Image:        in.GetImage(),
		Command:      append([]string(nil), in.GetArgv()...),
		Tty:          in.GetTty(),
		Timeout:      in.GetTimeoutSeconds(),
		Env:          cloneStringMap(in.GetEnv()),
		Cwd:          in.GetCwd(),
		User:         in.GetUser(),
		Mounts:       mounts,
		ManagedProxy: convertManagedProxySpec(in.GetManagedProxy()),
	}
}

func convertProcessImageRequest(in *nodesandboxv1.ProcessImageRequest, targetID string) (*runtimev1.ProcessImageRequest, error) {
	switch payload := in.GetPayload().(type) {
	case *nodesandboxv1.ProcessImageRequest_Open:
		return &runtimev1.ProcessImageRequest{
			Payload: &runtimev1.ProcessImageRequest_Open{
				Open: &runtimev1.ProcessImageOpen{
					ID:   targetID,
					Spec: convertImageProcessSpec(payload.Open.GetSpec()),
				},
			},
		}, nil
	case *nodesandboxv1.ProcessImageRequest_Stdin:
		return &runtimev1.ProcessImageRequest{Payload: &runtimev1.ProcessImageRequest_Stdin{Stdin: payload.Stdin}}, nil
	case *nodesandboxv1.ProcessImageRequest_Resize:
		return &runtimev1.ProcessImageRequest{Payload: &runtimev1.ProcessImageRequest_Resize{Resize: &runtimev1.TerminalResize{Cols: payload.Resize.GetCols(), Rows: payload.Resize.GetRows()}}}, nil
	case *nodesandboxv1.ProcessImageRequest_CloseStdin:
		return &runtimev1.ProcessImageRequest{Payload: &runtimev1.ProcessImageRequest_CloseStdin{CloseStdin: payload.CloseStdin}}, nil
	case *nodesandboxv1.ProcessImageRequest_Signal:
		return &runtimev1.ProcessImageRequest{Payload: &runtimev1.ProcessImageRequest_Signal{Signal: &runtimev1.ProcessSignal{Signal: payload.Signal.GetSignal()}}}, nil
	default:
		return nil, grpcstatus.Error(codes.InvalidArgument, "unsupported image process payload")
	}
}

func convertProcessImageResponse(in *runtimev1.ProcessImageResponse) *nodesandboxv1.ProcessImageResponse {
	switch payload := in.GetPayload().(type) {
	case *runtimev1.ProcessImageResponse_Stdout:
		return &nodesandboxv1.ProcessImageResponse{Payload: &nodesandboxv1.ProcessImageResponse_Stdout{Stdout: payload.Stdout}}
	case *runtimev1.ProcessImageResponse_Stderr:
		return &nodesandboxv1.ProcessImageResponse{Payload: &nodesandboxv1.ProcessImageResponse_Stderr{Stderr: payload.Stderr}}
	case *runtimev1.ProcessImageResponse_Exit:
		return &nodesandboxv1.ProcessImageResponse{Payload: &nodesandboxv1.ProcessImageResponse_Exit{Exit: &nodesandboxv1.ExecExit{
			ExitCode:           payload.Exit.GetExitCode(),
			Message:            payload.Exit.GetMessage(),
			ManagedProxyReport: convertManagedProxyReport(payload.Exit.GetManagedProxyReport()),
		}}}
	case *runtimev1.ProcessImageResponse_Ready:
		return &nodesandboxv1.ProcessImageResponse{Payload: &nodesandboxv1.ProcessImageResponse_Ready{Ready: &nodesandboxv1.ProcessReady{}}}
	default:
		return &nodesandboxv1.ProcessImageResponse{}
	}
}
