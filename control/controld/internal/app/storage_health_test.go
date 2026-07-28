package app

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestNodeHealthUsesOnlyActiveFreshNodeSummaries(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	ready := controldtest.ReadySummary(now)
	ready.Components.Volumed = &nodev1.VolumedSummary{State: nodev1.ComponentState_COMPONENT_STATE_READY, PublishedVolumeCount: 2}
	stale := controldtest.ReadySummary(now.Add(-time.Minute))
	stale.Components.Volumed = &nodev1.VolumedSummary{State: nodev1.ComponentState_COMPONENT_STATE_ERROR, PublishedVolumeCount: 9, LastReconcileError: "old error"}
	retired := controldtest.ReadySummary(now.Add(-time.Minute))
	store := &fakeNodeHealthStore{records: []*nodekernel.Record{
		{NodeID: "ready", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now, Summary: ready},
		{NodeID: "stale", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now.Add(-time.Minute), Summary: stale},
		{NodeID: "retired", Lifecycle: nodekernel.LifecycleRetired, UpdatedAt: now.Add(-time.Minute), Summary: retired},
	}}
	fleet, volumes, err := (nodeHealthSource{store: store, heartbeatWindow: 15 * time.Second, summaryWindow: 15 * time.Second}).NodeHealth(context.Background(), now)
	if err != nil {
		t.Fatalf("NodeHealth() error = %v", err)
	}
	if fleet.ActiveNodes != 2 || fleet.ReadyNodes != 1 || fleet.StaleHeartbeatNodes != 1 || fleet.StaleSummaryNodes != 1 {
		t.Fatalf("fleet health = %+v", fleet)
	}
	if volumes.PublishedVolumes != 2 || volumes.UnhealthyNodes != 0 || volumes.Error != "" {
		t.Fatalf("volume health = %+v", volumes)
	}
}

type fakeNodeHealthStore struct{ records []*nodekernel.Record }

func (f *fakeNodeHealthStore) ListNodes(context.Context, adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	return f.records, nil
}
