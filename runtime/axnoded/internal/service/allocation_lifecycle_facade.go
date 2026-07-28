package service

import (
	"context"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
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
	resp, err := h.allocationController().Start(ctx, request)
	if err != nil {
		op.SetErrorStatus("allocation start failed")
		return resp, errord.ToGRPC(err)
	}
	return resp, nil
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
