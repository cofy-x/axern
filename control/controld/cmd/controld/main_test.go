package main

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestShutdownDrainsOpenWatch(t *testing.T) {
	lifetime, stop := context.WithCancel(context.Background())
	defer stop()
	server := grpc.NewServer(grpc.StreamInterceptor(cancelWatchesOnShutdown(lifetime)))
	defer server.Stop()
	healthv1.RegisterHealthServer(server, health.NewServer())
	listener := bufconn.Listen(64 << 10)
	defer listener.Close()
	go server.Serve(listener)

	conn, err := grpc.NewClient("passthrough:///watch",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	watch, err := healthv1.NewHealthClient(conn).Watch(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := watch.Recv(); err != nil {
		t.Fatal(err)
	}
	stop()
	drained := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(drained)
	}()
	if _, err := watch.Recv(); status.Code(err) != codes.Unavailable {
		t.Fatalf("open watch on controller shutdown returned %v", err)
	}
	select {
	case <-drained:
	case <-ctx.Done():
		t.Fatal("open watch held the controller drain")
	}
}

func TestParseFlagsResourceCPUOvercommitRatio(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{
		"controld",
		"-postgres-dsn=postgres://test",
		"-secrets-master-key=test-only-master-key-32-bytes!!!",
		"-tls-ca-cert=ca.crt",
		"-workload-cluster=cluster.test",
		"-workload-bundle=controld.pem",
		"-workload-signer-bundle=signer.pem",
		"-resource-cpu-overcommit-ratio=2.5",
	}

	opts, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if opts.resourceCPUOvercommitRatio != 2.5 {
		t.Fatalf("resourceCPUOvercommitRatio = %v, want 2.5", opts.resourceCPUOvercommitRatio)
	}
}

func TestParseFlagsRejectsInvalidResourceCPUOvercommitRatio(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{
		"controld",
		"-postgres-dsn=postgres://test",
		"-secrets-master-key=test-only-master-key-32-bytes!!!",
		"-tls-ca-cert=ca.crt",
		"-workload-cluster=cluster.test",
		"-workload-bundle=controld.pem",
		"-workload-signer-bundle=signer.pem",
		"-resource-cpu-overcommit-ratio=0",
	}

	_, err := parseFlags()
	if err == nil {
		t.Fatal("parseFlags() returned nil error")
	}
	if !strings.Contains(err.Error(), "resource-cpu-overcommit-ratio must be > 0") {
		t.Fatalf("parseFlags() error = %q", err)
	}
}

func TestParseFlagsRejectsNegativeReconcileTimeout(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{
		"controld",
		"-postgres-dsn=postgres://test",
		"-secrets-master-key=test-only-master-key-32-bytes!!!",
		"-tls-ca-cert=ca.crt",
		"-workload-cluster=cluster.test",
		"-workload-bundle=controld.pem",
		"-workload-signer-bundle=signer.pem",
		"-reconcile-timeout=" + (-time.Second).String(),
	}

	_, err := parseFlags()
	if err == nil {
		t.Fatal("parseFlags() returned nil error")
	}
	if !strings.Contains(err.Error(), "reconcile-timeout must be >= 0") {
		t.Fatalf("parseFlags() error = %q", err)
	}
}
