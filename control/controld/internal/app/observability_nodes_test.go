package app

import (
	"context"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestObserveNodeStorageReportsReadyNodeTargets(t *testing.T) {
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		{
			NodeID:    "node-a",
			Lifecycle: nodekernel.LifecycleActive,
			UpdatedAt: now,
			Summary: readyNodeSummary(now, []*nodev1.NodeStorageSummary{
				{
					Target:          "axnoded_state",
					CapacityBytes:   1000,
					UsedBytes:       400,
					AvailableBytes:  600,
					InodesTotal:     100,
					InodesUsed:      30,
					InodesAvailable: 70,
					Collected:       true,
				},
				{
					Target:    "image_cache",
					Collected: false,
					Error:     "statfs /var/lib/imagemgr: no such file or directory",
				},
			}),
		},
		{
			NodeID:    "node-stale",
			Lifecycle: nodekernel.LifecycleActive,
			UpdatedAt: now.Add(-time.Minute),
			Summary: readyNodeSummary(now.Add(-time.Minute), []*nodev1.NodeStorageSummary{
				{Target: "volume_data", CapacityBytes: 999, Collected: true},
			}),
		},
	})
	app := &App{
		registry:                 registry,
		now:                      func() time.Time { return now },
		heartbeatFreshnessWindow: 15 * time.Second,
		summaryFreshnessWindow:   15 * time.Second,
	}

	got := collectStorageMetrics(t, app.observeNodeStorage)

	if value, ok := got[storageMetricKey{nodeID: "node-a", storage: "axnoded_state", state: "capacity"}]; !ok || value != 1000 {
		t.Fatalf("capacity metric = %d/%v, want 1000/true", value, ok)
	}
	if value, ok := got[storageMetricKey{nodeID: "node-a", storage: "axnoded_state", state: "used"}]; !ok || value != 400 {
		t.Fatalf("used metric = %d/%v, want 400/true", value, ok)
	}
	if value, ok := got[storageMetricKey{nodeID: "node-a", storage: "axnoded_state", state: "inodes_available"}]; !ok || value != 70 {
		t.Fatalf("inodes_available metric = %d/%v, want 70/true", value, ok)
	}
	if value, ok := got[storageMetricKey{nodeID: "node-a", storage: "axnoded_state", state: "collected"}]; !ok || value != 1 {
		t.Fatalf("collected metric = %d/%v, want 1/true", value, ok)
	}
	if value, ok := got[storageMetricKey{nodeID: "node-a", storage: "image_cache", state: "collected"}]; !ok || value != 0 {
		t.Fatalf("failed target collected metric = %d/%v, want 0/true", value, ok)
	}
	if _, ok := got[storageMetricKey{nodeID: "node-a", storage: "image_cache", state: "capacity"}]; ok {
		t.Fatalf("failed target capacity metric was reported: %#v", got)
	}
	if _, ok := got[storageMetricKey{nodeID: "node-stale", storage: "volume_data", state: "capacity"}]; ok {
		t.Fatalf("stale node storage metric was reported: %#v", got)
	}
}

