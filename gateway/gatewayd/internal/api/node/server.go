package node

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/cofy-x/axern/gateway/gatewayd/internal/auth"
	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Resolver interface {
	ResolveAllocationTerminal(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error)
}

type Dialer interface {
	NodeSandbox(ctx context.Context, target, nodeID string) (nodesandboxv1.NodeSandboxClient, error)
}

type AccessGrantRetryObserver interface {
	AccessGrantRetry(routeType string)
}

type Options struct {
	AccessGrantRetryAttempts int
	AccessGrantRetryDelay    time.Duration
	ClientFingerprint        func(context.Context) (string, error)
}

type Server struct {
	nodesandboxv1.UnimplementedNodeSandboxServer

	resolver Resolver
	dialer   Dialer
	options  Options
	metrics  AccessGrantRetryObserver
}

func New(resolver Resolver, dialer Dialer, options Options, metrics AccessGrantRetryObserver) *Server {
	if options.AccessGrantRetryAttempts <= 0 {
		options.AccessGrantRetryAttempts = nodekernel.DefaultAccessGrantRetryAttempts
	}
	if options.AccessGrantRetryDelay <= 0 {
		options.AccessGrantRetryDelay = nodekernel.DefaultAccessGrantRetryBaseDelay
	}
	if options.ClientFingerprint == nil {
		options.ClientFingerprint = auth.CertificateFingerprint
	}
	return &Server{resolver: resolver, dialer: dialer, options: options, metrics: metrics}
}

