package axernsdk

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cofy-x/axern/sdk/go/internal/tunnelrelay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func startTunnelTest(t *testing.T, fake *fakeAxernServer) (context.Context, *Sandbox) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	server, dialer := newBufconnServer(t, fake)
	t.Cleanup(server.Stop)
	client, sandbox := startTestSandbox(t, ctx, dialer)
	t.Cleanup(func() { client.Close() })
	previous := tunnelConnectorRunner
	tunnelConnectorRunner = func(ctx context.Context, _ tunnelrelay.ConnectorConfig) error {
		<-ctx.Done()
		return ctx.Err()
	}
	t.Cleanup(func() { tunnelConnectorRunner = previous })
	return ctx, sandbox
}

func TestSandboxCloseRetriesAcknowledgedTunnel(t *testing.T) {
	for _, first := range []codes.Code{codes.Unavailable, codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition} {
		t.Run(first.String(), func(t *testing.T) {
			var calls atomic.Int32
			fake := &fakeAxernServer{revokeTunnel: func(ctx context.Context, id string) error {
				if id != "tun-1" {
					t.Errorf("revoke used %q", id)
				}
				if calls.Add(1) == 1 {
					return status.Error(first, "revocation not confirmed")
				}
				return status.Error(codes.NotFound, "session gone")
			}}
			ctx, sandbox := startTunnelTest(t, fake)
			tunnel, err := sandbox.OpenTunnel(ctx, TunnelOptions{Upstream: "127.0.0.1:8080"})
			if err != nil {
				t.Fatal(err)
			}
			if err := sandbox.Close(ctx); errorCode(err) != first {
				t.Fatalf("first close = %v, want %s", err, first)
			}
			if _, err := sandbox.State(); !errors.Is(err, ErrSandboxNotStarted) {
				t.Fatalf("state after close = %v", err)
			}
			if err := sandbox.Start(ctx); err == nil {
				t.Fatal("started another Run while the original Tunnel still needed revocation")
			}
			if err := tunnel.Close(ctx); err != nil {
				t.Fatalf("retry acknowledged tunnel: %v", err)
			}
			if err := sandbox.Close(ctx); err != nil || calls.Load() != 2 {
				t.Fatalf("final close = %v, revoke RPCs = %d, want 2", err, calls.Load())
			}
		})
	}
}

func TestOpenTunnelRetainsFailedSetupCleanup(t *testing.T) {
	var calls atomic.Int32
	fake := &fakeAxernServer{
		getTunnel: func(context.Context) error { return status.Error(codes.Unavailable, "readiness interrupted") },
		revokeTunnel: func(_ context.Context, id string) error {
			if id != "tun-1" {
				t.Errorf("revoke used %q", id)
			}
			if calls.Add(1) == 1 {
				return status.Error(codes.Unavailable, "revoke interrupted")
			}
			return nil
		},
	}
	ctx, sandbox := startTunnelTest(t, fake)
	tunnel, err := sandbox.OpenTunnel(ctx, TunnelOptions{Upstream: "127.0.0.1:8080"})
	if tunnel != nil || err == nil || !strings.Contains(err.Error(), "readiness interrupted") || !strings.Contains(err.Error(), "revoke interrupted") {
		t.Fatalf("setup and cleanup failures = %v", err)
	}
	if err := sandbox.Close(ctx); err != nil || calls.Load() != 2 {
		t.Fatalf("retry failed setup cleanup = %v, revoke RPCs = %d", err, calls.Load())
	}
}

