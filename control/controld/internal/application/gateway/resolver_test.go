package appgateway

import (
	"context"
	"testing"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	gatewayv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/gateway/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type routeReaderStub struct{ allocation *Allocation }

func (s routeReaderStub) LoadAllocation(context.Context, string) (*Allocation, error) {
	return s.allocation, nil
}

type accessGrantIssuerStub struct {
	calls   int
	purpose gatewayv1.AllocationAccessPurpose
	ttl     time.Duration
}

func (s *accessGrantIssuerStub) IssueAllocationAccessGrant(_ context.Context, _ string, purpose gatewayv1.AllocationAccessPurpose, ttl time.Duration, _ time.Time) (*accessgrantkernel.IssuedGrant, error) {
	s.calls++
	s.purpose, s.ttl = purpose, ttl
	return &accessgrantkernel.IssuedGrant{PlaintextToken: "access-token"}, nil
}

func TestAllocationAccessByPurpose(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		status     commonv1.AllocationLifecycleState
		purpose    gatewayv1.AllocationAccessPurpose
		wantCode   codes.Code
		wantGrants int
		retention  time.Duration
	}{
		{name: "unspecified remains interactive", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, wantCode: codes.OK, wantGrants: 1},
		{name: "interactive running", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, wantCode: codes.OK, wantGrants: 1},
		{name: "interactive exited", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE, wantCode: codes.FailedPrecondition},
		{name: "preview running", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_HTTP_PREVIEW, wantCode: codes.OK, wantGrants: 1},
		{name: "preview releasing", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_HTTP_PREVIEW, wantCode: codes.FailedPrecondition},
		{name: "run output running", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.OK, wantGrants: 1},
		{name: "run output released", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, retention: 30 * time.Second, wantCode: codes.OK, wantGrants: 1},
		{name: "run output releasing", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, retention: 30 * time.Second, wantCode: codes.OK, wantGrants: 1},
		{name: "run output at expiry", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.FailedPrecondition},
		{name: "run output starting", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING, purpose: gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT, wantCode: codes.FailedPrecondition},
		{name: "unknown purpose", status: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, purpose: gatewayv1.AllocationAccessPurpose(99), wantCode: codes.InvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			grants := &accessGrantIssuerStub{}
			now := time.Now().UTC()
			expiry := now.Add(test.retention)
			resolver := NewResolver(routeReaderStub{allocation: &Allocation{OutputExpiresAt: &expiry, AllocationID: "alloc-1", RunID: "run-1", NodeID: "node-1", NodeTarget: "node:24010", LifecycleState: test.status}}, grants)
			req := &gatewayv1.ResolveAllocationAccessRequest{AllocationID: "alloc-1", Purpose: test.purpose}
			if got := grpcstatus.Code(resolver.ValidateAllocationAccess(t.Context(), req, now)); got != test.wantCode || grants.calls != 0 {
				t.Fatalf("revalidation code = %s, grants = %d; want %s and no grant", got, grants.calls, test.wantCode)
			}
			_, err := resolver.ResolveAllocationAccess(t.Context(), req, time.Minute, now)
			if got := grpcstatus.Code(err); got != test.wantCode {
				t.Fatalf("ResolveAllocationAccess() code = %v, want %v (err=%v)", got, test.wantCode, err)
			}
			if grants.calls != test.wantGrants {
				t.Fatalf("access grant calls = %d, want %d", grants.calls, test.wantGrants)
			}
			if test.wantGrants == 1 {
				wantPurpose := test.purpose
				if wantPurpose == gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_UNSPECIFIED {
					wantPurpose = gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE
				}
				wantTTL := time.Minute
				if test.retention > 0 {
					wantTTL = test.retention
				}
				if grants.purpose != wantPurpose || grants.ttl != wantTTL {
					t.Fatalf("issued purpose = %v, TTL = %v; want %v, %v", grants.purpose, grants.ttl, wantPurpose, wantTTL)
				}
			}
		})
	}
}
