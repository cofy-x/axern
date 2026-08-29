package app

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/cofy-x/axern/runtime/egressd/internal/api"
	"github.com/cofy-x/axern/runtime/egressd/internal/policy"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/grpc"
	grpc_health "google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	DefaultRoot   = "/var/lib/egressd"
	DefaultSocket = "/run/egressd/egressd.sock"
)

type options struct {
	root   string
	socket string
}

func Run(args []string) error {
	opts, err := parseFlags(args)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(opts.root, 0o755); err != nil {
		return fmt.Errorf("create egressd root: %w", err)
	}
	manager, err := policy.NewManager(policy.NewJSONStore(opts.root))
	if err != nil {
		return fmt.Errorf("create egress policy manager: %w", err)
	}
	lis, err := listenUnix(opts.socket)
	if err != nil {
		return err
	}
	defer lis.Close()

	grpcServer := grpc.NewServer()
	runtimeegressv1.RegisterRuntimeEgressServiceServer(grpcServer, api.NewServer(manager))
	healthServer := grpc_health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus("axern.private.runtime.egress.v1.RuntimeEgressService", healthpb.HealthCheckResponse_SERVING)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- grpcServer.Serve(lis) }()
	select {
	case <-ctx.Done():
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		return err
	}
}

func parseFlags(args []string) (options, error) {
	opts := options{}
	flags := flag.NewFlagSet("egressd", flag.ContinueOnError)
	flags.StringVar(&opts.root, "root", DefaultRoot, "egressd persistent state root")
	flags.StringVar(&opts.socket, "socket", DefaultSocket, "egressd gRPC Unix socket")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	opts.root = strings.TrimSpace(opts.root)
	opts.socket = strings.TrimSpace(opts.socket)
	if opts.root == "" {
		return options{}, fmt.Errorf("-root is required")
	}
	if opts.socket == "" {
		return options{}, fmt.Errorf("-socket is required")
	}
	return opts, nil
}

func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	lis, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen unix socket %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = lis.Close()
		return nil, fmt.Errorf("chmod socket %s: %w", path, err)
	}
	return lis, nil
}
