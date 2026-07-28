package tunnel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpcstatus "google.golang.org/grpc/status"
)

type Options struct {
	Target      string
	Resolver    TargetResolver
	CACert      string
	ServerName  string
	DialTimeout time.Duration
	DialOptions []grpc.DialOption
}

type TargetResolver interface {
	ResolveTunnelRelayTarget(context.Context, string) (string, error)
}

type Server struct {
	tunnelv1.UnimplementedTunnelRelayServer

	opts Options
}

func New(opts Options) (*Server, error) {
	if strings.TrimSpace(opts.Target) == "" && opts.Resolver == nil {
		return nil, fmt.Errorf("tunnel-relay-target or tunnel relay target resolver is required when tunnel relay edge is enabled")
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = 15 * time.Second
	}
	return &Server{opts: opts}, nil
}

func (s *Server) ConnectPeer(edgeStream tunnelv1.TunnelRelay_ConnectPeerServer) error {
	firstFrame, err := edgeStream.Recv()
	if err != nil {
		return err
	}
	if err := validateClientPeerOpen(firstFrame); err != nil {
		return err
	}
	target, err := s.resolveTarget(edgeStream.Context(), firstFrame.GetPeerOpen().GetSessionID())
	if err != nil {
		return err
	}
	backend, err := s.dialBackend(edgeStream.Context(), target)
	if err != nil {
		return err
	}
	defer backend.Close()
	backendStream, err := tunnelv1.NewTunnelRelayClient(backend).ConnectPeer(edgeStream.Context())
	if err != nil {
		return err
	}
	if err := backendStream.Send(firstFrame); err != nil {
		return err
	}

	errCh := make(chan error, 2)
	go func() {
		errCh <- copyEdgeToBackend(edgeStream, backendStream)
	}()
	go func() {
		errCh <- copyBackendToEdge(backendStream, edgeStream)
	}()
	for range 2 {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) resolveTarget(ctx context.Context, sessionID string) (string, error) {
	if s.opts.Resolver != nil {
		target, err := s.opts.Resolver.ResolveTunnelRelayTarget(ctx, sessionID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(target) == "" {
			return "", grpcstatus.Error(codes.FailedPrecondition, "tunnel session has no internal relay target")
		}
		return strings.TrimSpace(target), nil
	}
	return strings.TrimSpace(s.opts.Target), nil
}

func (s *Server) dialBackend(ctx context.Context, target string) (*grpc.ClientConn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, s.opts.DialTimeout)
	defer cancel()
	dialOptions, err := backendDialOptions(s.opts)
	if err != nil {
		return nil, err
	}
	dialOptions = append(dialOptions, s.opts.DialOptions...)
	return grpcclient.NewReadyClient(dialCtx, grpcclient.PassthroughTarget(target), dialOptions...)
}

func backendDialOptions(opts Options) ([]grpc.DialOption, error) {
	if strings.TrimSpace(opts.CACert) == "" {
		return nil, fmt.Errorf("tunnel-relay-tls-ca-cert is required")
	}
	caPEM, err := os.ReadFile(opts.CACert)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse tunnel relay tls ca cert %q", opts.CACert)
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: strings.TrimSpace(opts.ServerName),
	}))}, nil
}

func validateClientPeerOpen(frame *tunnelv1.TunnelFrame) error {
	open := frame.GetPeerOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "first tunnel relay frame must be peer_open")
	}
	if strings.TrimSpace(open.GetSessionID()) == "" {
		return grpcstatus.Error(codes.InvalidArgument, "peer_open session_id is required")
	}
	if open.GetPeerKind() != tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT {
		return grpcstatus.Error(codes.PermissionDenied, "gateway tunnel edge only accepts client peers")
	}
	return nil
}

func copyEdgeToBackend(edgeStream tunnelv1.TunnelRelay_ConnectPeerServer, backendStream tunnelv1.TunnelRelay_ConnectPeerClient) error {
	for {
		frame, err := edgeStream.Recv()
		if errors.Is(err, io.EOF) {
			return backendStream.CloseSend()
		}
		if err != nil {
			_ = backendStream.CloseSend()
			return err
		}
		if err := backendStream.Send(frame); err != nil {
			return err
		}
	}
}

func copyBackendToEdge(backendStream tunnelv1.TunnelRelay_ConnectPeerClient, edgeStream tunnelv1.TunnelRelay_ConnectPeerServer) error {
	for {
		frame, err := backendStream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := edgeStream.Send(frame); err != nil {
			return err
		}
	}
}
