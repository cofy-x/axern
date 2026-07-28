package nodebridge

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestProxyHTTPWaitsForLeaseAcceptanceBeforeReadingBody(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	backend := &leaseHandshakeBackend{}
	nodesandboxv1.RegisterNodeSandboxServer(server, backend)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	dialer := NewDialer(nil)
	t.Cleanup(func() { _ = dialer.Close() })
	staleBody := &countingReader{Reader: strings.NewReader("must-not-be-read")}
	_, err = dialer.ProxyHTTP(context.Background(), nodekernel.HTTPProxySpec{
		NodeTarget: listener.Addr().String(), AllocationID: "alloc-1", Attempt: 1,
		Token: "stale-token", Port: 8080, Method: "POST", Path: "/", Body: staleBody,
		HasBody: true, ContentLength: int64(len("must-not-be-read")), Timeout: time.Second,
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("ProxyHTTP(stale) error = %v, want Unauthenticated", err)
	}
	if staleBody.bytes != 0 {
		t.Fatalf("stale request body bytes read = %d, want 0", staleBody.bytes)
	}

	freshBody := &countingReader{Reader: strings.NewReader("payload")}
	resp, err := dialer.ProxyHTTP(context.Background(), nodekernel.HTTPProxySpec{
		NodeTarget: listener.Addr().String(), AllocationID: "alloc-1", Attempt: 2,
		Token: "fresh-token", Port: 8080, Method: "POST", Path: "/", Body: freshBody,
		HasBody: true, ContentLength: int64(len("payload")), Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("ProxyHTTP(fresh) error = %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" || backend.body != "payload" {
		t.Fatalf("response body = %q backend body = %q", body, backend.body)
	}
}

type countingReader struct {
	io.Reader
	bytes int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += n
	return n, err
}

type leaseHandshakeBackend struct {
	nodesandboxv1.UnimplementedNodeSandboxServer
	body string
}

func (b *leaseHandshakeBackend) ProxyHTTP(stream nodesandboxv1.NodeSandbox_ProxyHTTPServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first.GetOpen().GetExecutionLeaseToken() == "stale-token" {
		return status.Error(codes.Unauthenticated, "execution lease is invalid")
	}
	if err := stream.SendHeader(metadata.Pairs(nodekernel.ExecutionLeaseAcceptedHeader, "1")); err != nil {
		return err
	}
	var body strings.Builder
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if chunk := req.GetBody(); len(chunk) > 0 {
			_, _ = body.Write(chunk)
		}
	}
	b.body = body.String()
	if err := stream.Send(&nodesandboxv1.ProxyHTTPResponse{
		Payload: &nodesandboxv1.ProxyHTTPResponse_Head{Head: &nodesandboxv1.ProxyHTTPResponseHead{StatusCode: 200}},
	}); err != nil {
		return err
	}
	return stream.Send(&nodesandboxv1.ProxyHTTPResponse{
		Payload: &nodesandboxv1.ProxyHTTPResponse_Body{Body: []byte("ok")},
	})
}
