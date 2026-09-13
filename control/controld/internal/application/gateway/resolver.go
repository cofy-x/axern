package appgateway

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type LeaseIssuer interface {
	IssueExecutionLease(ctx context.Context, allocationID string, leaseType commonv1.LeaseType, ttl time.Duration, now time.Time) (*commonv1.ExecutionLease, error)
}

type RouteReader interface {
	LoadAllocation(ctx context.Context, allocationID string) (*Allocation, error)
}

type Allocation struct {
	AllocationID   string
	RunID          string
	NodeID         string
	NodeTarget     string
	LifecycleState commonv1.AllocationLifecycleState
}

type Resolver struct {
	routes RouteReader
	leases LeaseIssuer
}

func NewResolver(routes RouteReader, leases LeaseIssuer) *Resolver {
	return &Resolver{routes: routes, leases: leases}
}

func (r *Resolver) ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	if r == nil || r.routes == nil || r.leases == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "gateway terminal resolver is not configured")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	if allocationID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	alloc, err := r.routes.LoadAllocation(ctx, allocationID)
	if err != nil {
		return nil, err
	}
	purpose := req.GetPurpose()
	if purpose == gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_UNSPECIFIED {
		purpose = gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE
	}
	if purpose != gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE && purpose != gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation access purpose is invalid")
	}
	terminalRunOutput := purpose == gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT &&
		allocationkernel.IsCleanupState(alloc.LifecycleState)
	if alloc.LifecycleState != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE && !terminalRunOutput {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "allocation is not active")
	}
	lease, err := r.leases.IssueExecutionLease(ctx, alloc.AllocationID, commonv1.LeaseType_LEASE_TYPE_RUN, ttl, now)
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: alloc.AllocationID,
		RunID:        alloc.RunID,
		NodeID:       alloc.NodeID,
		NodeTarget:   alloc.NodeTarget,
		Lease:        lease,
	}, nil
}
