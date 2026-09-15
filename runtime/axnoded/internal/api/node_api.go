package api

import (
	"context"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"strings"
	"time"

	obsmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

type nodeSandboxServer struct {
	nodesandboxv1.UnimplementedNodeSandboxServer
	svc             service.SandboxService
	nodeID          string
	accessGrantAuth DirectAccessGrantValidator
	localOnly       bool
}

type DirectAccessGrantValidator interface {
	WaitValidate(ctx context.Context, allocationID string, token string, purpose gatewayv1.AllocationAccessPurpose, now func() time.Time) (valid, waited bool)
}

const (
	accessGrantVisibilityWaitTimeout = 2 * time.Second
	accessGrantAcceptedHeaderKey     = "x-axern-allocation-access-accepted"
	accessGrantTokenMetadataKey      = "x-axern-allocation-access-token"
)

type accessGrantHeaderSender interface {
	SendHeader(metadata.MD) error
}

func acknowledgeAllocationAccessGrant(stream accessGrantHeaderSender) error {
	return stream.SendHeader(metadata.Pairs(accessGrantAcceptedHeaderKey, "1"))
}

type directAuthTarget struct {
	allocationID string
	targetID     string
}

func NewNodeSandboxServer(svc service.SandboxService, nodeID string, validator DirectAccessGrantValidator) nodesandboxv1.NodeSandboxServer {
	return &nodeSandboxServer{
		svc:             svc,
		nodeID:          nodeID,
		accessGrantAuth: validator,
	}
}

func NewLocalNodeSandboxServer(svc service.NodeService, nodeID string) nodesandboxv1.NodeSandboxServer {
	return &nodeSandboxServer{svc: svc, nodeID: nodeID, localOnly: true}
}

func (s *nodeSandboxServer) validateDirectAuth(ctx context.Context, allocationID string) (directAuthTarget, error) {
	return s.validateAccessPurpose(ctx, allocationID, gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE)
}

func (s *nodeSandboxServer) validateAccessPurpose(ctx context.Context, allocationID string, purpose gatewayv1.AllocationAccessPurpose) (directAuthTarget, error) {
	allocationID = strings.TrimSpace(allocationID)
	accessTokens := metadata.ValueFromIncomingContext(ctx, accessGrantTokenMetadataKey)
	if allocationID == "" || len(accessTokens) != 1 || strings.TrimSpace(accessTokens[0]) == "" {
		return directAuthTarget{}, grpcstatus.Error(codes.Unauthenticated, "allocation_id and internal allocation access metadata are required")
	}
	if s.localOnly {
		svc, ok := s.svc.(interface{ IsControlPlaneAllocation(string) bool })
		if !ok || svc.IsControlPlaneAllocation(allocationID) {
			return directAuthTarget{}, grpcstatus.Error(codes.PermissionDenied, "the conformance endpoint cannot access a control-plane-bound Allocation")
		}
	}
	accessToken := strings.TrimSpace(accessTokens[0])
	visibilityCtx, cancel := context.WithTimeout(ctx, accessGrantVisibilityWaitTimeout)
	defer cancel()
	visibilityStart := time.Now()
	valid, waited := s.localOnly, false
	if s.accessGrantAuth != nil {
		valid, waited = s.accessGrantAuth.WaitValidate(visibilityCtx, allocationID, accessToken, purpose, func() time.Time { return time.Now().UTC() })
	}
	result := "cache_hit"
	if waited {
		result = "event_wait"
	}
	if !valid {
		result = "known_invalid"
		if visibilityCtx.Err() != nil {
			result = "timeout"
		}
	}
	obsmetrics.RecordAllocationAccessGrantVisibility(time.Since(visibilityStart), result)
	if !valid {
		if err := ctx.Err(); err != nil {
			return directAuthTarget{}, grpcstatus.FromContextError(err).Err()
		}
		return directAuthTarget{}, grpcstatus.Error(codes.Unauthenticated, "allocation access grant is invalid, expired, revoked, or not current")
	}
	return directAuthTarget{
		allocationID: allocationID,
		targetID:     allocationID,
	}, nil
}
