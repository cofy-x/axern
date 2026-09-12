package app

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
)

func TestNodeHealthUsesOnlyActiveFreshNodeSummaries(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	ready := controldtest.ReadySummary(now)
	stale := controldtest.ReadySummary(now.Add(-time.Minute))
	retired := controldtest.ReadySummary(now.Add(-time.Minute))
	store := &fakeNodeHealthStore{records: []*nodekernel.Record{
		{NodeID: "ready", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now, Summary: ready},
		{NodeID: "stale", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now.Add(-time.Minute), Summary: stale},
		{NodeID: "retired", Lifecycle: nodekernel.LifecycleRetired, UpdatedAt: now.Add(-time.Minute), Summary: retired},
	}}
	fleet, err := (nodeHealthSource{store: store, heartbeatWindow: 15 * time.Second, summaryWindow: 15 * time.Second}).NodeHealth(context.Background(), now)
	if err != nil {
		t.Fatalf("NodeHealth() error = %v", err)
	}
	if fleet.ActiveNodes != 2 || fleet.ReadyNodes != 1 || fleet.StaleHeartbeatNodes != 1 || fleet.StaleSummaryNodes != 1 {
		t.Fatalf("fleet health = %+v", fleet)
	}
}

type fakeNodeHealthStore struct{ records []*nodekernel.Record }

func (f *fakeNodeHealthStore) ListNodes(context.Context, adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	return f.records, nil
}
