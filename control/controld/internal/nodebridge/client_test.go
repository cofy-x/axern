package nodebridge

import (
	"context"
	"net"
	"sync"
	"testing"

	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeNodeLifecycleServer struct {
	privatenodev1.UnimplementedNodeLifecycleServer
	mu           sync.Mutex
	deleteErr    error
	deleteErrors []error
	deleteCalls  int
}

func (s *fakeNodeLifecycleServer) DeleteAllocation(context.Context, *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls++
	if len(s.deleteErrors) > 0 {
		err := s.deleteErrors[0]
		s.deleteErrors = s.deleteErrors[1:]
		if err != nil {
			return nil, err
		}
	}
	if s.deleteErr != nil {
		return nil, s.deleteErr
	}
	return &privatenodev1.DeleteAllocationResponse{}, nil
}

func TestGRPCClientReusesConnectionPerTarget(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	server := grpc.NewServer()
	privatenodev1.RegisterNodeLifecycleServer(server, &fakeNodeLifecycleServer{})
	go server.Serve(lis)
	defer server.Stop()

	client := NewGRPCClient()
	defer client.Close()
	if _, err := client.client(context.Background(), lis.Addr().String()); err != nil {
		t.Fatalf("first client: %v", err)
	}
	if _, err := client.client(context.Background(), lis.Addr().String()); err != nil {
		t.Fatalf("second client: %v", err)
	}
	if got := len(client.conns); got != 1 {
		t.Fatalf("connection count = %d, want 1", got)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := len(client.conns); got != 0 {
		t.Fatalf("connection count after close = %d, want 0", got)
	}
}

func TestGRPCClientDiscardsUnavailableConnection(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	server := grpc.NewServer()
	fake := &fakeNodeLifecycleServer{
		deleteErr: status.Error(codes.Unavailable, "connection error: desc = \"error reading server preface: EOF\""),
	}
	privatenodev1.RegisterNodeLifecycleServer(server, fake)
	go server.Serve(lis)
	defer server.Stop()

	client := NewGRPCClient()
	defer client.Close()
	_, err = client.DeleteAllocation(context.Background(), lis.Addr().String(), &privatenodev1.DeleteAllocationRequest{
		AllocationID: "alloc-test",
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("delete error code = %v, want %v", status.Code(err), codes.Unavailable)
	}
	if got := len(client.conns); got != 0 {
		t.Fatalf("connection count after unavailable error = %d, want 0", got)
	}
	if fake.deleteCalls != idempotentRPCAttempts {
		t.Fatalf("delete calls = %d, want %d", fake.deleteCalls, idempotentRPCAttempts)
	}
}

func TestGRPCClientRetriesRecoverableDeleteOnFreshConnection(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	server := grpc.NewServer()
	fake := &fakeNodeLifecycleServer{
		deleteErrors: []error{status.Error(codes.Unavailable, "connection error: desc = \"error reading server preface: EOF\""), nil},
	}
	privatenodev1.RegisterNodeLifecycleServer(server, fake)
	go server.Serve(lis)
	defer server.Stop()

	client := NewGRPCClient()
	defer client.Close()
	if _, err := client.DeleteAllocation(context.Background(), lis.Addr().String(), &privatenodev1.DeleteAllocationRequest{
		AllocationID: "alloc-test",
	}); err != nil {
		t.Fatalf("DeleteAllocation() error = %v", err)
	}
	if fake.deleteCalls != 2 {
		t.Fatalf("delete calls = %d, want 2", fake.deleteCalls)
	}
	if got := len(client.conns); got != 1 {
		t.Fatalf("connection count after retry success = %d, want 1", got)
	}
}
