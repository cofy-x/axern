package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cofy-x/axern/control/storaged/internal/app"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc"
)

const (
	defaultGRPCAddress = "127.0.0.1:24020"
	defaultHTTPAddress = "127.0.0.1:24021"
)

type options struct {
	grpcAddress string
	httpAddress string
	postgresDSN string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "storaged: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseFlags()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	svc, err := app.New(ctx, app.Config{PostgresDSN: opts.postgresDSN})
	if err != nil {
		return err
	}
	defer svc.Close()

	grpcServer := grpc.NewServer()
	storagev1.RegisterStorageControlServer(grpcServer, svc.Handler())
	privatestoragev1.RegisterStorageCoordinatorServer(grpcServer, svc.Handler())

	grpcLis, err := net.Listen("tcp", opts.grpcAddress)
	if err != nil {
		return fmt.Errorf("listen grpc %s: %w", opts.grpcAddress, err)
	}
	defer grpcLis.Close()

	httpServer := &http.Server{
		Addr:              opts.httpAddress,
		Handler:           svc.HTTPHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	grpcErrCh := make(chan error, 1)
	go func() { grpcErrCh <- grpcServer.Serve(grpcLis) }()
	httpErrCh := make(chan error, 1)
	go func() { httpErrCh <- httpServer.ListenAndServe() }()

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
		return err
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
	flagSet.StringVar(&opts.grpcAddress, "grpc-address", defaultGRPCAddress, "storaged gRPC listen address")
	flagSet.StringVar(&opts.httpAddress, "http-address", defaultHTTPAddress, "storaged HTTP health listen address")
	flagSet.StringVar(&opts.postgresDSN, "postgres-dsn", os.Getenv("STORAGED_POSTGRES_DSN"), "Postgres DSN for storaged state")
	if err := flagSet.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if strings.TrimSpace(opts.postgresDSN) == "" {
		return options{}, fmt.Errorf("postgres-dsn is required")
	}
	return opts, nil
}
