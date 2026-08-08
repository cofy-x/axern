package app

import (
	"context"
	"fmt"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"go.opentelemetry.io/otel/attribute"
)

func (a *App) observeNodes(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	counts := map[string]int64{
		"ready":     0,
		"stale":     0,
		"not_ready": 0,
		"retired":   0,
	}
	now := a.now()
	for _, record := range a.registry.Snapshot().Records {
		state := a.nodeState(record, now)
		counts[state]++
	}
	for state, count := range counts {
		observe(count, attribute.String(sdkobs.AttrState, state))
	}
	return nil
}

func (a *App) observeResourcePolicy(_ context.Context, observe sdkobs.Float64GaugeObserver) error {
	observe(a.resourcePolicy.CPUOvercommitRatio,
		attribute.String(sdkobs.AttrResource, "cpu_milli"),
		attribute.String(sdkobs.AttrState, "overcommit_ratio"),
	)
	observe(float64(a.resourcePolicy.RunscRuntimeOverheadMemoryBytes),
		attribute.String(sdkobs.AttrResource, "runsc_runtime_overhead_memory_bytes"),
		attribute.String(sdkobs.AttrState, "reservation"),
	)
	return nil
}

func (a *App) observeNodeResources(ctx context.Context, observe sdkobs.Int64GaugeObserver) error {
	reserved, err := a.activeReservedResources(ctx)
	if err != nil {
		return err
	}
	for _, record := range a.readyNodeRecords() {
		nodeReserved := reserved[record.NodeID]
		summary := record.Summary
		cpuCapacity := summary.GetCapacity().GetCpuMilli()
		cpuAllocatable := summary.GetAllocatable().GetCpuMilli()
		memoryCapacity := summary.GetCapacity().GetMemoryBytes()
		memoryAllocatable := summary.GetAllocatable().GetMemoryBytes()
		writableCapacity := summary.GetCapacity().GetWritableLayerBytes()
		writableAllocatable := summary.GetAllocatable().GetWritableLayerBytes()
		effective := a.resourcePolicy.EffectiveAllocatable(summary.GetAllocatable())
		observeNodeResource(observe, nodeResourceObservation{
			nodeID:               record.NodeID,
			resource:             "cpu_milli",
			capacity:             cpuCapacity,
			allocatable:          cpuAllocatable,
			effectiveAllocatable: effective.CPUMilli,
			reserved:             nodeReserved.cpuMilli,
		})
		observeNodeResource(observe, nodeResourceObservation{
			nodeID:               record.NodeID,
			resource:             "memory_bytes",
			capacity:             memoryCapacity,
			allocatable:          memoryAllocatable,
			effectiveAllocatable: effective.MemoryBytes,
			reserved:             nodeReserved.memoryBytes,
		})
		observeNodeResource(observe, nodeResourceObservation{
			nodeID: record.NodeID, resource: "writable_layer_bytes",
			capacity: writableCapacity, allocatable: writableAllocatable,
			effectiveAllocatable: effective.WritableLayerBytes, reserved: nodeReserved.writableLayerBytes,
		})
		if runtimeCapacity, known := nodekernel.RuntimeSlotCapacity(summary); known {
			observeNodeResource(observe, nodeResourceObservation{
				nodeID:               record.NodeID,
				resource:             "runtime_slots",
				capacity:             runtimeCapacity,
				allocatable:          runtimeCapacity,
				effectiveAllocatable: runtimeCapacity,
				reserved:             nodeReserved.instances,
			})
		}
	}
	return nil
}

func (a *App) observeNodePools(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	for _, record := range a.readyNodeRecords() {
		cgroup := record.Summary.GetPools().GetCgroup()
		if cgroup != nil {
			observeNodePool(observe, record.NodeID, "cgroup", int64(cgroup.GetIdle()), int64(cgroup.GetUsing()), int64(cgroup.GetCapacity()), int64(cgroup.GetUnavailable()))
		}
		iface := record.Summary.GetPools().GetInterface()
		if iface != nil {
			observeNodePool(observe, record.NodeID, "interface", int64(iface.GetIdle()), int64(iface.GetUsing()), int64(iface.GetCapacity()), int64(iface.GetUnavailable()))
		}
	}
	return nil
}

