package nodekernel

import (
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRegisterIdempotentUpsert(t *testing.T) {
	registry := NewRegistry()
	now := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)

	registry.Report("node-a", "127.0.0.1:25000", nil, now)
	registry.Report("node-a", "127.0.0.1:25000", nil, now)

	nodes := registry.DebugNodes(now, 15*time.Second, 15*time.Second)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
}

func TestRegistryRetirementIsMonotonic(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry()
	registry.Report("node-a", "node-a:25000", nil, now)
	registry.SyncLifecycle("node-a", LifecycleRetired, now.Add(time.Minute), "host removed")

	registry.Report("node-a", "node-a:25001", nil, now.Add(2*time.Minute))
	registry.SyncLifecycle("node-a", LifecycleActive, time.Time{}, "")
	record, ok := registry.Get("node-a")
	if !ok || record.Lifecycle != LifecycleRetired || record.RetiredReason != "host removed" {
		t.Fatalf("record = %+v, want retired", record)
	}
}

func TestReportUsesSummaryCollectedAtAsSingleTimestamp(t *testing.T) {
	registry := NewRegistry()
	now := time.Date(2026, 4, 21, 11, 0, 0, 0, time.UTC)
	collectedAt := now.Add(-7 * time.Second)

	registry.Report("node-b", "127.0.0.1:25001", readySummary(collectedAt), now)

	nodes := registry.DebugNodes(now, 15*time.Second, 15*time.Second)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if got := nodes[0].CollectedAt; !got.Equal(collectedAt) {
		t.Fatalf("collected_at = %v, want %v", got, collectedAt)
	}
	if got := nodes[0].SummaryAgeSecs; got != 7 {
		t.Fatalf("summary_age_secs = %d, want 7", got)
	}
}

func TestDebugNodesClassifiesFreshness(t *testing.T) {
	registry := NewRegistry()
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	registry.Report("fresh-node", "127.0.0.1:25002", readySummary(base.Add(20*time.Second)), base.Add(20*time.Second))
	registry.Report("stale-summary", "127.0.0.1:25003", readySummary(base), base.Add(20*time.Second))

	nodes := registry.DebugNodes(base.Add(20*time.Second), 15*time.Second, 15*time.Second)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].NodeID != "fresh-node" || !nodes[0].HeartbeatFresh || !nodes[0].SummaryFresh {
		t.Fatalf("unexpected fresh-node debug state: %#v", nodes[0])
	}
	if nodes[0].FreshnessState != "fresh" {
		t.Fatalf("fresh-node freshness_state = %q, want fresh", nodes[0].FreshnessState)
	}
	if nodes[1].NodeID != "stale-summary" || !nodes[1].HeartbeatFresh || nodes[1].SummaryFresh {
		t.Fatalf("unexpected stale-summary debug state: %#v", nodes[1])
	}
	if nodes[1].FreshnessState != "stale_summary" {
		t.Fatalf("stale-summary freshness_state = %q, want stale_summary", nodes[1].FreshnessState)
	}
}

func readySummary(collectedAt time.Time) *nodev1.NodeSummary {
	return &nodev1.NodeSummary{
		CollectedAt: timestamppb.New(collectedAt),
		Components: &nodev1.ComponentsSummary{
			Axnoded: &nodev1.AxnodedSummary{
				State: nodev1.ComponentState_COMPONENT_STATE_READY,
				Ready: true,
			},
		},
	}
}

func TestRegistryRevocationBeforeFirstReportIsMonotonic(t *testing.T) {
	registry := NewRegistry()
	registry.SyncLifecycle("node-a", LifecycleRevoked, time.Time{}, "")
	registry.Report("node-a", "node-a:25000", nil, time.Now())
	registry.SyncLifecycle("node-a", LifecycleActive, time.Time{}, "")
	record, _ := registry.Get("node-a")
	if record.Active() || record.Lifecycle != LifecycleRevoked {
		t.Fatal("late report revived revoked identity")
	}
	registry.SyncLifecycle("node-a", LifecycleRetired, time.Now(), "cleaned")
	registry.SyncLifecycle("node-a", LifecycleRevoked, time.Time{}, "")
	record, _ = registry.Get("node-a")
	if record.Lifecycle != LifecycleRetired {
		t.Fatal("revocation revived retired identity")
	}
}
