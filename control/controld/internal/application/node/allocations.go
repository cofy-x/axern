package appnode

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"go.opentelemetry.io/otel/attribute"
)

type AllocationControl interface {
	BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationStatusObservation, now time.Time) ([]string, error)
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error
	ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error
	ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error
	WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error)
}

type AllocationOwnerResolver interface {
	ResolveAllocationOwners(ctx context.Context, allocationIDs []string) (map[string]string, error)
}

type RunAllocationStore interface {
	runkernel.AllocationReporter
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error
	WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error)
}

func (n authoritativeAllocationAccess) BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error {
	return n.runStore.BatchReportAllocationCapabilityConditions(ctx, nodeID, reports, now)
}

func (n authoritativeAllocationAccess) BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error {
	return n.runStore.BatchReportAllocationMemoryObservations(ctx, nodeID, observations, now)
}

func NewAuthoritative(owners AllocationOwnerResolver, runStore RunAllocationStore, serviceReporter servicekernel.AllocationReporter) AllocationControl {
	return authoritativeAllocationAccess{owners: owners, runStore: runStore, serviceReporter: serviceReporter}
}

type authoritativeAllocationAccess struct {
	owners          AllocationOwnerResolver
	runStore        RunAllocationStore
	serviceReporter servicekernel.AllocationReporter
}

func (n authoritativeAllocationAccess) BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationStatusObservation, now time.Time) ([]string, error) {
	allocationIDs := make([]string, 0, len(observations))
	for _, observation := range observations {
		if observation != nil {
			allocationIDs = append(allocationIDs, observation.GetAllocationID())
		}
	}
	owners, err := n.owners.ResolveAllocationOwners(ctx, allocationIDs)
	if err != nil {
		return nil, err
	}
	runObservations := observationsForOwner(observations, owners, allocationkernel.OwnerRun)
	if len(runObservations) > 0 {
		if err := n.runStore.BatchReportAllocationStatus(ctx, nodeID, runObservations, now); err != nil {
			return nil, err
		}
	}
	var reconcileServiceIDs []string
	serviceObservations := observationsForOwner(observations, owners, allocationkernel.OwnerService)
	if n.serviceReporter != nil && len(serviceObservations) > 0 {
		result, err := n.serviceReporter.BatchReportAllocationStatus(ctx, nodeID, serviceObservations, now)
		if err != nil {
			return nil, err
		}
		reconcileServiceIDs = result.ReconcileServiceIDs
		for _, report := range result.Reports {
			recordServiceReadyLatency(ctx, report)
		}
	}
	return reconcileServiceIDs, nil
}

func observationsForOwner(observations []*controlnodev1.AllocationStatusObservation, owners map[string]string, ownerType string) []*controlnodev1.AllocationStatusObservation {
	out := make([]*controlnodev1.AllocationStatusObservation, 0, len(observations))
	for _, observation := range observations {
		if observation != nil && owners[strings.TrimSpace(observation.GetAllocationID())] == ownerType {
			out = append(out, observation)
		}
	}
	return out
}

func (n authoritativeAllocationAccess) WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	return n.runStore.WatchExecutionLeases(ctx, nodeID, afterRevision, now)
}

func (n authoritativeAllocationAccess) ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error {
	if err := n.runStore.ReconcileNodeInventory(ctx, snapshot, now); err != nil {
		return err
	}
	if n.serviceReporter != nil {
		return n.serviceReporter.ReconcileNodeInventory(ctx, snapshot, now)
	}
	return nil
}

func (n authoritativeAllocationAccess) ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error {
	if err := n.runStore.ReconcileNodeUnavailable(ctx, nodeID, now); err != nil {
		return err
	}
	if n.serviceReporter != nil {
		return n.serviceReporter.ReconcileNodeUnavailable(ctx, nodeID, now)
	}
	return nil
}

func recordServiceReadyLatency(ctx context.Context, report *servicekernel.AllocationStatusReport) {
	if report == nil || !report.ReplicaBecameReady {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(sdkobs.AttrPath, "status_report"),
		attribute.String(sdkobs.AttrResult, sdkobs.ResultOK),
	}
	if report.ReplicaReadyDurationKnown {
		replicaAttrs := append([]attribute.KeyValue{}, attrs...)
		replicaAttrs = append(replicaAttrs, attribute.String(sdkobs.AttrStage, "replica_ready_total"))
		sdkobs.DurationHistogram(ctrlobs.MetricServiceReplicaReadyDuration.Name, ctrlobs.MetricServiceReplicaReadyDuration.Description).RecordDuration(ctx, report.ReplicaReadyDuration, replicaAttrs...)
	}
	if report.ServiceBecameReady && report.ServiceReadyDurationKnown {
		serviceAttrs := append([]attribute.KeyValue{}, attrs...)
		serviceAttrs = append(serviceAttrs, attribute.String(sdkobs.AttrStage, "service_ready_total"))
		sdkobs.DurationHistogram(ctrlobs.MetricServiceReadyDuration.Name, ctrlobs.MetricServiceReadyDuration.Description).RecordDuration(ctx, report.ServiceReadyDuration, serviceAttrs...)
	}
}
