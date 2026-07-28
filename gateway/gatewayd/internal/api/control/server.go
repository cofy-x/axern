package control

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/cofy-x/axern/lib/go/observability"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/data/artifact/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

type Server struct {
	address string
	grpc    *grpc.Server
}

type Options struct {
	Address string
	CACert  string
	Cert    string
	Key     string
}

var publicControlServices = map[string]struct{}{
	"axern.control.admin.v1.AdminAudit":                 {},
	"axern.control.admin.v1.AdminReliability":           {},
	"axern.control.admin.v1.NodeAdmin":                  {},
	"axern.control.admin.v1.AllocationLifecycleAdmin":   {},
	"axern.control.admin.v1.ServiceAdmin":               {},
	"axern.control.admin.v1.StorageAdmin":               {},
	"axern.control.agentprofile.v1.AgentProfileControl": {},
	"axern.control.catalog.v1.RuntimeCatalog":           {},
	"axern.control.environment.v1.EnvironmentControl":   {},
	"axern.control.function.v1.FunctionControl":         {},
	"axern.control.gateway.v1.GatewayControl":           {},
	"axern.control.namespace.v1.NamespaceControl":       {},
	"axern.control.quota.v1.QuotaControl":               {},
	"axern.control.rollout.v1.RolloutControl":           {},
	"axern.control.run.v1.RunControl":                   {},
	"axern.control.secret.v1.SecretControl":             {},
	"axern.control.service.v1.ServiceControl":           {},
	"axern.control.tunnel.v1.TunnelControl":             {},
}

func New(backend *grpc.ClientConn, opts Options, obs *observability.Handle) (*Server, error) {
	if backend == nil {
		return nil, fmt.Errorf("control backend connection is required")
	}
	creds, err := loadServerCredentials(opts.CACert, opts.Cert, opts.Key)
	if err != nil {
		return nil, err
	}
	serverOpts := []grpc.ServerOption{
		grpc.Creds(creds),
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(proxyUnknownService(backend)),
	}
	if obs != nil {
		if statsHandler := obs.GRPCServerStatsHandler(); statsHandler != nil {
			serverOpts = append(serverOpts, grpc.StatsHandler(statsHandler))
		}
	}
	return &Server{
		address: opts.Address,
		grpc:    grpc.NewServer(serverOpts...),
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.grpc.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		s.grpc.GracefulStop()
		return nil
	case err := <-errCh:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}

func (s *Server) Close() {
	if s != nil && s.grpc != nil {
		s.grpc.Stop()
	}
}

func (s *Server) RegisterTunnelRelay(handler tunnelv1.TunnelRelayServer) {
	tunnelv1.RegisterTunnelRelayServer(s.grpc, handler)
}

func (s *Server) RegisterNodeSandbox(handler nodesandboxv1.NodeSandboxServer) {
	nodesandboxv1.RegisterNodeSandboxServer(s.grpc, handler)
}
func (s *Server) RegisterArtifactData(handler artifactv1.ArtifactDataServer) {
	artifactv1.RegisterArtifactDataServer(s.grpc, handler)
}

func proxyUnknownService(backend *grpc.ClientConn) grpc.StreamHandler {
	return proxyUnknownServiceForServices(backend, publicControlServices)
}

func proxyUnknownServiceForServices(backend *grpc.ClientConn, allowedServices map[string]struct{}) grpc.StreamHandler {
	return func(_ any, serverStream grpc.ServerStream) error {
		method, ok := grpc.MethodFromServerStream(serverStream)
		if !ok {
			return fmt.Errorf("control proxy could not resolve gRPC method")
		}
		service, ok := serviceNameFromMethod(method)
		if !ok {
			return grpcstatus.Errorf(codes.Unimplemented, "invalid gRPC method %q", method)
		}
		if _, ok := allowedServices[service]; !ok {
			return grpcstatus.Errorf(codes.PermissionDenied, "control service %q is not exposed by gatewayd", service)
		}
		outgoingCtx, cancel := context.WithCancel(outgoingContext(serverStream.Context()))
		defer cancel()
		desc := &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}
		var header metadata.MD
		var trailer metadata.MD
		clientStream, err := grpc.NewClientStream(
			outgoingCtx,
			desc,
			backend,
			method,
			grpc.ForceCodec(rawCodec{}),
			grpc.Header(&header),
			grpc.Trailer(&trailer),
		)
		if err != nil {
			return err
		}

		errCh := make(chan error, 2)
		go func() {
			errCh <- forwardRequests(serverStream, clientStream)
		}()
		go func() {
			errCh <- forwardResponses(serverStream, clientStream, &trailer)
		}()

		for range 2 {
			if err := <-errCh; err != nil {
				cancel()
				return err
			}
		}
		return nil
	}
}

func serviceNameFromMethod(method string) (string, bool) {
	method = strings.TrimPrefix(method, "/")
	service, _, ok := strings.Cut(method, "/")
	return service, ok && service != ""
}

func outgoingContext(ctx context.Context) context.Context {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, incoming.Copy())
}

func forwardRequests(serverStream grpc.ServerStream, clientStream grpc.ClientStream) error {
	for {
		var msg rawMessage
		err := serverStream.RecvMsg(&msg)
		if errors.Is(err, io.EOF) {
			return clientStream.CloseSend()
		}
		if err != nil {
			return err
		}
		if err := clientStream.SendMsg(msg); err != nil {
			return err
		}
	}
}

func forwardResponses(serverStream grpc.ServerStream, clientStream grpc.ClientStream, trailer *metadata.MD) error {
	header, err := clientStream.Header()
	if err != nil {
		return err
	}
	if len(header) > 0 {
		if err := serverStream.SendHeader(header); err != nil {
			return err
		}
	}
	for {
		var msg rawMessage
		err := clientStream.RecvMsg(&msg)
		if errors.Is(err, io.EOF) {
			if len(*trailer) > 0 {
				serverStream.SetTrailer(*trailer)
			}
			return nil
		}
		if err != nil {
			if len(*trailer) > 0 {
				serverStream.SetTrailer(*trailer)
			}
			return err
		}
		if err := serverStream.SendMsg(msg); err != nil {
			return err
		}
	}
}

func loadServerCredentials(caPath, certPath, keyPath string) (credentials.TransportCredentials, error) {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load control edge tls key pair: %w", err)
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read control edge tls ca cert: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse control edge tls ca cert %q", caPath)
	}
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    roots,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}), nil
}
