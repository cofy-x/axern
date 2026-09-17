package api

import (
	"context"
	"testing"

	nodenetworkv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/network/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type allocationNetworkServiceStub struct {
	network *service.SandboxNetwork
	err     error
	id      string
}

func (s *allocationNetworkServiceStub) ResolveAllocationNetwork(allocationID string) (*service.SandboxNetwork, error) {
	s.id = allocationID
	return s.network, s.err
}

func TestAllocationNetworkResolvesOnlyAllocationIdentity(t *testing.T) {
	stub := &allocationNetworkServiceStub{network: &service.SandboxNetwork{IP: "172.17.0.2", NetNSPath: "/run/netns/allocation-1"}}
	server := NewAllocationNetworkServer(stub)
	response, err := server.ResolveAllocationNetwork(context.Background(), &nodenetworkv1.ResolveAllocationNetworkRequest{AllocationID: " allocation-1 "})
	if err != nil {
		t.Fatalf("ResolveAllocationNetwork() error = %v", err)
	}
	if stub.id != "allocation-1" || response.GetAllocationID() != "allocation-1" || response.GetNetnsPath() != "/run/netns/allocation-1" {
		t.Fatalf("ResolveAllocationNetwork() response = %#v id = %q", response, stub.id)
	}
}

func TestAllocationNetworkFailsClosedOnUnknownIdentity(t *testing.T) {
	stub := &allocationNetworkServiceStub{err: errord.ErrNotFound}
	server := NewAllocationNetworkServer(stub)
	_, err := server.ResolveAllocationNetwork(context.Background(), &nodenetworkv1.ResolveAllocationNetworkRequest{AllocationID: "missing"})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("ResolveAllocationNetwork() code = %v, want NOT_FOUND", grpcstatus.Code(err))
	}
}
