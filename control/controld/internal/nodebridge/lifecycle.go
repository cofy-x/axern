package nodebridge

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	DefaultRuntime = "runsc"
)

const (
	nodeLifecycleOperationCreateAllocation         = "create_allocation"
	nodeLifecycleOperationCreateResolvedAllocation = "create_resolved_allocation"
	nodeLifecycleStageResolveCreateRequest         = "resolve_create_request"
	nodeLifecycleStageNodeCreateRPC                = "node_create_rpc"
)

type Bridge struct {
	client              LifecycleClient
	secretValues        secretkernel.ValueResolver
	registryCredentials environmentkernel.RegistryCredentialResolver
	defaultRuntime      string
	createTimeout       time.Duration
	operationTimeout    time.Duration
}

type Config struct {
	DefaultRuntime      string
	CreateTimeout       time.Duration
	OperationTimeout    time.Duration
	SecretValues        secretkernel.ValueResolver
	RegistryCredentials environmentkernel.RegistryCredentialResolver
}

func New(client LifecycleClient, cfg Config) *Bridge {
	if cfg.DefaultRuntime == "" {
		cfg.DefaultRuntime = DefaultRuntime
	}
	if cfg.CreateTimeout <= 0 {
		cfg.CreateTimeout = allocationkernel.CreateExecutionTimeout
	}
	if cfg.OperationTimeout <= 0 {
		cfg.OperationTimeout = allocationkernel.LifecycleOperationTimeout
	}
	return &Bridge{
		client:              client,
		secretValues:        cfg.SecretValues,
		registryCredentials: cfg.RegistryCredentials,
		defaultRuntime:      cfg.DefaultRuntime,
		createTimeout:       cfg.CreateTimeout,
		operationTimeout:    cfg.OperationTimeout,
	}
}

func (b *Bridge) CreateAllocation(ctx context.Context, target string, run *runv1.Run, env *environmentv1.Environment, nodeID string) error {
	callCtx, cancel := context.WithTimeout(ctx, b.createTimeout)
	defer cancel()
	stageStarted := time.Now()
	req, err := b.buildCreateAllocationRequest(callCtx, createAllocationRequestParams{
		AllocationID:   run.GetAllocationID(),
		Attempt:        run.GetAttempt(),
		Config:         run.GetConfig(),
		Environment:    env,
		NodeID:         nodeID,
		DefaultRuntime: b.defaultRuntime,
	})
	recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateAllocation, nodeLifecycleStageResolveCreateRequest, stageStarted, err)
	if err != nil {
		return err
	}
	stageStarted = time.Now()
	if _, err := b.client.CreateAllocation(callCtx, target, req); err != nil {
		recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateAllocation, nodeLifecycleStageNodeCreateRPC, stageStarted, err)
		return formatCreateAllocationError(err)
	}
	recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateAllocation, nodeLifecycleStageNodeCreateRPC, stageStarted, nil)
	return nil
}

func (b *Bridge) CreateResolvedAllocation(ctx context.Context, req servicekernel.CreateResolvedAllocationRequest) (*servicekernel.CreateResolvedAllocationResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, b.createTimeout)
	defer cancel()
	stageStarted := time.Now()
	wireReq, err := b.buildCreateAllocationRequest(callCtx, createAllocationRequestParams{
		AllocationID:   req.AllocationID,
		Attempt:        req.Attempt,
		Config:         req.Config,
		Environment:    req.Environment,
		NodeID:         req.NodeID,
		DefaultRuntime: b.defaultRuntime,
		Namespace:      req.Namespace,
		ServiceID:      req.ServiceID,
		ReadinessProbe: req.ReadinessProbe,
		LivenessProbe:  req.LivenessProbe,
		NodeVolumes:    req.NodeVolumes,
	})
	recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateResolvedAllocation, nodeLifecycleStageResolveCreateRequest, stageStarted, err)
	if err != nil {
		return nil, err
	}
	stageStarted = time.Now()
	resp, err := b.client.CreateAllocation(callCtx, req.Target, wireReq)
	if err != nil {
		recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateResolvedAllocation, nodeLifecycleStageNodeCreateRPC, stageStarted, err)
		return nil, formatCreateAllocationError(err)
	}
	recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateResolvedAllocation, nodeLifecycleStageNodeCreateRPC, stageStarted, nil)
	return &servicekernel.CreateResolvedAllocationResult{
		PublishedVolumes:     clonePublishedNodeVolumes(resp.GetPublishedVolumes()),
		WorkspacePreparation: resp.GetWorkspacePreparation(),
	}, nil
}

func (b *Bridge) DeleteAllocation(ctx context.Context, target, allocationID string, attempt int64, nodeID string) error {
	_, err := b.DeleteResolvedAllocation(ctx, target, allocationID, attempt, nodeID)
	return err
}

func (b *Bridge) DeleteResolvedAllocation(ctx context.Context, target, allocationID string, attempt int64, nodeID string) ([]*privatestoragev1.VolumeReleaseObservation, error) {
	callCtx, cancel := context.WithTimeout(ctx, b.operationTimeout)
	defer cancel()
	resp, err := b.client.DeleteAllocation(callCtx, target, &privatenodev1.DeleteAllocationRequest{
		AllocationID:   allocationID,
		Attempt:        attempt,
		NodeID:         nodeID,
		TimeoutSeconds: 10,
	})
	if grpcstatus.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return cloneVolumeReleaseObservations(resp.GetVolumeReleaseObservations()), nil
}

func (b *Bridge) AllocationDeleted(ctx context.Context, target, allocationID string, attempt int64, nodeID string) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, b.operationTimeout)
	defer cancel()
	_, err := b.client.GetAllocationStatus(callCtx, target, &privatenodev1.GetAllocationStatusRequest{
		AllocationID: allocationID,
		Attempt:      attempt,
		NodeID:       nodeID,
	})
	if grpcstatus.Code(err) == codes.NotFound {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

func (b *Bridge) DeleteVolume(ctx context.Context, target string, reclaim *privatestoragev1.VolumeReclaim) error {
	callCtx, cancel := context.WithTimeout(ctx, b.operationTimeout)
	defer cancel()
	_, err := b.client.DeleteVolume(callCtx, target, &privatenodev1.DeleteVolumeRequest{
		ClaimID: reclaim.GetClaimID(), Backend: reclaim.GetBackend(), BackendHandle: reclaim.GetBackendHandle(), NodeID: reclaim.GetNodeID(),
	})
	return err
}

func (b *Bridge) buildCreateAllocationRequest(ctx context.Context, params createAllocationRequestParams) (*privatenodev1.CreateAllocationRequest, error) {
	resolved, err := resolveExecutionSecrets(ctx, b.secretValues, b.registryCredentials, params.Config, params.Environment)
	if err != nil {
		return nil, err
	}
	params.ResolvedSecrets = resolved
	return buildCreateAllocationRequestFromParams(params), nil
}

func recordNodeLifecycleRPCStage(ctx context.Context, operation, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = nodeLifecycleErrorClass(err)
	}
	sdkobs.DurationHistogram(ctrlobs.MetricNodeLifecycleRPCDuration.Name, ctrlobs.MetricNodeLifecycleRPCDuration.Description).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrOperation, operation),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func nodeLifecycleErrorClass(err error) string {
	if err == nil {
		return ""
	}
	code := grpcstatus.Code(err)
	if code != codes.OK && code != codes.Unknown {
		return strings.ToLower(code.String())
	}
	return "error"
}
