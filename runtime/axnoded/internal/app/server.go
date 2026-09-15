package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/api"
	controlplane "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/trace"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	nodenetworkv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/network/v1"
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func serve(ctx context.Context, opts options, cfg config.Config, obs *sdkobs.Handle) error {
	svc, err := service.NewSandboxService(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create sandbox service: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.StopTimeout)
		defer cancel()
		if err := svc.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Warn("shutdown sandbox service")
		}
	}()

	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("run sandbox service: %w", err)
	}

	var nodeLis net.Listener
	var nodeGRPCServer *grpc.Server
	var nodeHealthServer *grpc_health.Server
	var localLis net.Listener
	var localGRPCServer *grpc.Server
	var localHealthServer *grpc_health.Server
	var conformanceLis net.Listener
	var conformanceGRPCServer *grpc.Server
	var conformanceHealthServer *grpc_health.Server
	var networkLis net.Listener
	var networkGRPCServer *grpc.Server
	hostname, _ := os.Hostname()
	nodeID := cfg.PluginConfig.ControlPlaneNodeIDValue(hostname)
	controlPlaneConfig := cfg.PluginConfig
	accessGrantCache := controlplane.NewAccessGrantCache()
	var accessGrantValidator api.DirectAccessGrantValidator
	accessGrantWatcher := controlplane.NewAccessGrantWatcher(
		controlplane.WithAccessGrantWatcherTarget(controlPlaneConfig.ControlPlaneTarget),
		controlplane.WithAccessGrantWatcherNode(nodeID, controlPlaneConfig.ControlPlaneNodeCredentialValue()),
		controlplane.WithAccessGrantWatcherTLS(
			controlPlaneConfig.ControlPlaneTLSCACert,
			controlPlaneConfig.ControlPlaneTLSCert,
			controlPlaneConfig.ControlPlaneTLSKey,
		),
		controlplane.WithAccessGrantWatcherCache(accessGrantCache),
	)
	if accessGrantWatcher != nil {
		accessGrantValidator = accessGrantCache
		accessGrantWatcher.Start()
		defer accessGrantWatcher.Stop()
	}

	if strings.TrimSpace(opts.socketPath) != "" {
		localLis, err = listenUnix(opts.socketPath, 0o600)
		if err != nil {
			return fmt.Errorf("listen local grpc %s: %w", opts.socketPath, err)
		}
		defer localLis.Close()

		localHealthServer = grpc_health.NewServer()
		localOptions := []grpc.ServerOption{grpc.UnaryInterceptor(trace.InjectTraceInterceptor)}
		if handler := obs.GRPCServerStatsHandler(); handler != nil {
			localOptions = append(localOptions, grpc.StatsHandler(handler))
		}
		localGRPCServer = grpc.NewServer(localOptions...)
		nodeoperatorv1.RegisterNodeOperatorServer(localGRPCServer, api.NewNodeOperatorServer(svc))
		healthpb.RegisterHealthServer(localGRPCServer, localHealthServer)
	}

	if strings.TrimSpace(opts.conformanceSocketPath) != "" {
		conformanceLis, err = listenUnix(opts.conformanceSocketPath, 0o600)
		if err != nil {
			return fmt.Errorf("listen conformance grpc %s: %w", opts.conformanceSocketPath, err)
		}
		defer conformanceLis.Close()
		conformanceHealthServer = grpc_health.NewServer()
		conformanceOptions := []grpc.ServerOption{grpc.UnaryInterceptor(trace.InjectTraceInterceptor)}
		if handler := obs.GRPCServerStatsHandler(); handler != nil {
			conformanceOptions = append(conformanceOptions, grpc.StatsHandler(handler))
		}
		conformanceGRPCServer = grpc.NewServer(conformanceOptions...)
		nodesandboxv1.RegisterNodeSandboxServer(conformanceGRPCServer, api.NewLocalNodeSandboxServer(svc, nodeID))
		nodelifecyclev1.RegisterNodeLifecycleServer(conformanceGRPCServer, api.NewLocalNodeLifecycleServer(svc, nodeID))
		healthpb.RegisterHealthServer(conformanceGRPCServer, conformanceHealthServer)
	}

	if strings.TrimSpace(opts.networkSocketPath) != "" {
		networkLis, err = listenUnix(opts.networkSocketPath, 0o600)
		if err != nil {
			return fmt.Errorf("listen Allocation network grpc %s: %w", opts.networkSocketPath, err)
		}
		defer networkLis.Close()
		networkOptions := []grpc.ServerOption{grpc.UnaryInterceptor(trace.InjectTraceInterceptor)}
		if handler := obs.GRPCServerStatsHandler(); handler != nil {
			networkOptions = append(networkOptions, grpc.StatsHandler(handler))
		}
		networkGRPCServer = grpc.NewServer(networkOptions...)
		nodenetworkv1.RegisterAllocationNetworkServer(networkGRPCServer, api.NewAllocationNetworkServer(svc))
	}

	if strings.TrimSpace(opts.grpcAddress) != "" {
		nodeLis, err = net.Listen("tcp", opts.grpcAddress)
		if err != nil {
			return fmt.Errorf("listen node grpc %s: %w", opts.grpcAddress, err)
		}
		defer nodeLis.Close()

		nodeHealthServer = grpc_health.NewServer()
		tlsConfig, tlsErr := loadNodeServerTLS(opts)
		if tlsErr != nil {
			return tlsErr
		}
		authority := api.NewNodeIngressAuthority()
		nodeOptions := []grpc.ServerOption{
			grpc.Creds(credentials.NewTLS(tlsConfig)),
			grpc.ChainUnaryInterceptor(authority.Unary, trace.InjectTraceInterceptor),
			grpc.StreamInterceptor(authority.Stream),
		}
		if handler := obs.GRPCServerStatsHandler(); handler != nil {
			nodeOptions = append(nodeOptions, grpc.StatsHandler(handler))
		}
		nodeGRPCServer = grpc.NewServer(nodeOptions...)
		nodesandboxv1.RegisterNodeSandboxServer(nodeGRPCServer, api.NewNodeSandboxServer(svc, nodeID, accessGrantValidator))
		nodelifecyclev1.RegisterNodeLifecycleServer(nodeGRPCServer, api.NewNodeLifecycleServer(svc, nodeID))
		healthpb.RegisterHealthServer(nodeGRPCServer, nodeHealthServer)
	}

	healthCtx, stopHealth := context.WithCancel(context.Background())
	defer stopHealth()
	healthEndpoints := []healthEndpoint{
		{server: localHealthServer, services: []string{nodeoperatorv1.NodeOperator_ServiceDesc.ServiceName}},
		{server: conformanceHealthServer, services: []string{nodesandboxv1.NodeSandbox_ServiceDesc.ServiceName, nodelifecyclev1.NodeLifecycle_ServiceDesc.ServiceName}},
		{server: nodeHealthServer, services: []string{nodesandboxv1.NodeSandbox_ServiceDesc.ServiceName, nodelifecyclev1.NodeLifecycle_ServiceDesc.ServiceName}},
	}
	updateHealth(svc, healthEndpoints...)
	go publishHealth(healthCtx, svc, healthEndpoints...)

	httpServer := &http.Server{
		Addr:              opts.httpAddress,
		Handler:           obs.HTTPHandler(api.NewHTTPMux(svc), sandboxobs.SpanHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}

	localErrCh := make(chan error, 1)
	if localGRPCServer != nil {
		go func() {
			localErrCh <- localGRPCServer.Serve(localLis)
		}()
	}
	conformanceErrCh := make(chan error, 1)
	if conformanceGRPCServer != nil {
		go func() {
			conformanceErrCh <- conformanceGRPCServer.Serve(conformanceLis)
		}()
	}
	networkErrCh := make(chan error, 1)
	if networkGRPCServer != nil {
		go func() {
			networkErrCh <- networkGRPCServer.Serve(networkLis)
		}()
	}

	grpcErrCh := make(chan error, 1)
	if nodeGRPCServer != nil {
		go func() {
			grpcErrCh <- nodeGRPCServer.Serve(nodeLis)
		}()
	}

	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- httpServer.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-localErrCh:
		if err != nil {
			runErr = fmt.Errorf("local grpc server exited: %w", err)
		}
	case err := <-conformanceErrCh:
		if err != nil {
			runErr = fmt.Errorf("conformance grpc server exited: %w", err)
		}
	case err := <-networkErrCh:
		if err != nil {
			runErr = fmt.Errorf("Allocation network grpc server exited: %w", err)
		}
	case err := <-grpcErrCh:
		if err != nil {
			runErr = fmt.Errorf("grpc server exited: %w", err)
		}
	case err := <-httpErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("http server exited: %w", err)
		}
	}

	stopHealth()
	for _, endpoint := range healthEndpoints {
		setHealthStatus(endpoint, healthpb.HealthCheckResponse_NOT_SERVING)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.StopTimeout)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logrus.WithError(err).Warn("shutdown HTTP server")
	}

	stopped := make(chan struct{})
	go func() {
		if localGRPCServer != nil {
			localGRPCServer.GracefulStop()
		}
		if conformanceGRPCServer != nil {
			conformanceGRPCServer.GracefulStop()
		}
		if nodeGRPCServer != nil {
			nodeGRPCServer.GracefulStop()
		}
		if networkGRPCServer != nil {
			networkGRPCServer.GracefulStop()
		}
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(config.StopTimeout):
		if localGRPCServer != nil {
			localGRPCServer.Stop()
		}
		if conformanceGRPCServer != nil {
			conformanceGRPCServer.Stop()
		}
		if nodeGRPCServer != nil {
			nodeGRPCServer.Stop()
		}
		if networkGRPCServer != nil {
			networkGRPCServer.Stop()
		}
	}

	if errors.Is(runErr, net.ErrClosed) {
		return nil
	}
	return runErr
}

