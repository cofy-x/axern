package allocation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"go.opentelemetry.io/otel/attribute"
)

// LaunchCapabilities supplies node observations and delivers the controller's durable decisions.
type LaunchCapabilities interface {
	AdmitLaunchRequirements(*runtime.StartRequest) ([]*capabilityv1.CapabilityRequirement, error)
	VerifyLaunchRequirements(*runtime.StartRequest) ([]*capabilityv1.CapabilityCondition, error)
	ReportCapabilityConditions(string, *capabilityv1.CapabilityConditionSet)
	WakeCapabilityReconcile(string)
}

// Launch serializes replay, admission, runtime activation and its final verification for one Allocation.
func (c *Controller) Launch(ctx context.Context, request *runtime.StartRequest, controlPlaneNodeID string, capabilities LaunchCapabilities) (response *runtime.StartResponse, err error) {
	if err := startplan.ValidateStartRequest(request); err != nil {
		return nil, errord.ToGRPC(err)
	}
	spanAttrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrAllocationID, request.GetAllocationID()),
		attribute.String(sdkobs.AttrRuntime, config.RuntimeNameRunsc),
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        sandboxobs.SpanAllocationStart,
		SpanAttrs:   spanAttrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrRuntime, config.RuntimeNameRunsc)},
		Counter:     sandboxobs.MetricAllocationStartTotal,
		Duration:    sandboxobs.MetricAllocationStartDuration,
	})
	defer func() { op.End(err) }()
	requestDigest, err := StartRequestDigest(request)
	if err != nil {
		return nil, fmt.Errorf("identify allocation request: %w", err)
	}
	leaseTTL := time.Duration(request.GetExecutionLeaseTtlSeconds()) * time.Second
	controlPlaneNodeID = strings.TrimSpace(controlPlaneNodeID)
	if controlPlaneNodeID != "" && leaseTTL <= 0 {
		return nil, errord.ToGRPC(fmt.Errorf("control-plane allocation requires a positive execution lease TTL: %w", errord.ErrInvalidArgument))
	}
	leaseExpiresAt := time.Now().Add(leaseTTL).UTC()
	unlockLifecycle, err := c.allocationLifecycleLocks.LockContext(ctx, request.GetAllocationID())
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	defer unlockLifecycle()
	if err := c.ValidateActivation(request.GetAllocationID()); err != nil {
		return nil, errord.ToGRPC(err)
	}
	replay, err := c.verifiedLaunchReplay(request.GetAllocationID(), controlPlaneNodeID, requestDigest)
	if err != nil {
		return nil, errord.ToGRPC(err)
	}
	if replay {
		if err := c.VerifyWorkspaceAttachment(request.GetAllocationID()); err != nil {
			return nil, c.terminateFailedLaunch(capabilities, request.GetAllocationID(), err)
		}
		resp, active, replayErr := c.existingActiveStartResponse(ctx, request)
		if replayErr != nil {
			return resp, errord.ToGRPC(replayErr)
		}
		if !active {
			return nil, errord.ToGRPC(fmt.Errorf("durably verified allocation has no active runtime: %w", errord.ErrFailedPrecondition))
		}
		if err := c.RenewExecutionLeases(map[string]time.Duration{request.GetAllocationID(): leaseTTL}, time.Now().UTC()); err != nil {
			return nil, errord.ToGRPC(err)
		}
		metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "replayed")
		return resp, nil
	}
	// A live replay is defined by the immutable request digest and
	// verified enforcement manifest above. Current node policy may legitimately differ
	// after a config or runtime identity change; applying it retroactively would
	// break idempotency. New creates still derive and verify the complete current
	// requirement contract before any allocation side effect.
	admitted, err := capabilities.AdmitLaunchRequirements(request)
	if err != nil {
		op.SetErrorStatus("allocation capability admission failed")
		return nil, err
	}
	if err := c.CheckWorkspaceAdmission(request); err != nil {
		return nil, errord.ToGRPC(fmt.Errorf("Workspace admission unavailable: %w", errord.ErrFailedPrecondition))
	}
	err = c.StoreAllocationIntent(request.GetAllocationID(), controlPlaneNodeID, requestDigest, leaseExpiresAt, request.GetResources(), admitted, request.GetDeclaredOutputs(), request.GetRootfsSnapshot(), request.GetWorkspace())
	if err != nil {
		op.SetErrorStatus("persist allocation capability requirements failed")
		return nil, err
	}
	resp, err := c.startAllocationWithLifecycleHeld(ctx, request)
	if err != nil {
		op.SetErrorStatus("allocation start failed")
		return nil, errord.ToGRPC(err)
	}
	verification, err := capabilities.VerifyLaunchRequirements(request)
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "post_create_failed")
		err = c.terminateFailedLaunch(capabilities, request.GetAllocationID(), fmt.Errorf("verify allocation capabilities after create: %w", err))
		op.SetErrorStatus("post-create capability enforcement failed")
		return nil, err
	}
	conditionSet, err := c.ReplaceCapabilityConditions(request.GetAllocationID(), verification, time.Now().UTC())
	if err != nil {
		return nil, c.terminateFailedLaunch(capabilities, request.GetAllocationID(), fmt.Errorf("build post-create capability conditions: %w", err))
	}
	resp.CapabilityVerification = conditionSet
	capabilities.ReportCapabilityConditions(request.GetAllocationID(), conditionSet)
	metrics.RecordCapabilityAllocationVerification(config.RuntimeNameRunsc, "verified")
	return resp, nil
}

func (c *Controller) terminateFailedLaunch(capabilities LaunchCapabilities, id string, cause error) error {
	// Only the bounded diagnosis may enter the durable record.
	if err := c.beginCapabilityTerminationLocked(id, errors.New("post-create enforcement verification failed")); err != nil {
		return fmt.Errorf("%w; persist durable fail-stop: %v", cause, err)
	}
	capabilities.WakeCapabilityReconcile(id)
	return cause
}
