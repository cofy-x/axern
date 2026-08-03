package appgateway

import (
	"context"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type routeReaderStub struct{ allocation *Allocation }

func (s routeReaderStub) LoadService(context.Context, string) (*servicev1.Service, error) {
	return nil, nil
}

func (s routeReaderStub) LoadAllocation(context.Context, string) (*Allocation, error) {
	return s.allocation, nil
}

func (s routeReaderStub) ReadyServiceEndpoints(context.Context, string) ([]EndpointTarget, error) {
	return nil, nil
}

type leaseIssuerStub struct{ calls int }

func (s *leaseIssuerStub) IssueExecutionLease(context.Context, string, int64, commonv1.LeaseType, time.Duration, time.Time) (*commonv1.ExecutionLease, error) {
	s.calls++
	return &commonv1.ExecutionLease{PlaintextToken: "lease-token"}, nil
}

func TestResolveAllocationTerminalAccessPurpose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		owner      string
		status     commonv1.AllocationStatus
		purpose    gatewayv1.AllocationAccessPurpose
		wantCode   codes.Code
		wantLeases int
	}{
		{name: "unspecified remains interactive", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, wantCode: codes.OK, wantLeases: 1},
		{name: "interactive running", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, wantCode: codes.OK, wantLeases: 1},
		{name: "interactive exited", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, wantCode: codes.FailedPrecondition},
		{name: "run output running", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantLeases: 1},
		{name: "run output exited", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantLeases: 1},
		{name: "run output releasing", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantLeases: 1},
		{name: "run output starting", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.FailedPrecondition},
		{name: "service output denied while running", owner: "service", status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.FailedPrecondition},
		{name: "unknown purpose", owner: "run", status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, purpose: gatewayv1.AllocationAccessPurpose(99), wantCode: codes.InvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			leases := &leaseIssuerStub{}
			resolver := NewResolver(routeReaderStub{allocation: &Allocation{AllocationID: "alloc-1", OwnerType: test.owner, OwnerID: "owner-1", NodeID: "node-1", NodeTarget: "node:24010", Attempt: 1, Status: test.status}}, leases)
			_, err := resolver.ResolveAllocationTerminal(context.Background(), &gatewayv1.ResolveAllocationTerminalRequest{AllocationID: "alloc-1", Purpose: test.purpose}, time.Minute, time.Now())
			if got := grpcstatus.Code(err); got != test.wantCode {
				t.Fatalf("ResolveAllocationTerminal() code = %v, want %v (err=%v)", got, test.wantCode, err)
			}
			if leases.calls != test.wantLeases {
				t.Fatalf("lease calls = %d, want %d", leases.calls, test.wantLeases)
			}
		})
	}
}

func TestResolvePortByName(t *testing.T) {
	t.Parallel()
	port, err := ResolvePort([]*commonv1.PortSpec{
		{Name: "http", Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP, ContainerPort: 8080},
	}, "http")
	if err != nil {
		t.Fatal(err)
	}
	if port.GetContainerPort() != 8080 || port.GetName() != "http" {
		t.Fatalf("port = %#v", port)
	}
}

func TestResolvePortByNumber(t *testing.T) {
	t.Parallel()
	port, err := ResolvePort([]*commonv1.PortSpec{
		{Name: "http", Protocol: commonv1.PortProtocol_PORT_PROTOCOL_TCP, ContainerPort: 8080},
	}, "8080")
	if err != nil {
		t.Fatal(err)
	}
	if port.GetContainerPort() != 8080 {
		t.Fatalf("container port = %d, want 8080", port.GetContainerPort())
	}
}

func TestResolvePortByNumberWithoutPortSpec(t *testing.T) {
	t.Parallel()
	port, err := ResolvePort(nil, "8080")
	if err != nil {
		t.Fatal(err)
	}
	if port.GetContainerPort() != 8080 || port.GetProtocol() != commonv1.PortProtocol_PORT_PROTOCOL_TCP {
		t.Fatalf("port = %#v", port)
	}
}

func TestResolvePortMissing(t *testing.T) {
	t.Parallel()
	if _, err := ResolvePort([]*commonv1.PortSpec{{Name: "http", ContainerPort: 8080}}, "metrics"); err == nil {
		t.Fatal("ResolvePort() unexpectedly succeeded")
	}
}
