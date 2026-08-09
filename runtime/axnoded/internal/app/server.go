package app

import (
	"bytes"
	"context"
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
	nodeoperatorv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/operator/v1"
	"github.com/pelletier/go-toml"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
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
	hostname, _ := os.Hostname()
	nodeID := cfg.PluginConfig.ControlPlaneNodeIDValue(hostname)
	controlPlaneConfig := cfg.PluginConfig
	allocationTargets := api.NewAllocationTargetRegistry()
	leaseCache := controlplane.NewLeaseCache()
	var leaseValidator api.DirectLeaseValidator
	leaseWatcher := controlplane.NewLeaseWatcher(
		controlplane.WithLeaseWatcherTarget(controlPlaneConfig.ControlPlaneTarget),
		controlplane.WithLeaseWatcherNode(nodeID, controlPlaneConfig.ControlPlaneNodeAuthTokenValue()),
		controlplane.WithLeaseWatcherTLS(
			controlPlaneConfig.ControlPlaneTLSCACert,
			controlPlaneConfig.ControlPlaneTLSCert,
			controlPlaneConfig.ControlPlaneTLSKey,
		),
		controlplane.WithLeaseWatcherCache(leaseCache),
	)
	if leaseWatcher != nil {
		leaseValidator = leaseCache
		leaseWatcher.Start()
		defer leaseWatcher.Stop()
	}

	if strings.TrimSpace(opts.socketPath) != "" {
		localLis, err = listenUnix(opts.socketPath)
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
		nodesandboxv1.RegisterNodeSandboxServer(localGRPCServer, api.NewNodeSandboxServer(svc, nodeID, allocationTargets, leaseValidator))
		nodelifecyclev1.RegisterNodeLifecycleServer(localGRPCServer, api.NewNodeLifecycleServer(svc, nodeID, allocationTargets))
		nodeoperatorv1.RegisterNodeOperatorServer(localGRPCServer, api.NewNodeOperatorServer(svc, allocationTargets))
		healthpb.RegisterHealthServer(localGRPCServer, localHealthServer)
	}

	if strings.TrimSpace(opts.grpcAddress) != "" {
		nodeLis, err = net.Listen("tcp", opts.grpcAddress)
		if err != nil {
			return fmt.Errorf("listen node grpc %s: %w", opts.grpcAddress, err)
		}
		defer nodeLis.Close()

		nodeHealthServer = grpc_health.NewServer()
		nodeOptions := []grpc.ServerOption{grpc.UnaryInterceptor(trace.InjectTraceInterceptor)}
		if handler := obs.GRPCServerStatsHandler(); handler != nil {
			nodeOptions = append(nodeOptions, grpc.StatsHandler(handler))
		}
		nodeGRPCServer = grpc.NewServer(nodeOptions...)
		nodesandboxv1.RegisterNodeSandboxServer(nodeGRPCServer, api.NewNodeSandboxServer(svc, nodeID, allocationTargets, leaseValidator))
		nodelifecyclev1.RegisterNodeLifecycleServer(nodeGRPCServer, api.NewNodeLifecycleServer(svc, nodeID, allocationTargets))
		healthpb.RegisterHealthServer(nodeGRPCServer, nodeHealthServer)
	}

	healthCtx, stopHealth := context.WithCancel(context.Background())
	defer stopHealth()
	go publishHealth(healthCtx, svc, localHealthServer, nodeHealthServer)

	natBackend := strings.TrimSpace(strings.ToLower(os.Getenv("NAT_BACKEND")))
	if natBackend == "" {
		natBackend = "iptables"
	}
	dashboard := api.NewNginxDashboard(svc, natBackend)
	httpServer := &http.Server{
		Addr:              opts.httpAddress,
		Handler:           obs.HTTPHandler(api.NewHTTPMux(svc, dashboard), sandboxobs.SpanHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}

	localErrCh := make(chan error, 1)
	if localGRPCServer != nil {
		go func() {
			localErrCh <- localGRPCServer.Serve(localLis)
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
	if nodeHealthServer != nil {
		nodeHealthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		nodeHealthServer.SetServingStatus(config.SandboxServiceName, healthpb.HealthCheckResponse_NOT_SERVING)
	}
	if localHealthServer != nil {
		localHealthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		localHealthServer.SetServingStatus(config.SandboxServiceName, healthpb.HealthCheckResponse_NOT_SERVING)
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
		if nodeGRPCServer != nil {
			nodeGRPCServer.GracefulStop()
		}
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(config.StopTimeout):
		if localGRPCServer != nil {
			localGRPCServer.Stop()
		}
		if nodeGRPCServer != nil {
			nodeGRPCServer.Stop()
		}
	}

	if errors.Is(runErr, net.ErrClosed) {
		return nil
	}
	return runErr
}

func publishHealth(ctx context.Context, svc service.SandboxService, healthServers ...*grpc_health.Server) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	update := func() {
		status := healthpb.HealthCheckResponse_NOT_SERVING
		if svc.Ready() {
			status = healthpb.HealthCheckResponse_SERVING
		}
		for _, healthServer := range healthServers {
			if healthServer == nil {
				continue
			}
			healthServer.SetServingStatus("", status)
			healthServer.SetServingStatus(config.SandboxServiceName, status)
		}
	}

	update()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			update()
		}
	}
}

func loadSandboxConfig(configPath string) (config.Config, error) {
	cfg := config.DefaultConfig()
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		return config.Config{}, err
	}
	if err := toml.NewDecoder(bytes.NewReader(configBytes)).Decode(&cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func listenUnix(socketPath string) (net.Listener, error) {
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
	if err := os.Chmod(cleaned, 0666); err != nil {
		_ = lis.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return lis, nil
}
