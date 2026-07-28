package node

import (
	"context"
	"errors"
	"io"
	"time"

	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type Resolver interface {
	ResolveAllocationTerminal(ctx context.Context, in *gatewayv1.ResolveAllocationTerminalRequest) (*gatewayv1.ResolveAllocationTerminalResponse, error)
}

type Dialer interface {
	NodeSandbox(ctx context.Context, target string) (nodesandboxv1.NodeSandboxClient, error)
}

type LeaseRetryObserver interface {
	LeaseRetry(routeType string)
}

type Options struct {
	LeaseRetryAttempts int
	LeaseRetryDelay    time.Duration
}

type Server struct {
	nodesandboxv1.UnimplementedNodeSandboxServer

	resolver Resolver
	dialer   Dialer
	options  Options
	metrics  LeaseRetryObserver
}

func New(resolver Resolver, dialer Dialer, options Options, metrics LeaseRetryObserver) *Server {
	if options.LeaseRetryAttempts <= 0 {
		options.LeaseRetryAttempts = 3
	}
	if options.LeaseRetryDelay <= 0 {
		options.LeaseRetryDelay = 500 * time.Millisecond
	}
	return &Server{resolver: resolver, dialer: dialer, options: options, metrics: metrics}
}

func (s *Server) Exec(ctx context.Context, req *nodesandboxv1.ExecRequest) (*nodesandboxv1.ExecResponse, error) {
	var response *nodesandboxv1.ExecResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Exec(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ExecImage(ctx context.Context, req *nodesandboxv1.ExecImageRequest) (*nodesandboxv1.ExecImageResponse, error) {
	var response *nodesandboxv1.ExecImageResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ExecImage(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) WaitSandbox(ctx context.Context, req *nodesandboxv1.WaitSandboxRequest) (*nodesandboxv1.WaitSandboxResponse, error) {
	var response *nodesandboxv1.WaitSandboxResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.WaitSandbox(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) CapabilityStatus(ctx context.Context, req *nodesandboxv1.CapabilityStatusRequest) (*nodesandboxv1.CapabilityStatusResponse, error) {
	var response *nodesandboxv1.CapabilityStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.CapabilityStatus(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) StatFile(ctx context.Context, req *nodesandboxv1.StatFileRequest) (*nodesandboxv1.StatFileResponse, error) {
	var response *nodesandboxv1.StatFileResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.StatFile(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ListDir(ctx context.Context, req *nodesandboxv1.ListDirRequest) (*nodesandboxv1.ListDirResponse, error) {
	var response *nodesandboxv1.ListDirResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ListDir(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ReadFile(ctx context.Context, req *nodesandboxv1.ReadFileRequest) (*nodesandboxv1.ReadFileResponse, error) {
	var response *nodesandboxv1.ReadFileResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ReadFile(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) WriteFile(ctx context.Context, req *nodesandboxv1.WriteFileRequest) (*nodesandboxv1.WriteFileResponse, error) {
	var response *nodesandboxv1.WriteFileResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.WriteFile(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) MaterializeTaskAssets(ctx context.Context, req *nodesandboxv1.MaterializeTaskAssetsRequest) (*nodesandboxv1.MaterializeTaskAssetsResponse, error) {
	var response *nodesandboxv1.MaterializeTaskAssetsResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.MaterializeTaskAssets(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Mkdir(ctx context.Context, req *nodesandboxv1.MkdirRequest) (*nodesandboxv1.MkdirResponse, error) {
	var response *nodesandboxv1.MkdirResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Mkdir(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Remove(ctx context.Context, req *nodesandboxv1.RemoveRequest) (*nodesandboxv1.RemoveResponse, error) {
	var response *nodesandboxv1.RemoveResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Remove(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Exists(ctx context.Context, req *nodesandboxv1.ExistsRequest) (*nodesandboxv1.ExistsResponse, error) {
	var response *nodesandboxv1.ExistsResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Exists(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Copy(ctx context.Context, req *nodesandboxv1.CopyRequest) (*nodesandboxv1.CopyResponse, error) {
	var response *nodesandboxv1.CopyResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Copy(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Move(ctx context.Context, req *nodesandboxv1.MoveRequest) (*nodesandboxv1.MoveResponse, error) {
	var response *nodesandboxv1.MoveResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Move(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Chmod(ctx context.Context, req *nodesandboxv1.ChmodRequest) (*nodesandboxv1.ChmodResponse, error) {
	var response *nodesandboxv1.ChmodResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Chmod(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) Touch(ctx context.Context, req *nodesandboxv1.TouchRequest) (*nodesandboxv1.TouchResponse, error) {
	var response *nodesandboxv1.TouchResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.Touch(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseStatus(ctx context.Context, req *nodesandboxv1.ComputerUseStatusRequest) (*nodesandboxv1.ComputerUseStatusResponse, error) {
	var response *nodesandboxv1.ComputerUseStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseStatus(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseScreenshot(ctx context.Context, req *nodesandboxv1.ComputerUseScreenshotRequest) (*nodesandboxv1.ComputerUseScreenshotResponse, error) {
	var response *nodesandboxv1.ComputerUseScreenshotResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseScreenshot(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseDisplay(ctx context.Context, req *nodesandboxv1.ComputerUseDisplayRequest) (*nodesandboxv1.ComputerUseDisplayResponse, error) {
	var response *nodesandboxv1.ComputerUseDisplayResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseDisplay(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseMouse(ctx context.Context, req *nodesandboxv1.ComputerUseMouseRequest) (*nodesandboxv1.ComputerUseMouseResponse, error) {
	var response *nodesandboxv1.ComputerUseMouseResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseMouse(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ComputerUseKeyboard(ctx context.Context, req *nodesandboxv1.ComputerUseKeyboardRequest) (*nodesandboxv1.ComputerUseKeyboardResponse, error) {
	var response *nodesandboxv1.ComputerUseKeyboardResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.ComputerUseKeyboard(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserStatus(ctx context.Context, req *nodesandboxv1.BrowserStatusRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserStatus(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserOpen(ctx context.Context, req *nodesandboxv1.BrowserOpenRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserOpen(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserClose(ctx context.Context, req *nodesandboxv1.BrowserCloseRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserClose(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserNavigate(ctx context.Context, req *nodesandboxv1.BrowserNavigateRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserNavigate(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserResize(ctx context.Context, req *nodesandboxv1.BrowserResizeRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserResize(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserClick(ctx context.Context, req *nodesandboxv1.BrowserClickRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserClick(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserType(ctx context.Context, req *nodesandboxv1.BrowserTypeRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserType(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) BrowserWait(ctx context.Context, req *nodesandboxv1.BrowserWaitRequest) (*nodesandboxv1.BrowserStatusResponse, error) {
	var response *nodesandboxv1.BrowserStatusResponse
	err := s.unary(ctx, req, func(client nodesandboxv1.NodeSandboxClient) error {
		var err error
		response, err = client.BrowserWait(ctx, req)
		return err
	})
	return response, err
}

func (s *Server) ExecStream(stream nodesandboxv1.NodeSandbox_ExecStreamServer) error {
	first, err := stream.Recv()
	if err != nil {
		return streamOpenError(err, "exec stream")
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "exec stream must start with open")
	}
	return s.withResolvedClient(stream.Context(), open, isLeaseOpenRejection, func(client nodesandboxv1.NodeSandboxClient) error {
		up, err := client.ExecStream(stream.Context())
		if err != nil {
			return err
		}
		defer up.CloseSend()
		if err := up.Send(first); err != nil {
			return markLeaseOpenRejection(err)
		}
		header, err := acceptedExecutionLeaseHeader(up, "exec stream", func() error {
			_, err := up.Recv()
			return err
		})
		if err != nil {
			return err
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		return bridgeExecStream(stream, up)
	})
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
	return bidi(s, stream.Context(), open, isLeaseOpenRejection, func(client nodesandboxv1.NodeSandboxClient) (processClient, error) {
		return client.Process(stream.Context())
	}, func(up processClient) error {
		if err := up.Send(first); err != nil {
			return markLeaseOpenRejection(err)
		}
		header, err := acceptedExecutionLeaseHeader(up, "process", func() error {
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

func (s *Server) ProcessImage(stream nodesandboxv1.NodeSandbox_ProcessImageServer) error {
	first, err := stream.Recv()
	if err != nil {
		return streamOpenError(err, "image process")
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "image process stream must start with open")
	}
	return bidi(s, stream.Context(), open, isLeaseOpenRejection, func(client nodesandboxv1.NodeSandboxClient) (processImageClient, error) {
		return client.ProcessImage(stream.Context())
	}, func(up processImageClient) error {
		if err := up.Send(first); err != nil {
			return markLeaseOpenRejection(err)
		}
		header, err := acceptedExecutionLeaseHeader(up, "image process", func() error {
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
		return bridgeProcessImage(stream, up)
	})
}

func (s *Server) ProxyHTTP(stream nodesandboxv1.NodeSandbox_ProxyHTTPServer) error {
	first, err := stream.Recv()
	if err != nil {
		return streamOpenError(err, "proxy http")
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "proxy http stream must start with open")
	}
	return bidi(s, stream.Context(), open, isLeaseOpenRejection, func(client nodesandboxv1.NodeSandboxClient) (proxyHTTPClient, error) {
		return client.ProxyHTTP(stream.Context())
	}, func(up proxyHTTPClient) error {
		if err := up.Send(first); err != nil {
			return markLeaseOpenRejection(err)
		}
		header, err := acceptedExecutionLeaseHeader(up, "proxy http", func() error {
			_, err := up.Recv()
			return err
		})
		if err != nil {
			return err
		}
		if err := stream.SendHeader(header); err != nil {
			return err
		}
		return bridgeProxyHTTP(stream, up)
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
	err = bidi(s, stream.Context(), open, isLeaseOpenRejection, func(client nodesandboxv1.NodeSandboxClient) (uploadArchiveClient, error) {
		return client.UploadArchive(stream.Context())
	}, func(up uploadArchiveClient) error {
		if err := up.Send(first); err != nil {
			return markLeaseOpenRejection(err)
		}
		header, err := acceptedExecutionLeaseHeader(up, "archive upload", func() error {
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
	return serverStream(s, stream.Context(), req, isLeaseOpenRejection, func(client nodesandboxv1.NodeSandboxClient) (downloadArchiveClient, error) {
		return client.DownloadArchive(stream.Context(), req)
	}, func(up downloadArchiveClient) error {
		header, err := acceptedExecutionLeaseHeader(up, "archive download", func() error {
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