func (a *App) observeNodeStorage(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	for _, record := range a.readyNodeRecords() {
		for _, storage := range record.Summary.GetStorage() {
			observeNodeStorage(observe, record.NodeID, storage)
		}
	}
	return nil
}

func (a *App) observeNodeBPFNet(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	for _, record := range a.readyNodeRecords() {
		observeNodeBPFNet(observe, record.NodeID, record.Summary.GetComponents().GetBpfnet())
	}
	return nil
}

func (a *App) observeNodeImages(_ context.Context, observe sdkobs.Int64GaugeObserver) error {
	for _, record := range a.readyNodeRecords() {
		imagemgr := record.Summary.GetComponents().GetImagemgr()
		observe(int64(imagemgr.GetImportedImageCount()),
			attribute.String(sdkobs.AttrNodeID, record.NodeID),
			attribute.String(sdkobs.AttrState, "imported"),
			attribute.String(sdkobs.AttrSource, "imagemgr"),
		)
		observe(int64(imagemgr.GetMountedImageCount()),
			attribute.String(sdkobs.AttrNodeID, record.NodeID),
			attribute.String(sdkobs.AttrState, "mounted"),
			attribute.String(sdkobs.AttrSource, "imagemgr"),
		)
	}
	return nil
}

func (a *App) readyNodeRecords() []*nodekernel.Record {
	now := a.now()
	records := make([]*nodekernel.Record, 0)
	for _, record := range a.registry.Snapshot().Records {
		if a.nodeState(record, now) == "ready" {
			records = append(records, record)
		}
	}
	return records
}

func (a *App) nodeState(record *nodekernel.Record, now time.Time) string {
	if record == nil {
		return "not_ready"
	}
	if !record.Active() {
		return "retired"
	}
	heartbeatFresh := nodekernel.HeartbeatFresh(record.UpdatedAt, now, a.heartbeatFreshnessWindow)
	summaryFresh := nodekernel.SummaryFresh(record.Summary, now, a.summaryFreshnessWindow)
	if !heartbeatFresh || !summaryFresh {
		return "stale"
	}
	if axnodedReadySummary(record.Summary) {
		return "ready"
	}
	return "not_ready"
}

func axnodedReadySummary(summary *nodev1.NodeSummary) bool {
	if summary == nil || summary.GetComponents() == nil || summary.GetComponents().GetAxnoded() == nil {
		return false
	}
	axnoded := summary.GetComponents().GetAxnoded()
	return axnoded.GetReady() && axnoded.GetState() == nodev1.ComponentState_COMPONENT_STATE_READY
}

type nodeReservedResources struct {
	cpuMilli           int64
	memoryBytes        int64
	writableLayerBytes int64
	instances          int64
}

type nodeResourceObservation struct {
	nodeID               string
	resource             string
	capacity             int64
	allocatable          int64
	effectiveAllocatable int64
	reserved             int64
}

func (a *App) activeReservedResources(ctx context.Context) (map[string]nodeReservedResources, error) {
	rows, err := a.db.Pool().Query(ctx, `
		SELECT node_id, COALESCE(sum(cpu_milli), 0), COALESCE(sum(memory_bytes + memory_overhead_bytes), 0), COALESCE(sum(writable_layer_bytes), 0), COUNT(*)
		FROM workload_reservations
		WHERE released_at IS NULL
		GROUP BY node_id
	`)
	if err != nil {
		return nil, fmt.Errorf("query reserved resources: %w", err)
	}
	defer rows.Close()

	out := map[string]nodeReservedResources{}
	for rows.Next() {
		var nodeID string
		var reserved nodeReservedResources
		if err := rows.Scan(&nodeID, &reserved.cpuMilli, &reserved.memoryBytes, &reserved.writableLayerBytes, &reserved.instances); err != nil {
			return nil, err
		}
		out[nodeID] = reserved
	}
	return out, rows.Err()
}

