package api

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type outputFollowTestService struct {
	fakeNodeSandboxService
	reading func(context.Context, string) ([]allocationoutput.Chunk, bool, error)
}

func (s *outputFollowTestService) ReadAllocationOutput(ctx context.Context, _ string, cursor string) ([]allocationoutput.Chunk, bool, error) {
	return s.reading(ctx, cursor)
}

func outputFollowTestClient(t *testing.T, server *grpc.Server, lifetime context.Context, svc *outputFollowTestService) nodesandboxv1.NodeSandboxClient {
	t.Helper()
	nodesandboxv1.RegisterNodeSandboxServer(server, NewNodeSandboxServer(lifetime, svc, "node-a", testAccessGrantValidator{}))
	listener := bufconn.Listen(64 << 10)
	t.Cleanup(func() { _ = listener.Close() })
	t.Cleanup(server.Stop)
	go func() { _ = server.Serve(listener) }()
	conn, err := grpc.NewClient("passthrough:///node-output",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return nodesandboxv1.NewNodeSandboxClient(conn)
}

func TestOutputFollowerStopsOnNodeShutdown(t *testing.T) {
	for _, test := range []struct {
		name        string
		pendingRead bool
	}{
		{name: "idle"},
		{name: "awaiting read", pendingRead: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			waiting := make(chan struct{})
			var observed sync.Once
			svc := &outputFollowTestService{
				reading: func(ctx context.Context, cursor string) ([]allocationoutput.Chunk, bool, error) {
					if cursor == "" {
						return []allocationoutput.Chunk{{Stream: "stdout", Data: []byte("hello"), Cursor: "next"}}, false, nil
					}
					observed.Do(func() { close(waiting) })
					if test.pendingRead {
						<-ctx.Done()
						return nil, false, grpcstatus.FromContextError(ctx.Err()).Err()
					}
					return nil, false, nil
				},
			}
			lifetime, stop := context.WithCancel(t.Context())
			defer stop()
			server := grpc.NewServer()
			client := outputFollowTestClient(t, server, lifetime, svc)
			ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), accessGrantTokenMetadataKey, "token"), 2*time.Second)
			defer cancel()
			follower, err := client.ReadOutput(ctx, &nodesandboxv1.ReadOutputRequest{AllocationID: "allocation-one", Follow: true})
			if err != nil {
				t.Fatal(err)
			}
			first, err := follower.Recv()
			if err != nil || string(first.GetData()) != "hello" || first.GetNextCursor() != "next" {
				t.Fatalf("follow did not deliver output and its cursor: %v, %v", first, err)
			}
			select {
			case <-waiting:
			case <-ctx.Done():
				t.Fatal("follower did not reach its next read")
			}
			start := time.Now()
			stop()
			drained := make(chan struct{})
			go func() {
				server.GracefulStop()
				close(drained)
			}()
			if _, err := follower.Recv(); grpcstatus.Code(err) != codes.Unavailable {
				t.Fatalf("follower shutdown got %v, want Unavailable", err)
			}
			select {
			case <-drained:
				t.Logf("follower drained gracefully in %s", time.Since(start))
			case <-ctx.Done():
				t.Fatal("output follower held the node drain")
			}
		})
	}
}

func TestOutputFollowerCanStillBeCanceledByCaller(t *testing.T) {
	svc := &outputFollowTestService{
		reading: func(context.Context, string) ([]allocationoutput.Chunk, bool, error) {
			return nil, false, nil
		},
	}
	server := grpc.NewServer()
	client := outputFollowTestClient(t, server, t.Context(), svc)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), accessGrantTokenMetadataKey, "token"), 2*time.Second)
	defer cancel()
	follower, err := client.ReadOutput(ctx, &nodesandboxv1.ReadOutputRequest{AllocationID: "allocation-one", Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follower.Header(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := follower.Recv(); grpcstatus.Code(err) != codes.Canceled {
		t.Fatalf("caller cancellation got %v, want Canceled", err)
	}
}

func TestReadOutputWithoutFollowCompletesDuringDrain(t *testing.T) {
	svc := &outputFollowTestService{
		reading: func(ctx context.Context, _ string) ([]allocationoutput.Chunk, bool, error) {
			return []allocationoutput.Chunk{{Stream: "stdout", Data: []byte("end"), Terminal: true}}, true, ctx.Err()
		},
	}
	lifetime, stop := context.WithCancel(t.Context())
	stop()
	server := grpc.NewServer()
	client := outputFollowTestClient(t, server, lifetime, svc)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), accessGrantTokenMetadataKey, "token"), 2*time.Second)
	defer cancel()
	snapshot, err := client.ReadOutput(ctx, &nodesandboxv1.ReadOutputRequest{AllocationID: "allocation-one"})
	if err != nil {
		t.Fatal(err)
	}
	chunk, err := snapshot.Recv()
	if err != nil || string(chunk.GetData()) != "end" || !chunk.GetTerminal() {
		t.Fatalf("bounded read did not return the retained terminal output: %v, %v", chunk, err)
	}
	if _, err := snapshot.Recv(); err != io.EOF {
		t.Fatalf("bounded read ended with %v, want EOF", err)
	}
}
