package nodebridge

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	secretkernel "github.com/cofy-x/axern/control/controld/internal/kernel/secret"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	privatenodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	nodeLifecycleOperationCreateAllocation = "create_allocation"
	nodeLifecycleStageResolveCreateRequest = "resolve_create_request"
	nodeLifecycleStageNodeCreateRPC        = "node_create_rpc"
)

type Bridge struct {
	client              LifecycleClient
	secretValues        secretkernel.ValueResolver
	registryCredentials environmentkernel.RegistryCredentialResolver
	createTimeout       time.Duration
	operationTimeout    time.Duration
}

func cloneOCIImageDescriptor(in *environmentv1.OciImageDescriptor) *environmentv1.OciImageDescriptor {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*environmentv1.OciImageDescriptor)
}

type Config struct {
	CreateTimeout       time.Duration
	OperationTimeout    time.Duration
	SecretValues        secretkernel.ValueResolver
	RegistryCredentials environmentkernel.RegistryCredentialResolver
}

func New(client LifecycleClient, cfg Config) *Bridge {
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
		createTimeout:       cfg.CreateTimeout,
		operationTimeout:    cfg.OperationTimeout,
	}
}

func (b *Bridge) CreateAllocation(ctx context.Context, target string, run *runv1.Run, env *environmentv1.Environment, nodeID string, requirements []*capabilityv1.CapabilityRequirement) (*capabilityv1.CapabilityConditionSet, error) {
	callCtx, cancel := context.WithTimeout(ctx, b.createTimeout)
	defer cancel()
	stageStarted := time.Now()
	req, err := b.buildCreateAllocationRequest(callCtx, createAllocationRequestParams{
		AllocationID:           run.GetAllocationID(),
		Config:                 run.GetConfig(),
		Environment:            env,
		NodeID:                 nodeID,
		CapabilityRequirements: requirements,
	})
	recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateAllocation, nodeLifecycleStageResolveCreateRequest, stageStarted, err)
	if err != nil {
		return nil, err
	}
	stageStarted = time.Now()
	resp, err := b.client.CreateAllocation(callCtx, target, req)
	if err != nil {
		recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateAllocation, nodeLifecycleStageNodeCreateRPC, stageStarted, err)
		return nil, formatCreateAllocationError(err)
	}
	recordNodeLifecycleRPCStage(ctx, nodeLifecycleOperationCreateAllocation, nodeLifecycleStageNodeCreateRPC, stageStarted, nil)
	return cloneCapabilityConditionSet(resp.GetCapabilityVerification()), nil
}

func (b *Bridge) DeleteAllocation(ctx context.Context, target, allocationID string, nodeID string, outputSealing *allocationkernel.OutputSealing, snapshot *allocationkernel.RootfsSnapshotSealing) (*allocationkernel.RootfsSnapshotResult, error) {
	var callCtx context.Context
	var cancel context.CancelFunc
	if snapshot != nil {
		// The reconciler owns the longer snapshot-finalization deadline and
		// renews the durable claim while this call is active. Do not silently
		// re-cap the streaming RPC with the ordinary cleanup timeout.
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			callCtx, cancel = context.WithCancel(ctx)
		} else {
			callCtx, cancel = context.WithTimeout(ctx, allocationkernel.RootfsSnapshotOperationTimeout)
		}
	} else {
		callCtx, cancel = context.WithTimeout(ctx, b.operationTimeout)
	}
	defer cancel()
	var sealingRequest *privatenodev1.OutputSealingRequest
	if outputSealing != nil {
		sealingRequest = &privatenodev1.OutputSealingRequest{
			ExpiresAtUnixNano: outputSealing.ExpiresAt.UTC().UnixNano(),
			Outputs:           cloneDeclaredOutputs(outputSealing.Outputs),
		}
	}
	var snapshotRequest *privatenodev1.RootfsSnapshotSealingRequest
	if snapshot != nil {
		snapshotRequest = &privatenodev1.RootfsSnapshotSealingRequest{BaseImageRef: strings.TrimSpace(snapshot.BaseImageRef)}
		if credentialID := strings.TrimSpace(snapshot.RegistryCredentialID); credentialID != "" {
			if b.registryCredentials == nil {
				return nil, grpcstatus.Error(codes.FailedPrecondition, "snapshot base registry credential resolver is unavailable")
			}
			dockerConfigJSON, ok, resolveErr := b.registryCredentials.ResolveDockerConfigJSON(callCtx, credentialID)
			if resolveErr != nil {
				return nil, resolveErr
			}
			if !ok {
				return nil, grpcstatus.Errorf(codes.FailedPrecondition, "snapshot base registry credential %q not found", credentialID)
			}
			snapshotRequest.BaseRegistryCredential = &privatenodev1.RegistryCredential{DockerConfigJson: dockerConfigJSON}
		}
	}
	resp, err := b.client.DeleteAllocation(callCtx, target, &privatenodev1.DeleteAllocationRequest{
		AllocationID:          allocationID,
		NodeID:                nodeID,
		TimeoutSeconds:        10,
		OutputSealing:         sealingRequest,
		RootfsSnapshotSealing: snapshotRequest,
	})
	if grpcstatus.Code(err) == codes.NotFound {
		if snapshot != nil {
			return nil, grpcstatus.Error(codes.FailedPrecondition, "node no longer has the runtime or recovery state required to seal the rootfs snapshot")
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if result := resp.GetRootfsSnapshot(); result != nil {
		return &allocationkernel.RootfsSnapshotResult{
			ImageRef: result.GetImageRef(), ImageDescriptor: cloneOCIImageDescriptor(result.GetImageDescriptor()),
			PlatformOS: result.GetPlatformOS(), PlatformArch: result.GetPlatformArch(), PlatformVariant: result.GetPlatformVariant(),
		}, nil
	}
	return nil, nil
}

func (b *Bridge) AcknowledgeAllocationRelease(ctx context.Context, target, allocationID, nodeID string) error {
	callCtx, cancel := context.WithTimeout(ctx, b.operationTimeout)
	defer cancel()
	_, err := b.client.AcknowledgeAllocationRelease(callCtx, target, &privatenodev1.AcknowledgeAllocationReleaseRequest{AllocationID: allocationID, NodeID: nodeID})
	return err
}

func (b *Bridge) AllocationDeleted(ctx context.Context, target, allocationID string, nodeID string) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, b.operationTimeout)
	defer cancel()
	_, err := b.client.GetAllocationLifecycle(callCtx, target, &privatenodev1.GetAllocationLifecycleRequest{
		AllocationID: allocationID,
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
