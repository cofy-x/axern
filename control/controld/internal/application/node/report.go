package appnode

import (
	"context"
	"errors"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"go.opentelemetry.io/otel/attribute"
)

var errNodeReporterUnavailable = errors.New("node reporter is unavailable")

type ReportStore interface {
	Report(context.Context, nodekernel.ReportParams) (*nodekernel.Record, error)
}

type ReportRegistry interface {
	Report(nodeID, nodeTarget string, summary *nodev1.NodeSummary, now time.Time)
}

// Reporter is the application-level node-report use case. The store commits
// summary and durable reconcile work atomically; only then does
// this use case expose the new state through the in-process registry.
type Reporter struct {
	store       ReportStore
	registry    ReportRegistry
	allocations AllocationControl
	now         func() time.Time
}

func NewReporter(store ReportStore, registry ReportRegistry, allocations AllocationControl, now func() time.Time) *Reporter {
	return &Reporter{store: store, registry: registry, allocations: allocations, now: now}
}

func (r *Reporter) Report(ctx context.Context, params nodekernel.ReportParams) error {
	if r == nil || r.store == nil || r.registry == nil {
		return errNodeReporterUnavailable
	}
	record, err := r.store.Report(ctx, params)
	if err != nil {
		return err
	}
	recordCapabilityChanges(ctx, record.ReportedCapabilityChanges)
	r.registry.Report(record.NodeID, record.NodeTarget, record.Summary, record.LastHeartbeatAt)
	if !reportedAxnodedReady(record.Summary) || r.allocations == nil {
		return nil
	}
	now := params.Now
	if now.IsZero() && r.now != nil {
		now = r.now()
	}
	return r.allocations.ReconcileNodeInventory(ctx, allocationkernel.NodeInventorySnapshot{
		NodeID: record.NodeID, ActiveAllocationIDs: record.Summary.GetComponents().GetAxnoded().GetActiveAllocationIds(),
		CollectedAt: reportSnapshotTime(record.Summary, now),
	}, now)
}

func recordCapabilityChanges(ctx context.Context, changes []nodekernel.CapabilityChange) {
	counter := sdkobs.Int64Counter(ctrlobs.MetricNodeCapabilityChangeTotal.Name, ctrlobs.MetricNodeCapabilityChangeTotal.Description)
	for _, change := range changes {
		counter.Add(ctx, 1,
			attribute.String(sdkobs.AttrCapability, capabilitycontract.MetricKey(change.Key)),
			attribute.String(sdkobs.AttrState, change.NewState.String()),
			attribute.String(sdkobs.AttrReason, change.ReasonCode.String()),
		)
	}
}

func reportedAxnodedReady(summary *nodev1.NodeSummary) bool {
	axnoded := summary.GetComponents().GetAxnoded()
	return axnoded.GetReady() && axnoded.GetState() == nodev1.ComponentState_COMPONENT_STATE_READY
}

func reportSnapshotTime(summary *nodev1.NodeSummary, fallback time.Time) time.Time {
	if summary.GetCollectedAt() == nil || summary.GetCollectedAt().AsTime().IsZero() {
		return fallback
	}
	return summary.GetCollectedAt().AsTime()
}
