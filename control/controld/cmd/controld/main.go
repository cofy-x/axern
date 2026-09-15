package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/api/authz"
	"github.com/cofy-x/axern/control/controld/internal/api/rpcstatus"
	"github.com/cofy-x/axern/control/controld/internal/app"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	controldobs "github.com/cofy-x/axern/control/controld/internal/observability"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	privateadminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/admin/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const (
	defaultGRPCAddress              = "127.0.0.1:24000"
	defaultHTTPAddress              = "127.0.0.1:24001"
	defaultHeartbeatFreshnessWindow = 15 * time.Second
	defaultSummaryFreshnessWindow   = 15 * time.Second
	defaultTLSCACert                = ".dev/certs/ca.crt"
	defaultTunnelRelays             = "default,127.0.0.1:25000,127.0.0.1:24100,1,false"
)

type options struct {
	grpcAddress                string
	httpAddress                string
	logLevel                   string
	heartbeatFreshnessWindow   time.Duration
	summaryFreshnessWindow     time.Duration
	postgresDSN                string
	postgresMaxConnections     int
	secretsMasterKey           string
	reconcileTimeout           time.Duration
	resourceCPUOvercommitRatio float64
	tlsCACert                  string
	tunnelRelays               string
	enrollmentAddress          string
	workloadCluster            string
	workloadBundle             string
	workloadSignerBundle       string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "controld: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseFlags()
	if err != nil {
		return err
	}
	if err := configureLogging(opts.logLevel); err != nil {
		return err
	}
	obs, err := sdkobs.Init(context.Background(), sdkobs.ConfigFromEnv(
		sdkobs.WithServiceName("controld"),
		sdkobs.WithComponent("controld"),
	))
	if err != nil {
		return err
	}
	if obs.Enabled() {
		logrus.AddHook(logrusotel.New("controld"))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := obs.Shutdown(shutdownCtx); err != nil {
			logrus.WithError(err).Warn("shutdown OpenTelemetry")
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc, err := app.New(app.Config{
		LifecycleContext:         ctx,
		HeartbeatFreshnessWindow: opts.heartbeatFreshnessWindow,
		SummaryFreshnessWindow:   opts.summaryFreshnessWindow,
		PostgresDSN:              opts.postgresDSN,
		PostgresMaxConnections:   int32(opts.postgresMaxConnections),
		SecretsMasterKey:         opts.secretsMasterKey,
		ReconcileTimeout:         opts.reconcileTimeout,
		TunnelRelays:             opts.tunnelRelays,
		ResourcePolicy: resourcekernel.AdmissionPolicy{
			CPUOvercommitRatio: opts.resourceCPUOvercommitRatio,
		},
		NodeTransportCredentials: func(nodeID string) credentials.TransportCredentials {
			return &workloadtls.Credentials{BundlePath: opts.workloadBundle, TrustPath: opts.tlsCACert, Local: workloadtls.Identity{Cluster: opts.workloadCluster, Role: "controld"}, Peer: workloadtls.Identity{Cluster: opts.workloadCluster, Role: "axnoded", NodeID: nodeID}}
		},
	})
	if err != nil {
		return err
	}
	defer svc.Close()
	enrollmentHandler, err := svc.NodeEnrollmentHandler(workloadtls.FileIssuer{BundlePath: opts.workloadSignerBundle, Cluster: opts.workloadCluster})
	if err != nil {
		return fmt.Errorf("configure node enrollment: %w", err)
	}
	enrollmentServer := grpc.NewServer(
		grpc.Creds(&workloadtls.EnrollmentCredentials{Workload: &workloadtls.Credentials{
			BundlePath: opts.workloadBundle, TrustPath: opts.tlsCACert,
			Local: workloadtls.Identity{Cluster: opts.workloadCluster, Role: "controld"},
		}}),
		grpc.MaxRecvMsgSize(32<<10),
		grpc.MaxConcurrentStreams(16),
		grpc.ConnectionTimeout(10*time.Second),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionAge: 5 * time.Minute, MaxConnectionAgeGrace: 10 * time.Second}),
		grpc.ChainUnaryInterceptor(enrollmentDeadline, rpcstatus.UnaryServerInterceptor(postgres.IsDependencyUnavailable)),
	)
	defer enrollmentServer.Stop()
	nodev1.RegisterNodeEnrollmentServer(enrollmentServer, enrollmentHandler)
	enrollmentLis, err := net.Listen("tcp", opts.enrollmentAddress)
	if err != nil {
		return fmt.Errorf("listen node enrollment: %w", err)
	}
	defer enrollmentLis.Close()
	hasAdmin, err := svc.HasActivePlatformAdmin(ctx)
	if err != nil {
		return fmt.Errorf("check access bootstrap: %w", err)
	}
	if !hasAdmin {
		return errors.New("access bootstrap is incomplete: no active platform administrator")
	}
	authorization := authz.New(svc.AccessControl(), opts.workloadCluster, svc.RequireActiveNode)
	grpcOptions := []grpc.ServerOption{
		grpc.Creds(&workloadtls.Credentials{BundlePath: opts.workloadBundle, TrustPath: opts.tlsCACert, Local: workloadtls.Identity{Cluster: opts.workloadCluster, Role: "controld"}}),
		grpc.KeepaliveParams(keepalive.ServerParameters{MaxConnectionAge: 5 * time.Minute, MaxConnectionAgeGrace: 10 * time.Second}),
		grpc.ChainUnaryInterceptor(authorization.Unary, rpcstatus.UnaryServerInterceptor(postgres.IsDependencyUnavailable)),
		grpc.ChainStreamInterceptor(authorization.Stream, rpcstatus.StreamServerInterceptor(postgres.IsDependencyUnavailable)),
	}
	if handler := obs.GRPCServerStatsHandler(); handler != nil {
		grpcOptions = append(grpcOptions, grpc.StatsHandler(handler))
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	privateadminv1.RegisterAllocationLifecycleAdminServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterAdminAuditServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterAdminReliabilityServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterNodeAdminServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterAccessAdminServer(grpcServer, svc.AdminV1Handler())
	identityv1.RegisterIdentityControlServer(grpcServer, svc.IdentityV1Handler())
	environmentv1.RegisterEnvironmentControlServer(grpcServer, svc.PublicV1Handler())
	runv1.RegisterRunControlServer(grpcServer, svc.PublicV1Handler())
	secretv1.RegisterSecretControlServer(grpcServer, svc.PublicV1Handler())
	tunnelcontrolv1.RegisterTunnelControlServer(grpcServer, svc.PublicV1Handler())
	namespacev1.RegisterNamespaceControlServer(grpcServer, svc.PublicV1Handler())
	quotav1.RegisterQuotaControlServer(grpcServer, svc.PublicV1Handler())
	tunnelrelaycontrolv1.RegisterTunnelRelayControlServer(grpcServer, svc.RelayV1Handler())
	if svc.GatewayV1Handler() != nil {
		gatewayv1.RegisterGatewayControlServer(grpcServer, svc.GatewayV1Handler())
	}
	nodev1.RegisterNodeControlServer(grpcServer, svc.NodeV1Handler())

	grpcLis, err := net.Listen("tcp", opts.grpcAddress)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", opts.grpcAddress, err)
	}
	defer grpcLis.Close()

	httpServer := &http.Server{
		Addr:              opts.httpAddress,
		Handler:           obs.HTTPHandler(svc.HTTPHandler(), controldobs.SpanHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}

	grpcErrCh := make(chan error, 1)
	go func() {
		grpcErrCh <- grpcServer.Serve(grpcLis)
	}()
	enrollmentErrCh := make(chan error, 1)
	go func() { enrollmentErrCh <- enrollmentServer.Serve(enrollmentLis) }()

	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- httpServer.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-enrollmentErrCh:
		if err != nil {
			runErr = fmt.Errorf("node enrollment server exited: %w", err)
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logrus.WithError(err).Warn("shutdown controld HTTP server")
	}

	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		grpcServer.Stop()
	}

	if errors.Is(runErr, net.ErrClosed) {
		return nil
	}
	return runErr
}

