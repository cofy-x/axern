package service

import (
	"context"
	"fmt"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"go.opentelemetry.io/otel/attribute"
)

func (h *sandboxService) Start(ctx context.Context, request *runtime.StartRequest) (*runtime.StartResponse, error) {
	spanAttrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrAllocationID, request.GetContainerID()),
		attribute.String(sdkobs.AttrRuntime, request.GetRuntimeTemplate().GetSandbox()),
	}
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name:        sandboxobs.SpanAllocationStart,
		SpanAttrs:   spanAttrs,
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrRuntime, request.GetRuntimeTemplate().GetSandbox())},
		Counter:     sandboxobs.MetricAllocationStartTotal,
		Duration:    sandboxobs.MetricAllocationStartDuration,
	})
	var err error
	defer func() { op.End(err) }()
	admitted, verification, err := h.admitCapabilityDependencies(request.GetCapabilityDependencies(), time.Now())
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "pre_create_failed")
		op.SetErrorStatus("allocation capability gate failed")
		return nil, fmt.Errorf("verify allocation capabilities before create: %w", err)
	}
	if len(admitted) > 0 {
		if err = h.allocationController().PrepareCapabilityDependencies(request.GetContainerID(), admitted); err != nil {
			op.SetErrorStatus("persist allocation capability manifest failed")
			return nil, err
		}
	}
	resp, err := h.allocationController().Start(ctx, request)
	if err != nil || resp == nil || resp.GetCode() != 0 {
		if err == nil {
			if resp == nil {
				err = fmt.Errorf("allocation start returned no response")
			} else {
				err = fmt.Errorf("allocation start failed: %s", resp.GetMessage())
			}
		}
		if cleanupErr := h.allocationController().CleanupFailedStart(ctx, request.GetContainerID()); cleanupErr != nil {
			err = fmt.Errorf("%w; cleanup failed-start allocation: %v", err, cleanupErr)
		}
		op.SetErrorStatus("allocation start failed")
		return resp, errord.ToGRPC(err)
	}
	admitted, verification, err = h.verifyPostCreateCapabilityDependencies(ctx, request.GetContainerID(), admitted, time.Now())
	if err != nil {
		metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "post_create_failed")
		_, deleteErr := h.allocationController().Delete(ctx, &runtime.DeleteRequest{ID: request.GetContainerID(), Timeout: 10})
		if deleteErr != nil {
			err = fmt.Errorf("verify allocation capabilities after create: %w; force cleanup: %v", err, deleteErr)
		} else {
			err = fmt.Errorf("verify allocation capabilities after create: %w", err)
		}
		op.SetErrorStatus("post-create capability enforcement failed")
		return nil, err
	}
	if len(admitted) > 0 {
		if err = h.allocationController().PrepareCapabilityDependencies(request.GetContainerID(), admitted); err != nil {
			_, deleteErr := h.allocationController().Delete(ctx, &runtime.DeleteRequest{ID: request.GetContainerID(), Timeout: 10})
			if deleteErr != nil {
				err = fmt.Errorf("persist post-create capability evidence: %w; force cleanup: %v", err, deleteErr)
			}
			return nil, err
		}
	}
	resp.CapabilityVerification = verification
	resp.AdmittedCapabilityDependencies = admitted
	metrics.RecordCapabilityAllocationVerification(request.GetRuntimeTemplate().GetSandbox(), "verified")
	return resp, nil
}

func (h *sandboxService) verifyPostCreateCapabilityDependencies(ctx context.Context, containerID string, dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	admitted, conditions, err := h.admitCapabilityDependencies(dependencies, now)
	if err != nil {
		return nil, nil, err
	}
	byKey := make(map[string]*capabilityv1.CapabilityCondition, len(conditions))
	for _, condition := range conditions {
		id, keyErr := capabilitycontract.KeyID(condition.GetKey())
		if keyErr != nil {
			return nil, nil, keyErr
		}
		byKey[id] = condition
	}
	for _, dependency := range admitted {
		if dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
			continue
		}
		verification := h.verifyAllocationCapability(ctx, containerID, dependency)
		if verification.State != contract.CapabilityVerificationVerified {
			return nil, nil, fmt.Errorf("verify enforced capability after runtime create: %s", verificationMessage(verification))
		}
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		if condition := byKey[id]; condition != nil {
			condition.Message = "runtime-specific enforcement verified after create"
		}
	}
	return admitted, conditions, nil
}

func (h *sandboxService) admitCapabilityDependencies(dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	if len(dependencies) == 0 {
		return nil, nil, nil
	}
	if h.capabilityManager == nil {
		return nil, nil, fmt.Errorf("capability manager is unavailable")
	}
	return h.capabilityManager.AdmitDependencies(dependencies, now)
}

func (h *sandboxService) Delete(ctx context.Context, request *runtime.DeleteRequest) (response *runtime.DeleteResponse, err error) {
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name: sandboxobs.SpanAllocationDelete,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrAllocationID, request.GetID()),
		},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "delete")},
		Counter:     sandboxobs.MetricAllocationDeleteTotal,
		Duration:    sandboxobs.MetricAllocationDeleteDuration,
	})
	defer func() { op.End(err) }()
	resp, err := h.allocationController().Delete(ctx, request)
	if err != nil {
		op.SetErrorStatus("allocation delete failed")
		return resp, errord.ToGRPC(err)
	}
	return resp, nil
}