func loadNodeServerTLS(opts options) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(opts.nodeTLSCert, opts.nodeTLSKey)
	if err != nil {
		return nil, fmt.Errorf("load node tls key pair: %w", err)
	}
	caPEM, err := os.ReadFile(opts.nodeTLSCACert)
	if err != nil {
		return nil, fmt.Errorf("read node tls ca cert: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse node tls ca cert %q", opts.nodeTLSCACert)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

type healthEndpoint struct {
	server   *grpc_health.Server
	services []string
}

func publishHealth(ctx context.Context, svc service.SandboxService, endpoints ...healthEndpoint) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateHealth(svc, endpoints...)
		}
	}
}

func updateHealth(svc service.SandboxService, endpoints ...healthEndpoint) {
	status := healthpb.HealthCheckResponse_NOT_SERVING
	if svc.Ready() {
		status = healthpb.HealthCheckResponse_SERVING
	}
	for _, endpoint := range endpoints {
		setHealthStatus(endpoint, status)
	}
}

func setHealthStatus(endpoint healthEndpoint, status healthpb.HealthCheckResponse_ServingStatus) {
	if endpoint.server == nil {
		return
	}
	endpoint.server.SetServingStatus("", status)
	for _, serviceName := range endpoint.services {
		endpoint.server.SetServingStatus(serviceName, status)
	}
}

func loadSandboxConfig(configPath string) (config.Config, error) {
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return config.Config{}, err
	}
	return config.Decode(configBytes)
}

func listenUnix(socketPath string, mode os.FileMode) (net.Listener, error) {
	cleaned := strings.TrimSpace(socketPath)
	if cleaned == "" {
		return nil, fmt.Errorf("socket path is required")
	}
	if err := os.MkdirAll(filepath.Dir(cleaned), 0755); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(cleaned); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	lis, err := net.Listen("unix", cleaned)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(cleaned, mode); err != nil {
		_ = lis.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return lis, nil
}
