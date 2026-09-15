package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/tunneld/internal/control"
	tunneldobs "github.com/cofy-x/axern/runtime/tunneld/internal/observability"
	"github.com/cofy-x/axern/runtime/tunneld/internal/relay"
	"github.com/cofy-x/axern/runtime/tunneld/internal/relaytls"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tunneld: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		listen          string
		controlTarget   string
		caCert          string
		cert            string
		cluster         string
		relayCert       string
		relayKey        string
		relayID         string
		drain           bool
		maxSessions     int
		sendQueueSize   int
		maxFrameBytes   int
		revalidate      time.Duration
		pairWaitTimeout time.Duration
		pingInterval    time.Duration
		pongTimeout     time.Duration
	)
	flag.StringVar(&listen, "listen", "127.0.0.1:24100", "Tunnel relay gRPC listen address")
	flag.StringVar(&controlTarget, "control-target", "127.0.0.1:24000", "controld gRPC target")
	flag.StringVar(&caCert, "tls-ca-cert", ".dev/certs/ca.crt", "controld CA certificate")
	flag.StringVar(&cert, "workload-bundle", ".dev/certs/tunneld.pem", "tunneld workload certificate and key PEM bundle")
	flag.StringVar(&cluster, "workload-cluster", "axern.local", "workload URI trust domain")
	flag.StringVar(&relayCert, "relay-tls-cert", ".dev/certs/tunneld.pem", "tunnel relay server certificate")
	flag.StringVar(&relayKey, "relay-tls-key", ".dev/certs/tunneld.pem", "tunnel relay server key")
	flag.StringVar(&relayID, "relay-id", "default", "stable tunnel relay id used in controld relay registry")
	flag.BoolVar(&drain, "drain", false, "reject new tunnel peers while preserving process health for existing peers")
	flag.IntVar(&maxSessions, "max-sessions", 10000, "maximum active tunnel relay sessions")
	flag.IntVar(&sendQueueSize, "send-queue-size", 128, "per-peer relay frame send queue capacity")
	flag.IntVar(&maxFrameBytes, "max-frame-bytes", 1024*1024, "maximum stream data frame payload size")
	flag.DurationVar(&revalidate, "peer-revalidate-interval", 15*time.Second, "interval for revalidating connected tunnel peers; 0 disables")
	flag.DurationVar(&pairWaitTimeout, "pair-wait-timeout", 30*time.Second, "maximum time a peer waits for its opposite peer")
	flag.DurationVar(&pingInterval, "ping-interval", 15*time.Second, "relay protocol ping interval; 0 disables")
	flag.DurationVar(&pongTimeout, "pong-timeout", 45*time.Second, "maximum time since last peer frame before closing")
	flag.Parse()
	if maxSessions < 0 {
		return fmt.Errorf("-max-sessions must be >= 0")
	}
	if revalidate < 0 {
		return fmt.Errorf("-peer-revalidate-interval must be >= 0")
	}
	if relayID == "" {
		return fmt.Errorf("-relay-id is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	obs, err := sdkobs.Init(context.Background(), sdkobs.ConfigFromEnv(
		sdkobs.WithServiceName("tunneld"),
		sdkobs.WithComponent("tunneld"),
	))
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = obs.Shutdown(shutdownCtx)
	}()
	conn, err := control.Dial(ctx, controlTarget, caCert, cert, cluster)
	if err != nil {
		return err
	}
	defer conn.Close()
	lis, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer lis.Close()
	serverOpts, err := relaytls.ServerOptions(relaytls.ServerConfig{Cert: relayCert, Key: relayKey})
	if err != nil {
		return err
	}
	if handler := obs.GRPCServerStatsHandler(); handler != nil {
		serverOpts = append(serverOpts, grpc.StatsHandler(handler))
	}
	server := grpc.NewServer(serverOpts...)
	relayServer := relay.New(
		relay.RelayControlAdapter{
			Client: control.NewRelayControlClient(conn),
		},
		relay.WithRelayID(relayID),
		relay.WithDrain(drain),
		relay.WithMaxSessions(maxSessions),
		relay.WithSendQueueSize(sendQueueSize),
		relay.WithMaxFrameBytes(maxFrameBytes),
		relay.WithPeerRevalidateInterval(revalidate),
		relay.WithPairWaitTimeout(pairWaitTimeout),
		relay.WithPingInterval(pingInterval),
		relay.WithPongTimeout(pongTimeout),
	)
	if _, err := obs.RegisterInt64ObservableGauge(tunneldobs.MetricRelayActiveSessions.Name, tunneldobs.MetricRelayActiveSessions.Description, relayServer.ObserveActiveSessions); err != nil {
		return err
	}
	if _, err := obs.RegisterInt64ObservableGauge(tunneldobs.MetricRelayActivePeers.Name, tunneldobs.MetricRelayActivePeers.Description, relayServer.ObserveActivePeers); err != nil {
		return err
	}
	tunnelv1.RegisterTunnelRelayServer(server, relayServer)
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(lis) }()
	select {
	case <-ctx.Done():
		server.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}