func parseFlags() (options, error) {
	opts := options{}
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flagSet.StringVar(&opts.grpcAddress, "grpc-address", defaultGRPCAddress, "controld gRPC listen address")
	flagSet.StringVar(&opts.enrollmentAddress, "enrollment-address", "127.0.0.1:24002", "TLS-only Node enrollment and renewal listen address")
	flagSet.StringVar(&opts.workloadCluster, "workload-cluster", os.Getenv("AXERN_WORKLOAD_CLUSTER"), "workload URI trust domain")
	flagSet.StringVar(&opts.workloadBundle, "workload-bundle", os.Getenv("CONTROLD_WORKLOAD_BUNDLE"), "atomic controld workload certificate and key PEM bundle")
	flagSet.StringVar(&opts.workloadSignerBundle, "workload-signer-bundle", os.Getenv("CONTROLD_WORKLOAD_SIGNER_BUNDLE"), "control-only workload signing CA certificate and key PEM bundle")
	flagSet.StringVar(&opts.httpAddress, "http-address", defaultHTTPAddress, "controld HTTP listen address for diagnostics and internal runtime artifacts")
	flagSet.StringVar(&opts.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	flagSet.DurationVar(&opts.heartbeatFreshnessWindow, "heartbeat-freshness-window", defaultHeartbeatFreshnessWindow, "heartbeat freshness window")
	flagSet.DurationVar(&opts.summaryFreshnessWindow, "summary-freshness-window", defaultSummaryFreshnessWindow, "summary freshness window")
	flagSet.StringVar(&opts.postgresDSN, "postgres-dsn", os.Getenv("CONTROLD_POSTGRES_DSN"), "Postgres DSN for the authoritative control-plane state store")
	flagSet.IntVar(&opts.postgresMaxConnections, "postgres-max-connections", 0, "controld Postgres connection pool ceiling; 0 uses the application default")
	flagSet.StringVar(&opts.secretsMasterKey, "secrets-master-key", os.Getenv("AXERN_SECRETS_MASTER_KEY"), "32-byte raw or base64-encoded master key for encrypted secret storage")
	flagSet.DurationVar(&opts.reconcileTimeout, "reconcile-timeout", 0, "timeout for one background reconcile operation; 0 uses the application default")
	flagSet.Float64Var(&opts.resourceCPUOvercommitRatio, "resource-cpu-overcommit-ratio", resourcekernel.DefaultCPUOvercommitRatio, "CPU overcommit ratio for request resource admission")
	flagSet.StringVar(&opts.tlsCACert, "tls-ca-cert", defaultString(os.Getenv("CONTROLD_TLS_CA_CERT"), defaultTLSCACert), "CA certificate used to verify mTLS clients")
	flagSet.StringVar(&opts.tunnelRelays, "tunnel-relays", defaultString(os.Getenv("CONTROLD_TUNNEL_RELAYS"), defaultTunnelRelays), "semicolon-separated tunnel relay registry entries: id,client_target,node_target,weight,drain")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.postgresDSN) == "" {
		return options{}, fmt.Errorf("postgres-dsn is required")
	}
	if strings.TrimSpace(opts.enrollmentAddress) == "" || strings.TrimSpace(opts.workloadBundle) == "" || strings.TrimSpace(opts.workloadSignerBundle) == "" {
		return options{}, fmt.Errorf("enrollment-address, workload-bundle and workload-signer-bundle are required")
	}
	if _, err := (workloadtls.Identity{Cluster: opts.workloadCluster, Role: "controld"}).URI(); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.secretsMasterKey) == "" {
		return options{}, fmt.Errorf("secrets-master-key is required")
	}
	if opts.resourceCPUOvercommitRatio <= 0 || math.IsNaN(opts.resourceCPUOvercommitRatio) || math.IsInf(opts.resourceCPUOvercommitRatio, 0) {
		return options{}, fmt.Errorf("resource-cpu-overcommit-ratio must be > 0")
	}
	if opts.postgresMaxConnections < 0 {
		return options{}, fmt.Errorf("postgres-max-connections must be >= 0")
	}
	if opts.reconcileTimeout < 0 {
		return options{}, fmt.Errorf("reconcile-timeout must be >= 0")
	}
	if strings.TrimSpace(opts.tlsCACert) == "" {
		return options{}, fmt.Errorf("tls-ca-cert is required")
	}
	return opts, nil
}

func enrollmentDeadline(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return next(ctx, req)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a valid duration: %w", name, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be > 0", name)
	}
	return parsed, nil
}

func configureLogging(levelName string) error {
	level, err := logrus.ParseLevel(strings.ToLower(levelName))
	if err != nil {
		return fmt.Errorf("parse log level %q: %w", levelName, err)
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	return nil
}