func (s *Server) Exec(ctx context.Context, req *nodesandboxv1.ExecRequest) (*nodesandboxv1.ExecResponse, error) {
	var response *nodesandboxv1.ExecResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Exec(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) CapabilityStatus(ctx context.Context, req *nodesandboxv1.CapabilityStatusRequest) (*nodesandboxv1.CapabilityStatusResponse, error) {
	var response *nodesandboxv1.CapabilityStatusResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.CapabilityStatus(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) StatFile(ctx context.Context, req *nodesandboxv1.StatFileRequest) (*nodesandboxv1.StatFileResponse, error) {
	var response *nodesandboxv1.StatFileResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.StatFile(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ListDir(ctx context.Context, req *nodesandboxv1.ListDirRequest) (*nodesandboxv1.ListDirResponse, error) {
	var response *nodesandboxv1.ListDirResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ListDir(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ReadFile(ctx context.Context, req *nodesandboxv1.ReadFileRequest) (*nodesandboxv1.ReadFileResponse, error) {
	var response *nodesandboxv1.ReadFileResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ReadFile(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) WriteFile(ctx context.Context, req *nodesandboxv1.WriteFileRequest) (*nodesandboxv1.WriteFileResponse, error) {
	var response *nodesandboxv1.WriteFileResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.WriteFile(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Mkdir(ctx context.Context, req *nodesandboxv1.MkdirRequest) (*nodesandboxv1.MkdirResponse, error) {
	var response *nodesandboxv1.MkdirResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Mkdir(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Remove(ctx context.Context, req *nodesandboxv1.RemoveRequest) (*nodesandboxv1.RemoveResponse, error) {
	var response *nodesandboxv1.RemoveResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Remove(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Exists(ctx context.Context, req *nodesandboxv1.ExistsRequest) (*nodesandboxv1.ExistsResponse, error) {
	var response *nodesandboxv1.ExistsResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Exists(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Copy(ctx context.Context, req *nodesandboxv1.CopyRequest) (*nodesandboxv1.CopyResponse, error) {
	var response *nodesandboxv1.CopyResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Copy(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Move(ctx context.Context, req *nodesandboxv1.MoveRequest) (*nodesandboxv1.MoveResponse, error) {
	var response *nodesandboxv1.MoveResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Move(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Chmod(ctx context.Context, req *nodesandboxv1.ChmodRequest) (*nodesandboxv1.ChmodResponse, error) {
	var response *nodesandboxv1.ChmodResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Chmod(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Touch(ctx context.Context, req *nodesandboxv1.TouchRequest) (*nodesandboxv1.TouchResponse, error) {
	var response *nodesandboxv1.TouchResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Touch(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseStatus(ctx context.Context, req *nodesandboxv1.ComputerUseStatusRequest) (*nodesandboxv1.ComputerUseStatusResponse, error) {
	var response *nodesandboxv1.ComputerUseStatusResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseStatus(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseScreenshot(ctx context.Context, req *nodesandboxv1.ComputerUseScreenshotRequest) (*nodesandboxv1.ComputerUseScreenshotResponse, error) {
	var response *nodesandboxv1.ComputerUseScreenshotResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseScreenshot(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseDisplay(ctx context.Context, req *nodesandboxv1.ComputerUseDisplayRequest) (*nodesandboxv1.ComputerUseDisplayResponse, error) {
	var response *nodesandboxv1.ComputerUseDisplayResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseDisplay(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseMouse(ctx context.Context, req *nodesandboxv1.ComputerUseMouseRequest) (*nodesandboxv1.ComputerUseMouseResponse, error) {
	var response *nodesandboxv1.ComputerUseMouseResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseMouse(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseKeyboard(ctx context.Context, req *nodesandboxv1.ComputerUseKeyboardRequest) (*nodesandboxv1.ComputerUseKeyboardResponse, error) {
	var response *nodesandboxv1.ComputerUseKeyboardResponse
	err := s.unary(ctx, req, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseKeyboard(backendCtx, req)
		return err
	})
	return response, err
}

func (s *Server) Process(stream nodesandboxv1.NodeSandbox_ProcessServer) error {
	first, err := stream.Recv()
	if err != nil {
		return streamOpenError(err, "process")
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "process stream must start with open")
	}
	return bidi(s, stream.Context(), open, isAccessGrantOpenRejection, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) (processClient, error) {
		return client.Process(backendCtx)
	}, func(up processClient) error {
		if err := up.Send(first); err != nil {
			return markAccessGrantOpenRejection(err)
		}
		header, err := acceptedAllocationAccessGrantHeader(up, "process", func() error {
			_, err := up.Recv()
			return err
		})
		if err != nil {
			return err
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		initial, err := up.Recv()
		if err != nil {
			return err
		}
		if err := stream.Send(initial); err != nil {
			return err
		}
		return bridgeProcess(stream, up)
	})
}

func (s *Server) UploadArchive(stream nodesandboxv1.NodeSandbox_UploadArchiveServer) error {
	first, err := stream.Recv()
	if err != nil {
		return streamOpenError(err, "upload archive")
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "upload archive stream must start with open")
	}
	var response *nodesandboxv1.UploadArchiveResponse
	err = bidi(s, stream.Context(), open, isAccessGrantOpenRejection, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) (uploadArchiveClient, error) {
		return client.UploadArchive(backendCtx)
	}, func(up uploadArchiveClient) error {
		if err := up.Send(first); err != nil {
			return markAccessGrantOpenRejection(err)
		}
		header, err := acceptedAllocationAccessGrantHeader(up, "archive upload", func() error {
			_, err := up.CloseAndRecv()
			return err
		})
		if err != nil {
			return err
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		for {
			req, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				var closeErr error
				response, closeErr = up.CloseAndRecv()
				return closeErr
			}
			if err != nil {
				_ = up.CloseSend()
				return err
			}
			if err := up.Send(req); err != nil {
				return err
			}
		}
	})
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}

func (s *Server) DownloadArchive(req *nodesandboxv1.DownloadArchiveRequest, stream nodesandboxv1.NodeSandbox_DownloadArchiveServer) error {
	return serverStream(s, stream.Context(), req, isAccessGrantOpenRejection, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) (downloadArchiveClient, error) {
		return client.DownloadArchive(backendCtx, req)
	}, func(up downloadArchiveClient) error {
		header, err := acceptedAllocationAccessGrantHeader(up, "archive download", func() error {
			_, err := up.Recv()
			return err
		})
		if err != nil {
			return err
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		for {
			response, err := up.Recv()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := stream.Send(response); err != nil {
				return err
			}
		}
	})
}

func (s *Server) ReadOutput(req *nodesandboxv1.ReadOutputRequest, stream nodesandboxv1.NodeSandbox_ReadOutputServer) error {
	return serverStreamForPurpose(s, stream.Context(), req, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, isAccessGrantOpenRejection, func(backendCtx context.Context, client nodesandboxv1.NodeSandboxClient) (nodesandboxv1.NodeSandbox_ReadOutputClient, error) {
		return client.ReadOutput(backendCtx, req)
	}, func(up nodesandboxv1.NodeSandbox_ReadOutputClient) error {
		header, err := acceptedAllocationAccessGrantHeader(up, "run output", func() error {
			_, err := up.Recv()
			return err
		})
		if err != nil {
			return err
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		for {
			response, err := up.Recv()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := stream.Send(response); err != nil {
				return err
			}
		}
	})
}
