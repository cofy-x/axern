package controlplane

import (
	"context"
	"net"
	"testing"

	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNodeControlClientProviderReusesConnection(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	server := grpc.NewServer()
	nodev1.RegisterNodeControlServer(server, &fakeNodeControlServer{})
	go server.Serve(lis)
	defer server.Stop()

	provider := &nodeControlClientProvider{
		target: lis.Addr().String(),
		opts:   []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	}
	defer provider.Close()

	first, err := provider.Client(context.Background())
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	second, err := provider.Client(context.Background())
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	if first != second {
		t.Fatal("expected provider to return cached client")
	}
	if provider.conn == nil {
		t.Fatal("expected provider to keep a connection")
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if provider.conn != nil || provider.client != nil {
		t.Fatal("expected close to clear cached client")
	}
}