func observeNodeResource(observe sdkobs.Int64GaugeObserver, observation nodeResourceObservation) {
	observe(observation.capacity, nodeResourceAttrs(observation.nodeID, observation.resource, "capacity")...)
	observe(observation.allocatable, nodeResourceAttrs(observation.nodeID, observation.resource, "allocatable")...)
	observe(observation.effectiveAllocatable, nodeResourceAttrs(observation.nodeID, observation.resource, "effective_allocatable")...)
	observe(observation.reserved, nodeResourceAttrs(observation.nodeID, observation.resource, "reserved")...)
	observe(maxInt64(observation.effectiveAllocatable-observation.reserved, 0), nodeResourceAttrs(observation.nodeID, observation.resource, "available")...)
}

func observeNodePool(observe sdkobs.Int64GaugeObserver, nodeID, resource string, idle, using, capacity, unavailable int64) {
	observe(idle, nodeResourceAttrs(nodeID, resource, "idle")...)
	observe(using, nodeResourceAttrs(nodeID, resource, "using")...)
	observe(capacity, nodeResourceAttrs(nodeID, resource, "capacity")...)
	observe(unavailable, nodeResourceAttrs(nodeID, resource, "unavailable")...)
}

func observeNodeStorage(observe sdkobs.Int64GaugeObserver, nodeID string, storage *nodev1.NodeStorageSummary) {
	if storage == nil || storage.GetTarget() == "" {
		return
	}
	observe(boolToInt64(storage.GetCollected()), nodeStorageAttrs(nodeID, storage.GetTarget(), "collected")...)
	if !storage.GetCollected() {
		return
	}
	observe(storage.GetCapacityBytes(), nodeStorageAttrs(nodeID, storage.GetTarget(), "capacity")...)
	observe(storage.GetUsedBytes(), nodeStorageAttrs(nodeID, storage.GetTarget(), "used")...)
	observe(storage.GetAvailableBytes(), nodeStorageAttrs(nodeID, storage.GetTarget(), "available")...)
	observe(storage.GetInodesTotal(), nodeStorageAttrs(nodeID, storage.GetTarget(), "inodes_total")...)
	observe(storage.GetInodesUsed(), nodeStorageAttrs(nodeID, storage.GetTarget(), "inodes_used")...)
	observe(storage.GetInodesAvailable(), nodeStorageAttrs(nodeID, storage.GetTarget(), "inodes_available")...)
	observe(storage.GetSystemReserveBytes(), nodeStorageAttrs(nodeID, storage.GetTarget(), "system_reserve")...)
	observe(storage.GetReservedBytes(), nodeStorageAttrs(nodeID, storage.GetTarget(), "reserved")...)
	observe(storage.GetAllocatableBytes(), nodeStorageAttrs(nodeID, storage.GetTarget(), "allocatable")...)
	observe(storage.GetActiveReservations(), nodeStorageAttrs(nodeID, storage.GetTarget(), "active_reservations")...)
}

func observeNodeBPFNet(observe sdkobs.Int64GaugeObserver, nodeID string, bpfnet *nodev1.BpfNetSummary) {
	if bpfnet == nil {
		return
	}
	observe(boolToInt64(bpfnet.GetEnabled()), nodeBPFNetAttrs(nodeID, "enabled")...)
	observe(boolToInt64(bpfnet.GetReady()), nodeBPFNetAttrs(nodeID, "ready")...)
	observe(boolToInt64(bpfnet.GetNeedsSnatFallback()), nodeBPFNetAttrs(nodeID, "snat_fallback")...)
	observe(boolToInt64(bpfnet.GetNeedsFullDnatFallback()), nodeBPFNetAttrs(nodeID, "full_dnat_fallback")...)
	observe(boolToInt64(bpfnet.GetNeedsLocalhostCompat()), nodeBPFNetAttrs(nodeID, "localhost_compat")...)
}

func nodeResourceAttrs(nodeID, resource, state string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(sdkobs.AttrNodeID, nodeID),
		attribute.String(sdkobs.AttrResource, resource),
		attribute.String(sdkobs.AttrState, state),
	}
}

func nodeStorageAttrs(nodeID, storage, state string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(sdkobs.AttrNodeID, nodeID),
		attribute.String(sdkobs.AttrStorage, storage),
		attribute.String(sdkobs.AttrState, state),
	}
}

func nodeBPFNetAttrs(nodeID, state string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(sdkobs.AttrNodeID, nodeID),
		attribute.String(sdkobs.AttrState, state),
	}
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
