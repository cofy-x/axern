package appgateway

import (
	"context"
	"testing"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type routeReaderStub struct{ allocation *Allocation }

func (s routeReaderStub) LoadAllocation(context.Context, string) (*Allocation, error) {
	return s.allocation, nil
}

type accessGrantIssuerStub struct{ calls int }

func (s *accessGrantIssuerStub) IssueAllocationAccessGrant(context.Context, string, time.Duration, time.Time) (*accessgrantkernel.IssuedGrant, error) {
	s.calls++
	return &accessgrantkernel.IssuedGrant{PlaintextToken: "access-token"}, nil
}

func TestResolveAllocationTerminalAccessPurpose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     commonv1.AllocationLifecycleState
		purpose    gatewayv1.AllocationAccessPurpose
		wantCode   codes.Code
		wantGrants int
	}{
		{name: "unspecified remains interactive", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, wantCode: codes.OK, wantGrants: 1},
		{name: "interactive running", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, wantCode: codes.OK, wantGrants: 1},
		{name: "interactive exited", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, wantCode: codes.FailedPrecondition},
		{name: "run output running", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantGrants: 1},
		{name: "run output exited", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantGrants: 1},
		{name: "run output releasing", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantGrants: 1},
		{name: "run output starting", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.FailedPrecondition},
		{name: "unknown purpose", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose(99), wantCode: codes.InvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			grants := &accessGrantIssuerStub{}
			resolver := NewResolver(routeReaderStub{allocation: &Allocation{AllocationID: "alloc-1", RunID: "run-1", NodeID: "node-1", NodeTarget: "node:24010", LifecycleState: test.status}}, grants)
			_, err := resolver.ResolveAllocationTerminal(context.Background(), &gatewayv1.ResolveAllocationTerminalRequest{AllocationID: "alloc-1", Purpose: test.purpose}, time.Minute, time.Now())
			if got := grpcstatus.Code(err); got != test.wantCode {
				t.Fatalf("ResolveAllocationTerminal() code = %v, want %v (err=%v)", got, test.wantCode, err)
			}
			if grants.calls != test.wantGrants {
				t.Fatalf("access grant calls = %d, want %d", grants.calls, test.wantGrants)
			}
		})
	}
}
