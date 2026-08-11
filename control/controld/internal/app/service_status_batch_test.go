package app

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestPostgresConcurrentServiceStatusBatchesProjectOncePerNode(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 7, 10, 14, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	for i := 0; i < 6; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		registerReadyNode(t, app, nodeID, now)
		summary := controldtest.ReadySummary(now)
		controldtest.SetReadySummaryMemory(summary, 64<<30)
		if _, err := app.NodeV1Handler().ReportNode(context.Background(), &nodev1.ReportNodeRequest{
			NodeID:        nodeID,
			Runtimes:      []string{"runsc"},
			NodeTarget:    "127.0.0.1:25000",
			NodeAuthToken: "test-node-token",
			Summary:       summary,
		}); err != nil {
			t.Fatalf("ReportNode(%s) error = %v", nodeID, err)
		}
	}
	env := createDefaultEnvironment(t, app)
	created, err := app.PublicV1Handler().CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      36,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if err := app.serviceReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	app.pendingServiceReconcile.Take()
	admitted, err := app.PublicV1Handler().GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: created.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after admission) error = %v", err)
	}
	if got := len(admitted.GetService().GetAllocationIds()); got != 36 {
		t.Fatalf("admitted allocations = %d, want 36", got)
	}
	allocations, err := app.servicePG.CurrentServiceAllocations(context.Background(), admitted.GetService().GetID())
	if err != nil {
		t.Fatalf("CurrentServiceAllocations() error = %v", err)
	}
	byNode := make(map[string][]*nodev1.AllocationStatusObservation)
	for _, allocation := range allocations {
		byNode[allocation.NodeID] = append(byNode[allocation.NodeID], &nodev1.AllocationStatusObservation{
			AllocationID: allocation.AllocationID,
			Attempt:      allocation.Attempt,
			Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Ready:        true,
		})
	}
	if len(byNode) != 6 {
		t.Fatalf("allocation node groups = %d, want 6", len(byNode))
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(byNode))
	for nodeID, observations := range byNode {
		nodeID, observations := nodeID, observations
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := app.NodeV1Handler().BatchReportAllocationStatus(context.Background(), &nodev1.BatchReportAllocationStatusRequest{
				NodeID:        nodeID,
				NodeAuthToken: "test-node-token",
				Observations:  observations,
			})
			if err != nil {
				errs <- fmt.Errorf("report %s status batch: %w", nodeID, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	item := app.pendingServiceReconcile.Take()
	if item.FullSweep || item.ServiceID != "" {
		t.Fatalf("service reconcile item = %#v, want no controller work for initial readiness projection", item)
	}

	ready, err := app.PublicV1Handler().GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: admitted.GetService().GetID()})
	if err != nil {
		t.Fatalf("GetService(after status batches) error = %v", err)
	}
	if ready.GetService().GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY || ready.GetService().GetReadyReplicas() != 36 {
		t.Fatalf("service after status batches = status:%s ready:%d, want READY/36", ready.GetService().GetStatus(), ready.GetService().GetReadyReplicas())
	}
	wantVersion := admitted.GetService().GetVersion() + int64(len(byNode))
	if ready.GetService().GetVersion() != wantVersion {
		t.Fatalf("service version after status batches = %d, want %d", ready.GetService().GetVersion(), wantVersion)
	}
}
