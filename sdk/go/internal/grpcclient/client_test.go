package grpcclient

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestNewReadyClientWithPassthroughTargetPreservesDialerAddress(t *testing.T) {
	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()

	const target = "/tmp/axern-test.sock"
	gotAddr := make(chan string, 1)
	dialer := func(ctx context.Context, addr string) (net.Conn, error) {
		gotAddr <- addr
		return lis.DialContext(ctx)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := NewReadyClient(ctx, PassthroughTarget(target), grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("new ready client: %v", err)
	}
	defer conn.Close()

	select {
	case got := <-gotAddr:
		if got != target {
			t.Fatalf("dialer address = %q, want %q", got, target)
		}
	default:
		t.Fatal("dialer was not called")
	}
}