func TestSandboxCloseDuringTunnelSetup(t *testing.T) {
	observing := make(chan struct{})
	var calls atomic.Int32
	fake := &fakeAxernServer{
		getTunnel: func(ctx context.Context) error {
			close(observing)
			<-ctx.Done()
			return ctx.Err()
		},
		revokeTunnel: func(_ context.Context, id string) error {
			if id != "tun-1" {
				t.Errorf("revoke used %q", id)
			}
			calls.Add(1)
			return nil
		},
	}
	ctx, sandbox := startTunnelTest(t, fake)
	openErr := make(chan error, 1)
	go func() {
		_, err := sandbox.OpenTunnel(ctx, TunnelOptions{Upstream: "127.0.0.1:8080"})
		openErr <- err
	}()
	select {
	case <-observing:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := sandbox.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-openErr:
		if err == nil || calls.Load() != 1 {
			t.Fatalf("open during close = %v, revoke RPCs = %d, want 1", err, calls.Load())
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestConcurrentTunnelCloseHonorsItsOwnDeadline(t *testing.T) {
	revoking := make(chan struct{})
	hold, release := context.WithCancel(t.Context())
	defer release()
	var calls atomic.Int32
	fake := &fakeAxernServer{revokeTunnel: func(ctx context.Context, id string) error {
		if calls.Add(1) == 1 {
			close(revoking)
		}
		select {
		case <-hold.Done():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	ctx, sandbox := startTunnelTest(t, fake)
	tunnel, err := sandbox.OpenTunnel(ctx, TunnelOptions{Upstream: "127.0.0.1:8080"})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- tunnel.Close(ctx) }()
	select {
	case <-revoking:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	short, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := tunnel.Close(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("concurrent close = %v, want its own deadline", err)
	}
	state, err := sandbox.State()
	if err != nil || state.TunnelSessionID != "" || state.BoundAddr != "" {
		t.Fatalf("local access while revoke is pending: state=%+v, err=%v", state, err)
	}
	release()
	if err := <-first; err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := sandbox.Close(ctx); err != nil || calls.Load() != 1 {
		t.Fatalf("close after confirmation = %v, revoke RPCs = %d", err, calls.Load())
	}
}

func TestSandboxOpenTunnelLifecycle(t *testing.T) {
	fake := &fakeAxernServer{files: map[string][]byte{}}
	server, dialer := newBufconnServer(t, fake)
	defer server.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := NewClient(
		ctx,
		"bufnet",
		WithDialOptions(
			grpc.WithContextDialer(dialer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		),
		WithProxyMode(ProxyModeDirect),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()
	client.relayOptions.Insecure = true

	connectorConfigs := make(chan tunnelrelay.ConnectorConfig, 1)
	previousRunner := tunnelConnectorRunner
	tunnelConnectorRunner = func(ctx context.Context, config tunnelrelay.ConnectorConfig) error {
		connectorConfigs <- config
		<-ctx.Done()
		return ctx.Err()
	}
	defer func() {
		tunnelConnectorRunner = previousRunner
	}()

	sandbox, err := NewSandbox(SandboxOptions{
		Client:       client,
		Image:        "python:3.12-slim",
		ReadyTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("new sandbox: %v", err)
	}
	if err := sandbox.Start(ctx); err != nil {
		t.Fatalf("start sandbox: %v", err)
	}
	tunnel, err := sandbox.OpenTunnel(ctx, TunnelOptions{
		Upstream:     "127.0.0.1:8080",
		ProxyPort:    9000,
		TTL:          time.Hour,
		ReadyTimeout: time.Second,
		Connector: TunnelConnectorOptions{
			MaxStreams: 7,
		},
	})
	if err != nil {
		t.Fatalf("open tunnel: %v", err)
	}
	if tunnel.SessionID() != "tun-1" || tunnel.BoundAddr() != "127.0.0.1:9000" {
		t.Fatalf("unexpected tunnel session=%q bound=%q", tunnel.SessionID(), tunnel.BoundAddr())
	}
	snapshot := tunnel.Session()
	snapshot.SessionID = "local-copy"
	if tunnel.Session().GetSessionID() != "tun-1" {
		t.Fatal("caller mutation changed the acknowledged session")
	}
	if fake.tunnelAllocationID != "alloc-1" || fake.tunnelRemotePort != 9000 {
		t.Fatalf("unexpected create tunnel request allocation=%q port=%d", fake.tunnelAllocationID, fake.tunnelRemotePort)
	}
	var config tunnelrelay.ConnectorConfig
	select {
	case config = <-connectorConfigs:
	case <-ctx.Done():
		t.Fatalf("connector runner was not called: %v", ctx.Err())
	}
	if config.SessionID != "tun-1" || config.ClientEdgeTarget != "gateway.example:25000" || config.ClientToken != "client-token" || config.LocalTarget != "127.0.0.1:8080" || !config.RelayInsecure || config.ProxyMode != ProxyModeDirect || config.MaxStreams != 7 {
		t.Fatalf("unexpected connector config: %+v", config)
	}
	metadata, err := sandbox.Metadata()
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if metadata.TunnelSessionID != "tun-1" || metadata.BoundAddr != "127.0.0.1:9000" {
		t.Fatalf("unexpected tunnel metadata: %+v", metadata)
	}
	if err := sandbox.Close(ctx); err != nil {
		t.Fatalf("close sandbox: %v", err)
	}
	if fake.revokedTunnelSessionID != "tun-1" || fake.revokedTunnelReason != "sdk close" {
		t.Fatalf("unexpected revoke tunnel request session=%q reason=%q", fake.revokedTunnelSessionID, fake.revokedTunnelReason)
	}
}
