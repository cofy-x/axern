package appgateway

import (
	"context"
	"strings"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type AccessGrantIssuer interface {
	IssueAllocationAccessGrant(ctx context.Context, allocationID string, purpose gatewayv1.AllocationAccessPurpose, ttl time.Duration, now time.Time) (*accessgrantkernel.IssuedGrant, error)
}

type RouteReader interface {
	LoadAllocation(ctx context.Context, allocationID string) (*Allocation, error)
}

type Allocation struct {
	OutputExpiresAt *time.Time
	AllocationID    string
	RunID           string
	NodeID          string
	NodeTarget      string
	LifecycleState  commonv1.AllocationLifecycleState
}

type Resolver struct {
	routes       RouteReader
	accessGrants AccessGrantIssuer
}

func NewResolver(routes RouteReader, accessGrants AccessGrantIssuer) *Resolver {
	return &Resolver{routes: routes, accessGrants: accessGrants}
}

func (r *Resolver) ResolveAllocationTerminal(ctx context.Context, req *gatewayv1.ResolveAllocationTerminalRequest, ttl time.Duration, now time.Time) (*gatewayv1.ResolveAllocationTerminalResponse, error) {
	if r == nil || r.routes == nil || r.accessGrants == nil {
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
		allocationkernel.IsCleanupState(alloc.LifecycleState) && alloc.OutputExpiresAt != nil && now.Before(*alloc.OutputExpiresAt)
	if alloc.LifecycleState != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE && !terminalRunOutput {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "allocation is not active")
	}
	if terminalRunOutput && (ttl <= 0 || now.Add(ttl).After(*alloc.OutputExpiresAt)) {
		ttl = alloc.OutputExpiresAt.Sub(now)
	}
	grant, err := r.accessGrants.IssueAllocationAccessGrant(ctx, alloc.AllocationID, purpose, ttl, now)
	if err != nil {
		return nil, err
	}
	return &gatewayv1.ResolveAllocationTerminalResponse{
		AllocationID: alloc.AllocationID,
		RunID:        alloc.RunID,
		NodeID:       alloc.NodeID,
		NodeTarget:   alloc.NodeTarget,
		AccessGrant: &gatewayv1.AllocationAccessGrant{
			GrantID:        grant.GrantID,
			AllocationID:   grant.AllocationID,
			NodeID:         grant.NodeID,
			PlaintextToken: grant.PlaintextToken,
			ExpiresAt:      timestamppb.New(grant.ExpiresAt),
		},
	}, nil
}