func TestObserveNodeBPFNetReportsReadyNodeState(t *testing.T) {
	now := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		{
			NodeID:    "node-a",
			Lifecycle: nodekernel.LifecycleActive,
			UpdatedAt: now,
			Summary: readyNodeSummaryWithBPFNet(now, &nodev1.BpfNetSummary{
				Enabled:               true,
				Ready:                 true,
				NeedsSnatFallback:     false,
				NeedsFullDnatFallback: false,
				NeedsLocalhostCompat:  true,
			}),
		},
		{
			NodeID:    "node-stale",
			Lifecycle: nodekernel.LifecycleActive,
			UpdatedAt: now.Add(-time.Minute),
			Summary: readyNodeSummaryWithBPFNet(now.Add(-time.Minute), &nodev1.BpfNetSummary{
				Enabled:               true,
				NeedsFullDnatFallback: true,
			}),
		},
	})
	app := &App{
		registry:                 registry,
		now:                      func() time.Time { return now },
		heartbeatFreshnessWindow: 15 * time.Second,
		summaryFreshnessWindow:   15 * time.Second,
	}

	got := collectStateMetrics(t, app.observeNodeBPFNet)

	if value, ok := got[stateMetricKey{nodeID: "node-a", state: "enabled"}]; !ok || value != 1 {
		t.Fatalf("enabled metric = %d/%v, want 1/true", value, ok)
	}
	if value, ok := got[stateMetricKey{nodeID: "node-a", state: "ready"}]; !ok || value != 1 {
		t.Fatalf("ready metric = %d/%v, want 1/true", value, ok)
	}
	if value, ok := got[stateMetricKey{nodeID: "node-a", state: "snat_fallback"}]; !ok || value != 0 {
		t.Fatalf("snat fallback metric = %d/%v, want 0/true", value, ok)
	}
	if value, ok := got[stateMetricKey{nodeID: "node-a", state: "full_dnat_fallback"}]; !ok || value != 0 {
		t.Fatalf("full fallback metric = %d/%v, want 0/true", value, ok)
	}
	if value, ok := got[stateMetricKey{nodeID: "node-a", state: "localhost_compat"}]; !ok || value != 1 {
		t.Fatalf("localhost compat metric = %d/%v, want 1/true", value, ok)
	}
	if _, ok := got[stateMetricKey{nodeID: "node-stale", state: "full_dnat_fallback"}]; ok {
		t.Fatalf("stale node bpfnet metric was reported: %#v", got)
	}
}

func readyNodeSummary(collectedAt time.Time, storage []*nodev1.NodeStorageSummary) *nodev1.NodeSummary {
	return &nodev1.NodeSummary{
		CollectedAt: timestamppb.New(collectedAt),
		Components: &nodev1.ComponentsSummary{
			Axnoded: &nodev1.AxnodedSummary{
				State: nodev1.ComponentState_COMPONENT_STATE_READY,
				Ready: true,
			},
		},
		Storage: storage,
	}
}

func readyNodeSummaryWithBPFNet(collectedAt time.Time, bpfnet *nodev1.BpfNetSummary) *nodev1.NodeSummary {
	return &nodev1.NodeSummary{
		CollectedAt: timestamppb.New(collectedAt),
		Components: &nodev1.ComponentsSummary{
			Axnoded: &nodev1.AxnodedSummary{
				State: nodev1.ComponentState_COMPONENT_STATE_READY,
				Ready: true,
			},
			Bpfnet: bpfnet,
		},
	}
}

type storageMetricKey struct {
	nodeID  string
	storage string
	state   string
}

type stateMetricKey struct {
	nodeID string
	state  string
}

func collectStorageMetrics(t *testing.T, callback sdkobs.Int64GaugeCallback) map[storageMetricKey]int64 {
	t.Helper()
	out := map[storageMetricKey]int64{}
	if err := callback(context.Background(), func(value int64, attrs ...attribute.KeyValue) {
		var key storageMetricKey
		for _, attr := range attrs {
			switch string(attr.Key) {
			case sdkobs.AttrNodeID:
				key.nodeID = attr.Value.AsString()
			case sdkobs.AttrStorage:
				key.storage = attr.Value.AsString()
			case sdkobs.AttrState:
				key.state = attr.Value.AsString()
			}
		}
		out[key] = value
	}); err != nil {
		t.Fatalf("observe storage metrics: %v", err)
	}
	return out
}

func collectStateMetrics(t *testing.T, callback sdkobs.Int64GaugeCallback) map[stateMetricKey]int64 {
	t.Helper()
	out := map[stateMetricKey]int64{}
	if err := callback(context.Background(), func(value int64, attrs ...attribute.KeyValue) {
		var key stateMetricKey
		for _, attr := range attrs {
			switch string(attr.Key) {
			case sdkobs.AttrNodeID:
				key.nodeID = attr.Value.AsString()
			case sdkobs.AttrState:
				key.state = attr.Value.AsString()
			}
		}
		out[key] = value
	}); err != nil {
		t.Fatalf("observe state metrics: %v", err)
	}
	return out
}
