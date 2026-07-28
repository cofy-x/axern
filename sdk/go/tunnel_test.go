package axernsdk

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/sdk/go/internal/tunnelrelay"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

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
		TemplateID:   "python311",
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
	if fake.tunnelAllocationID != "alloc-1" || fake.tunnelLocalTarget != "127.0.0.1:8080" || fake.tunnelRemotePort != 9000 {
		t.Fatalf("unexpected create tunnel request allocation=%q local=%q port=%d", fake.tunnelAllocationID, fake.tunnelLocalTarget, fake.tunnelRemotePort)
	}
	var config tunnelrelay.ConnectorConfig
	select {
	case config = <-connectorConfigs:
	case <-ctx.Done():
		t.Fatalf("connector runner was not called: %v", ctx.Err())
	}
	if config.SessionID != "tun-1" || config.EdgeTarget != "gateway.example:25000" || config.ClientToken != "client-token" || config.LocalTarget != "127.0.0.1:8080" || !config.RelayInsecure || config.ProxyMode != ProxyModeDirect || config.MaxStreams != 7 {
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
