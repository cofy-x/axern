package app

import (
	"context"

	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
)

type placementMetricsObserver struct{}

func (placementMetricsObserver) RecordSelection(ctx context.Context, observation placement.SelectionObservation) {
	resultAttrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrOperation, observation.Mode),
		attribute.String(sdkobs.AttrResult, observation.Result),
		attribute.String(sdkobs.AttrMountType, observation.MountType.String()),
	}
	sdkobs.Int64Counter(ctrlobs.MetricPlacementSelectionTotal.Name, ctrlobs.MetricPlacementSelectionTotal.Description).Add(ctx, 1, resultAttrs...)
	recordPlacementRequestedResources(ctx, observation, resultAttrs)
	recordPlacementCandidateCount(ctx, "eligible", observation.EligibleCount, observation)
	recordPlacementCandidateCount(ctx, "rejected", observation.RejectedCount, observation)
	recordPlacementRejections(ctx, observation)
}

func recordPlacementRequestedResources(ctx context.Context, observation placement.SelectionObservation, baseAttrs []attribute.KeyValue) {
	recordPlacementRequestedResource(ctx, "cpu_milli", observation.RequestedCPUMilli, baseAttrs)
	recordPlacementRequestedResource(ctx, "memory_bytes", observation.RequestedMemoryBytes, baseAttrs)
	recordPlacementRequestedResource(ctx, "writable_layer_bytes", observation.RequestedWritableLayerBytes, baseAttrs)
}

func recordPlacementRequestedResource(ctx context.Context, resource string, value int64, baseAttrs []attribute.KeyValue) {
	if value <= 0 {
		return
	}
	attrs := append([]attribute.KeyValue{}, baseAttrs...)
	attrs = append(attrs, attribute.String("resource", resource))
	sdkobs.Int64Counter(ctrlobs.MetricPlacementRequestedResourceTotal.Name, ctrlobs.MetricPlacementRequestedResourceTotal.Description).Add(ctx, value, attrs...)
}

func recordPlacementCandidateCount(ctx context.Context, state string, count int, observation placement.SelectionObservation) {
	if count <= 0 {
		return
	}
	sdkobs.Int64Counter(ctrlobs.MetricPlacementCandidateTotal.Name, ctrlobs.MetricPlacementCandidateTotal.Description).Add(ctx, int64(count),
		attribute.String(sdkobs.AttrOperation, observation.Mode),
		attribute.String(sdkobs.AttrResult, observation.Result),
		attribute.String(sdkobs.AttrMountType, observation.MountType.String()),
		attribute.String(sdkobs.AttrState, state),
	)
}

func recordPlacementRejections(ctx context.Context, observation placement.SelectionObservation) {
	reasons := observation.RejectionReasons
	if observation.Result == placement.SelectionResultNoEligible && len(reasons) == 0 {
		recordPlacementRejection(ctx, observation, "no_nodes")
		return
	}
	for _, reason := range reasons {
		recordPlacementRejection(ctx, observation, reason.String())
	}
}

func recordPlacementRejection(ctx context.Context, observation placement.SelectionObservation, reason string) {
	sdkobs.Int64Counter(ctrlobs.MetricPlacementRejectionTotal.Name, ctrlobs.MetricPlacementRejectionTotal.Description).Add(ctx, 1,
		attribute.String(sdkobs.AttrOperation, observation.Mode),
		attribute.String(sdkobs.AttrResult, observation.Result),
		attribute.String(sdkobs.AttrReason, reason),
	)
}
