package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/lib/go/observability/logrusotel"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	identityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/identity/v1"
	namespacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/namespace/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelrelaycontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/tunnel/v1"
	artifactaccessv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/artifact/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	defaultGRPCAddress              = "127.0.0.1:24000"
	defaultHTTPAddress              = "127.0.0.1:24001"
	defaultHeartbeatFreshnessWindow = 15 * time.Second
	defaultSummaryFreshnessWindow   = 15 * time.Second
	defaultTLSCACert                = ".dev/certs/ca.crt"
	defaultTLSCert                  = ".dev/certs/controld.crt"
	defaultTLSKey                   = ".dev/certs/controld.key"
	defaultTunnelRelays             = "default,127.0.0.1:25000,127.0.0.1:24100,1,false"
	defaultStoragedTarget           = "127.0.0.1:24020"
	defaultFunctionGatewayTimeout   = 30 * time.Second
)

type options struct {
	grpcAddress                                            string
	httpAddress                                            string
	logLevel                                               string
	heartbeatFreshnessWindow                               time.Duration
	summaryFreshnessWindow                                 time.Duration
	postgresDSN                                            string
	postgresMaxConnections                                 int
	secretsMasterKey                                       string
	reconcileTimeout                                       time.Duration
	resourceCPUOvercommitRatio                             float64
	serviceReconcileWorkers                                int
	serviceAllocationGlobalWorkers                         int
	serviceAllocationWorkersPerNode                        int
	tlsCACert                                              string
	tlsCert                                                string
	tlsKey                                                 string
	tunnelRelays                                           string
	storagedTarget                                         string
	functionGatewayURL                                     string
	functionGatewayToken                                   string
	functionGatewayTimeout                                 time.Duration
	functionInvocationWorkers                              int
	volumeReclaimWorkers                                   int
	volumeReclaimWorkersPerNode                            int
	functionBundleBaseURL                                  string
	functionBundleToken                                    string
	rolloutWorkerToken                                     string
	artifactS3Endpoint, artifactS3Region, artifactS3Bucket string
	artifactS3AccessKey, artifactS3SecretKey               string
	artifactS3UsePathStyle                                 bool
	artifactTicketSigningKey                               string
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
		LifecycleContext:            ctx,
		HeartbeatFreshnessWindow:    opts.heartbeatFreshnessWindow,
		SummaryFreshnessWindow:      opts.summaryFreshnessWindow,
		PostgresDSN:                 opts.postgresDSN,
		PostgresMaxConnections:      int32(opts.postgresMaxConnections),
		SecretsMasterKey:            opts.secretsMasterKey,
		ReconcileTimeout:            opts.reconcileTimeout,
		TunnelRelays:                opts.tunnelRelays,
		StoragedTarget:              opts.storagedTarget,
		FunctionGatewayURL:          opts.functionGatewayURL,
		FunctionGatewayToken:        opts.functionGatewayToken,
		FunctionGatewayTimeout:      opts.functionGatewayTimeout,
		FunctionInvocationWorkers:   opts.functionInvocationWorkers,
		VolumeReclaimWorkers:        opts.volumeReclaimWorkers,
		VolumeReclaimWorkersPerNode: opts.volumeReclaimWorkersPerNode,
		FunctionBundleBaseURL:       opts.functionBundleBaseURL,
		FunctionBundleToken:         opts.functionBundleToken,
		RolloutWorkerToken:          opts.rolloutWorkerToken,
		ArtifactS3Endpoint:          opts.artifactS3Endpoint, ArtifactS3Region: opts.artifactS3Region, ArtifactS3Bucket: opts.artifactS3Bucket, ArtifactS3AccessKey: opts.artifactS3AccessKey, ArtifactS3SecretKey: opts.artifactS3SecretKey, ArtifactS3UsePathStyle: opts.artifactS3UsePathStyle,
		ArtifactTicketSigningKey: opts.artifactTicketSigningKey,
		ResourcePolicy: resourcekernel.AdmissionPolicy{
			CPUOvercommitRatio: opts.resourceCPUOvercommitRatio,
		},
		ServiceReconcileWorkers:         opts.serviceReconcileWorkers,
		ServiceAllocationGlobalWorkers:  opts.serviceAllocationGlobalWorkers,
		ServiceAllocationWorkersPerNode: opts.serviceAllocationWorkersPerNode,
	})
	if err != nil {
		return err
	}
	defer svc.Close()
	hasAdmin, err := svc.HasActivePlatformAdmin(ctx)
	if err != nil {
		return fmt.Errorf("check access bootstrap: %w", err)
	}
	if !hasAdmin {
		return errors.New("access bootstrap is incomplete: no active platform administrator")
	}
	tlsConfig, err := loadServerTLS(opts)
	if err != nil {
		return err
	}
	authorization := authz.New(svc.AccessControl())
	grpcOptions := []grpc.ServerOption{
		grpc.Creds(credentials.NewTLS(tlsConfig)),
		grpc.ChainUnaryInterceptor(authorization.Unary, rpcstatus.UnaryServerInterceptor(postgres.IsDependencyUnavailable)),
		grpc.ChainStreamInterceptor(authorization.Stream, rpcstatus.StreamServerInterceptor(postgres.IsDependencyUnavailable)),
	}
	if handler := obs.GRPCServerStatsHandler(); handler != nil {
		grpcOptions = append(grpcOptions, grpc.StatsHandler(handler))
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	adminv1.RegisterAllocationLifecycleAdminServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterAdminAuditServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterAdminReliabilityServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterNodeAdminServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterStorageAdminServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterServiceAdminServer(grpcServer, svc.AdminV1Handler())
	adminv1.RegisterAccessAdminServer(grpcServer, svc.AdminV1Handler())
	identityv1.RegisterIdentityControlServer(grpcServer, svc.IdentityV1Handler())
	catalogv1.RegisterRuntimeCatalogServer(grpcServer, svc.PublicV1Handler())
	environmentv1.RegisterEnvironmentControlServer(grpcServer, svc.PublicV1Handler())
	runv1.RegisterRunControlServer(grpcServer, svc.PublicV1Handler())
	secretv1.RegisterSecretControlServer(grpcServer, svc.PublicV1Handler())
	servicev1.RegisterServiceControlServer(grpcServer, svc.PublicV1Handler())
	functionv1.RegisterFunctionControlServer(grpcServer, svc.PublicV1Handler())
	tunnelcontrolv1.RegisterTunnelControlServer(grpcServer, svc.PublicV1Handler())
	namespacev1.RegisterNamespaceControlServer(grpcServer, svc.PublicV1Handler())
	quotav1.RegisterQuotaControlServer(grpcServer, svc.PublicV1Handler())
	agentprofilev1.RegisterAgentProfileControlServer(grpcServer, svc.PublicV1Handler())
	rolloutv1.RegisterRolloutControlServer(grpcServer, svc.PublicV1Handler())
	if svc.RolloutWorkerV1Handler() != nil {
		workerrolloutv1.RegisterRolloutWorkerControlServer(grpcServer, svc.RolloutWorkerV1Handler())
	}
	if svc.ArtifactAccessV1Handler() != nil {
		artifactaccessv1.RegisterArtifactAccessServer(grpcServer, svc.ArtifactAccessV1Handler())
	}
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

	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- httpServer.ListenAndServe()
	}()

	var runErr error
	select {
	case <-ctx.Done():
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
	functionGatewayTimeout, err := durationFromEnv("CONTROLD_FUNCTION_GATEWAY_TIMEOUT", defaultFunctionGatewayTimeout)
	if err != nil {
		return options{}, err
	}
	flagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flagSet.StringVar(&opts.grpcAddress, "grpc-address", defaultGRPCAddress, "controld gRPC listen address")
	flagSet.StringVar(&opts.httpAddress, "http-address", defaultHTTPAddress, "controld HTTP listen address for diagnostics and internal runtime artifacts")
	flagSet.StringVar(&opts.logLevel, "log-level", "info", "log level: debug|info|warn|error")
	flagSet.DurationVar(&opts.heartbeatFreshnessWindow, "heartbeat-freshness-window", defaultHeartbeatFreshnessWindow, "heartbeat freshness window")
	flagSet.DurationVar(&opts.summaryFreshnessWindow, "summary-freshness-window", defaultSummaryFreshnessWindow, "summary freshness window")
	flagSet.StringVar(&opts.postgresDSN, "postgres-dsn", os.Getenv("CONTROLD_POSTGRES_DSN"), "Postgres DSN for the authoritative control-plane state store")
	flagSet.IntVar(&opts.postgresMaxConnections, "postgres-max-connections", 0, "controld Postgres connection pool ceiling; 0 uses the application default")
	flagSet.StringVar(&opts.secretsMasterKey, "secrets-master-key", os.Getenv("AXERN_SECRETS_MASTER_KEY"), "32-byte raw or base64-encoded master key for encrypted secret storage")
	flagSet.DurationVar(&opts.reconcileTimeout, "reconcile-timeout", 0, "timeout for one background reconcile operation; 0 uses the application default")
	flagSet.Float64Var(&opts.resourceCPUOvercommitRatio, "resource-cpu-overcommit-ratio", resourcekernel.DefaultCPUOvercommitRatio, "CPU overcommit ratio for request reservation admission")
	flagSet.IntVar(&opts.serviceReconcileWorkers, "service-reconcile-workers", 0, "global service reconcile workers; 0 uses the application default")
	flagSet.IntVar(&opts.serviceAllocationGlobalWorkers, "service-allocation-global-workers", 0, "global concurrent service allocation creates; 0 uses the application default")
	flagSet.IntVar(&opts.serviceAllocationWorkersPerNode, "service-allocation-workers-per-node", 0, "concurrent service allocation creates per node; 0 uses the application default")
	flagSet.StringVar(&opts.tlsCACert, "tls-ca-cert", defaultString(os.Getenv("CONTROLD_TLS_CA_CERT"), defaultTLSCACert), "CA certificate used to verify mTLS clients")
	flagSet.StringVar(&opts.tlsCert, "tls-cert", defaultString(os.Getenv("CONTROLD_TLS_CERT"), defaultTLSCert), "controld server certificate")
	flagSet.StringVar(&opts.tlsKey, "tls-key", defaultString(os.Getenv("CONTROLD_TLS_KEY"), defaultTLSKey), "controld server private key")
	flagSet.StringVar(&opts.tunnelRelays, "tunnel-relays", defaultString(os.Getenv("CONTROLD_TUNNEL_RELAYS"), defaultTunnelRelays), "semicolon-separated tunnel relay registry entries: id,client_target,node_target,weight,drain")
	flagSet.StringVar(&opts.storagedTarget, "storaged-target", defaultString(os.Getenv("CONTROLD_STORAGED_TARGET"), defaultStoragedTarget), "storaged gRPC target for service volume coordination")
	flagSet.StringVar(&opts.functionGatewayURL, "function-gateway-url", os.Getenv("CONTROLD_FUNCTION_GATEWAY_URL"), "gatewayd base HTTP URL used for Function worker dispatch")
	flagSet.StringVar(&opts.functionGatewayToken, "function-gateway-token", os.Getenv("CONTROLD_FUNCTION_GATEWAY_TOKEN"), "bearer token sent to gatewayd Function dispatch when configured")
	flagSet.DurationVar(&opts.functionGatewayTimeout, "function-gateway-timeout", functionGatewayTimeout, "timeout for Function worker dispatch through gatewayd")
	flagSet.IntVar(&opts.functionInvocationWorkers, "function-invocation-workers", 0, "global asynchronous Function invocation workers; 0 uses the application default")
	flagSet.IntVar(&opts.volumeReclaimWorkers, "volume-reclaim-workers", 0, "global durable volume reclaim workers; 0 uses the application default")
	flagSet.IntVar(&opts.volumeReclaimWorkersPerNode, "volume-reclaim-workers-per-node", 0, "per-node durable volume reclaim workers; 0 uses the application default")
	flagSet.StringVar(&opts.functionBundleBaseURL, "function-bundle-base-url", os.Getenv("CONTROLD_FUNCTION_BUNDLE_BASE_URL"), "base HTTP URL advertised to Function workers for uploaded bundle downloads")
	flagSet.StringVar(&opts.functionBundleToken, "function-bundle-token", os.Getenv("CONTROLD_FUNCTION_BUNDLE_TOKEN"), "bearer token required for Function bundle downloads when configured")
	flagSet.StringVar(&opts.rolloutWorkerToken, "rollout-worker-token", os.Getenv("CONTROLD_ROLLOUT_WORKER_TOKEN"), "bootstrap credential for durable rollout workers; empty disables the worker API")
	flagSet.StringVar(&opts.artifactS3Endpoint, "artifact-s3-endpoint", os.Getenv("CONTROLD_ARTIFACT_S3_ENDPOINT"), "S3-compatible endpoint for rollout artifacts")
	flagSet.StringVar(&opts.artifactS3Region, "artifact-s3-region", os.Getenv("CONTROLD_ARTIFACT_S3_REGION"), "S3 region for rollout artifacts")
	flagSet.StringVar(&opts.artifactS3Bucket, "artifact-s3-bucket", os.Getenv("CONTROLD_ARTIFACT_S3_BUCKET"), "S3 bucket for rollout artifacts; empty disables artifact upload")
	flagSet.StringVar(&opts.artifactS3AccessKey, "artifact-s3-access-key", os.Getenv("CONTROLD_ARTIFACT_S3_ACCESS_KEY"), "S3 access key for rollout artifacts")
	flagSet.StringVar(&opts.artifactS3SecretKey, "artifact-s3-secret-key", os.Getenv("CONTROLD_ARTIFACT_S3_SECRET_KEY"), "S3 secret key for rollout artifacts")
	flagSet.BoolVar(&opts.artifactS3UsePathStyle, "artifact-s3-use-path-style", strings.EqualFold(os.Getenv("CONTROLD_ARTIFACT_S3_USE_PATH_STYLE"), "true"), "use path-style S3 addressing")
	flagSet.StringVar(&opts.artifactTicketSigningKey, "artifact-ticket-signing-key", os.Getenv("CONTROLD_ARTIFACT_TICKET_KEY"), "persistent HMAC key for gateway artifact download tickets")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.postgresDSN) == "" {
		return options{}, fmt.Errorf("postgres-dsn is required")
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
	if opts.serviceReconcileWorkers < 0 || opts.serviceAllocationGlobalWorkers < 0 || opts.serviceAllocationWorkersPerNode < 0 {
		return options{}, fmt.Errorf("service worker counts must be >= 0")
	}
	if strings.TrimSpace(opts.tlsCACert) == "" || strings.TrimSpace(opts.tlsCert) == "" || strings.TrimSpace(opts.tlsKey) == "" {
		return options{}, fmt.Errorf("tls-ca-cert, tls-cert, and tls-key are required")
	}
	return opts, nil
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

func loadServerTLS(opts options) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(opts.tlsCert, opts.tlsKey)
	if err != nil {
		return nil, fmt.Errorf("load tls key pair: %w", err)
	}
	caPEM, err := os.ReadFile(opts.tlsCACert)
	if err != nil {
		return nil, fmt.Errorf("read tls ca cert: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse tls ca cert %q", opts.tlsCACert)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    roots,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
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
