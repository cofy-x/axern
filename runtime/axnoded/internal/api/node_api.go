package api

import (
	"context"
	"strings"
	"time"

	obsmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

type nodeSandboxServer struct {
	nodesandboxv1.UnimplementedNodeSandboxServer
	svc       service.SandboxService
	nodeID    string
	targets   *AllocationTargetRegistry
	leaseAuth DirectLeaseValidator
}

type DirectLeaseValidator interface {
	WaitValidate(ctx context.Context, allocationID string, attempt int64, token string, now func() time.Time) (valid, waited bool)
}

const (
	leaseVisibilityWaitTimeout      = 2 * time.Second
	executionLeaseAcceptedHeaderKey = "x-axern-execution-lease-accepted"
)

type executionLeaseHeaderSender interface {
	SendHeader(metadata.MD) error
}

func acknowledgeExecutionLease(stream executionLeaseHeaderSender) error {
	return stream.SendHeader(metadata.Pairs(executionLeaseAcceptedHeaderKey, "1"))
}

type directAuthTarget struct {
	allocationID string
	targetID     string
	attempt      int64
}

type allocationExitReport struct {
	allocationID  string
	attempt       int64
	exitCode      int32
	exitCodeKnown bool
	message       string
}

func NewNodeSandboxServer(svc service.SandboxService, nodeID string, targets *AllocationTargetRegistry, leaseAuth ...DirectLeaseValidator) nodesandboxv1.NodeSandboxServer {
	var validator DirectLeaseValidator
	if len(leaseAuth) > 0 {
		validator = leaseAuth[0]
	}
	return &nodeSandboxServer{
		svc:       svc,
		nodeID:    nodeID,
		targets:   targets,
		leaseAuth: validator,
	}
}

func (s *nodeSandboxServer) validateDirectAuth(ctx context.Context, allocationID string, attempt int64, leaseToken string) (directAuthTarget, error) {
	if strings.TrimSpace(allocationID) == "" || attempt <= 0 || strings.TrimSpace(leaseToken) == "" {
		return directAuthTarget{}, grpcstatus.Error(codes.Unauthenticated, "allocation_id, attempt, and execution_lease_token are required")
	}
	allocationID = strings.TrimSpace(allocationID)
	visibilityCtx, cancel := context.WithTimeout(ctx, leaseVisibilityWaitTimeout)
	defer cancel()
	visibilityStart := time.Now()
	valid, waited := true, false
	if s.leaseAuth != nil {
		valid, waited = s.leaseAuth.WaitValidate(visibilityCtx, allocationID, attempt, leaseToken, func() time.Time { return time.Now().UTC() })
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
	obsmetrics.RecordExecutionLeaseVisibility(time.Since(visibilityStart), result)
	if !valid {
		if err := ctx.Err(); err != nil {
			return directAuthTarget{}, grpcstatus.FromContextError(err).Err()
		}
		return directAuthTarget{}, grpcstatus.Error(codes.Unauthenticated, "execution lease is invalid, expired, revoked, or not current")
	}
	return directAuthTarget{
		allocationID: allocationID,
		targetID:     s.targets.resolve(allocationID),
		attempt:      attempt,
	}, nil
}

func (s *nodeSandboxServer) reportExit(report allocationExitReport) {
	s.svc.ReportAllocationStatus(
		report.allocationID,
		report.attempt,
		commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		report.exitCode,
		report.exitCodeKnown,
		false,
		"",
		report.message,
		time.Now().UTC(),
	)
}
