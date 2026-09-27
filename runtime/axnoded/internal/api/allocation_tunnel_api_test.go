package api

import (
	"context"
	"testing"

	nodetunnelv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/tunnel/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type allocationTunnelServiceStub struct {
	err error
	id  string
}

func (s *allocationTunnelServiceStub) ValidateAllocationTunnel(allocationID string) error {
	s.id = allocationID
	return s.err
}

func TestAllocationTunnelValidatesOnlyAllocationIdentity(t *testing.T) {
	stub := &allocationTunnelServiceStub{}
	server := NewAllocationTunnelServer(stub)
	response, err := server.ValidateAllocationTunnel(context.Background(), &nodetunnelv1.ValidateAllocationTunnelRequest{AllocationID: " allocation-1 "})
	if err != nil {
		t.Fatalf("ValidateAllocationTunnel() error = %v", err)
	}
	if stub.id != "allocation-1" || response.GetAllocationID() != "allocation-1" {
		t.Fatalf("ValidateAllocationTunnel() response = %#v id = %q", response, stub.id)
	}
}

func TestAllocationTunnelFailsClosedOnUnknownIdentity(t *testing.T) {
	stub := &allocationTunnelServiceStub{err: errord.ErrNotFound}
	server := NewAllocationTunnelServer(stub)
	_, err := server.ValidateAllocationTunnel(context.Background(), &nodetunnelv1.ValidateAllocationTunnelRequest{AllocationID: "missing"})
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("ValidateAllocationTunnel() code = %v, want NOT_FOUND", grpcstatus.Code(err))
	}
}

func TestAllocationTunnelRejectsMissingIdentity(t *testing.T) {
	stub := &allocationTunnelServiceStub{}
	server := NewAllocationTunnelServer(stub)
	_, err := server.ValidateAllocationTunnel(context.Background(), &nodetunnelv1.ValidateAllocationTunnelRequest{})
	if grpcstatus.Code(err) != codes.InvalidArgument || stub.id != "" {
		t.Fatalf("ValidateAllocationTunnel() code = %v, want INVALID_ARGUMENT before service call", grpcstatus.Code(err))
	}
}
