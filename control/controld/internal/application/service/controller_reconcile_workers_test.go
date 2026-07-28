package appservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestCreateWakesReconcilerOnlyAfterDurableWrite(t *testing.T) {
	notifications := 0
	var notifiedServiceIDs []string
	store := &fakeReconcileServiceStore{createResult: &servicev1.Service{ID: "svc-a"}}
	controller := &controller{
		store: store,
		notifyReconcile: func(serviceIDs ...string) {
			notifications++
			notifiedServiceIDs = append(notifiedServiceIDs, serviceIDs...)
		},
	}

	if _, err := controller.Create(context.Background(), servicekernel.CreateParams{}, time.Now()); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if notifications != 1 {
		t.Fatalf("reconcile notifications = %d, want 1", notifications)
	}
	if len(notifiedServiceIDs) != 1 || notifiedServiceIDs[0] != "svc-a" {
		t.Fatalf("notified service IDs = %#v, want [svc-a]", notifiedServiceIDs)
	}

	store.createResult = nil
	store.createErr = errors.New("write failed")
	if _, err := controller.Create(context.Background(), servicekernel.CreateParams{}, time.Now()); err == nil {
		t.Fatal("Create() error = nil, want write failure")
	}
	if notifications != 1 {
		t.Fatalf("reconcile notifications after failed write = %d, want 1", notifications)
	}
}

func TestReconcileServicesTargetsUniqueCurrentServices(t *testing.T) {
	statuses := &fakeReconcileStatusStore{}
	controller := &controller{
		store: &fakeReconcileServiceStore{getByID: map[string]*servicev1.Service{
			"svc-a": deletingService("svc-a"),
			"svc-b": deletingService("svc-b"),
		}},
		allocations:          &fakeReconcileAllocationStore{},
		environments:         &fakeReconcileEnvironmentReader{},
		statuses:             statuses,
		reconcileConcurrency: 1,
	}

	if err := controller.ReconcileServices(context.Background(), []string{"svc-b", "svc-a", "svc-b", "", "missing"}, time.Now()); err != nil {
		t.Fatalf("ReconcileServices() error = %v", err)
	}
	if got := statuses.syncedServiceIDs; len(got) != 2 || got[0] != "svc-a" || got[1] != "svc-b" {
		t.Fatalf("synced service IDs = %#v, want [svc-a svc-b]", got)
	}
}

func TestReconcileServicesUsesBoundedParallelWorkers(t *testing.T) {
	reader := &blockingEnvironmentReader{
		started: make(chan struct{}, 4),
		release: make(chan struct{}),
	}
	controller := &controller{
		allocations:          &fakeReconcileAllocationStore{},
		environments:         reader,
		statuses:             &concurrentReconcileStatusStore{},
		reconcileConcurrency: 2,
	}
	services := []*servicev1.Service{
		activeService("svc-a"),
		activeService("svc-b"),
		activeService("svc-c"),
		activeService("svc-d"),
	}
	done := make(chan error, 1)
	go func() {
		done <- controller.reconcileServices(context.Background(), services, time.Now())
	}()

	for range 2 {
		select {
		case <-reader.started:
		case <-time.After(time.Second):
			t.Fatal("two service reconciles did not start concurrently")
		}
	}
	select {
	case <-reader.started:
		t.Fatal("service reconcile exceeded configured concurrency")
	case <-time.After(25 * time.Millisecond):
	}
	close(reader.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reconcileServices() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("service reconcile workers did not finish")
	}
}

func TestReconcileAllocationBatchDrainsPastInitialWorkerBudget(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	items := []allocationkernel.ReconcileItem{
		{AllocationID: "alloc-a", OwnerID: "svc-a", EnvironmentID: "env-a", Reason: allocationkernel.ReconcileReasonCreate, NodeID: "node-a", NodeTarget: "node-a:24010"},
		{AllocationID: "alloc-b", OwnerID: "svc-b", EnvironmentID: "env-a", Reason: allocationkernel.ReconcileReasonCreate, NodeID: "node-b", NodeTarget: "node-b:24010"},
		{AllocationID: "alloc-c", OwnerID: "svc-c", EnvironmentID: "env-a", Reason: allocationkernel.ReconcileReasonCreate, NodeID: "node-c", NodeTarget: "node-c:24010"},
	}
	reconcile := &fakeServiceAllocationReconcileStore{items: append([]allocationkernel.ReconcileItem(nil), items[:1]...)}
	services := map[string]*servicev1.Service{}
	for _, item := range items {
		services[item.OwnerID] = &servicev1.Service{
			ID:            item.OwnerID,
			EnvironmentID: item.EnvironmentID,
			AllocationIds: []string{item.AllocationID},
		}
	}
	lifecycle := &fakeServiceAllocationLifecycle{
		createStarted: make(chan struct{}, len(items)),
		createRelease: make(chan struct{}),
	}
	controller := &controller{
		store:                        &fakeReconcileServiceStore{getByID: services},
		allocations:                  &fakeReconcileAllocationStore{},
		environments:                 &fakeReconcileEnvironmentReader{},
		reconcile:                    reconcile,
		lifecycle:                    lifecycle,
		allocationGlobalConcurrency:  2,
		allocationPerNodeConcurrency: 2,
	}

	done := make(chan struct {
		processed int
		err       error
	}, 1)
	go func() {
		processed, err := controller.ReconcileAllocationBatch(context.Background(), now)
		done <- struct {
			processed int
			err       error
		}{processed: processed, err: err}
	}()
	select {
	case <-lifecycle.createStarted:
	case <-time.After(time.Second):
		t.Fatal("initial allocation did not start")
	}
	reconcile.mu.Lock()
	reconcile.items = append(reconcile.items, items[1:]...)
	reconcile.mu.Unlock()
	select {
	case <-lifecycle.createStarted:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not refill an available worker slot")
	}
	select {
	case <-lifecycle.createStarted:
		t.Fatal("dispatcher exceeded the configured allocation concurrency")
	case <-time.After(25 * time.Millisecond):
	}
	close(lifecycle.createRelease)
	result := <-done
	if result.err != nil {
		t.Fatalf("ReconcileAllocationBatch() error = %v", result.err)
	}
	if result.processed != 3 {
		t.Fatalf("processed = %d, want 3", result.processed)
	}
	if len(lifecycle.createRequests) != 3 {
		t.Fatalf("create requests = %d, want 3", len(lifecycle.createRequests))
	}
	if len(reconcile.items) != 0 {
		t.Fatalf("remaining reconcile items = %#v, want none", reconcile.items)
	}
}

func TestReconcileAllocationBatchLimitsEachNodeWithoutBlockingOtherNodes(t *testing.T) {
	now := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)
	items := []allocationkernel.ReconcileItem{
		{AllocationID: "alloc-a1", OwnerID: "svc-a1", EnvironmentID: "env-a", Reason: allocationkernel.ReconcileReasonCreate, NodeID: "node-a", NodeTarget: "node-a:24010"},
		{AllocationID: "alloc-a2", OwnerID: "svc-a2", EnvironmentID: "env-a", Reason: allocationkernel.ReconcileReasonCreate, NodeID: "node-a", NodeTarget: "node-a:24010"},
		{AllocationID: "alloc-b1", OwnerID: "svc-b1", EnvironmentID: "env-a", Reason: allocationkernel.ReconcileReasonCreate, NodeID: "node-b", NodeTarget: "node-b:24010"},
	}
	services := make(map[string]*servicev1.Service, len(items))
	for _, item := range items {
		services[item.OwnerID] = &servicev1.Service{ID: item.OwnerID, EnvironmentID: item.EnvironmentID, AllocationIds: []string{item.AllocationID}}
	}
	lifecycle := &fakeServiceAllocationLifecycle{createStarted: make(chan struct{}, len(items)), createRelease: make(chan struct{})}
	controller := &controller{
		store:                        &fakeReconcileServiceStore{getByID: services},
		allocations:                  &fakeReconcileAllocationStore{},
		environments:                 &fakeReconcileEnvironmentReader{},
		reconcile:                    &fakeServiceAllocationReconcileStore{items: items},
		lifecycle:                    lifecycle,
		allocationGlobalConcurrency:  3,
		allocationPerNodeConcurrency: 1,
	}

	done := make(chan error, 1)
	go func() {
		_, err := controller.ReconcileAllocationBatch(context.Background(), now)
		done <- err
	}()
	for range 2 {
		select {
		case <-lifecycle.createStarted:
		case <-time.After(time.Second):
			t.Fatal("allocations on distinct nodes did not start concurrently")
		}
	}
	select {
	case <-lifecycle.createStarted:
		t.Fatal("dispatcher exceeded per-node allocation concurrency")
	case <-time.After(25 * time.Millisecond):
	}
	close(lifecycle.createRelease)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ReconcileAllocationBatch() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("allocation dispatcher did not drain pending node work")
	}
}

func deletedService(id string) *servicev1.Service {
	return &servicev1.Service{
		ID:            id,
		EnvironmentID: "env-a",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
		},
	}
}

func deletingService(id string) *servicev1.Service {
	return &servicev1.Service{
		ID:            id,
		EnvironmentID: "env-a",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
		},
	}
}

func activeService(id string) *servicev1.Service {
	return &servicev1.Service{
		ID:            id,
		EnvironmentID: "env-a",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
	}
}

type blockingEnvironmentReader struct {
	started chan struct{}
	release chan struct{}
}

func (r *blockingEnvironmentReader) GetEnvironment(context.Context, string) (*environmentv1.Environment, error) {
	r.started <- struct{}{}
	<-r.release
	return &environmentv1.Environment{ID: "env-a"}, nil
}

type concurrentReconcileStatusStore struct {
	mu sync.Mutex
}

func (s *concurrentReconcileStatusStore) SyncObservedStatus(_ context.Context, serviceID string, _ time.Time) (*servicev1.Service, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return deletedService(serviceID), nil
}

func (s *concurrentReconcileStatusStore) UpdateDeletionStatus(context.Context, string, *servicev1.ServiceDeletionStatus, time.Time) (*servicev1.Service, error) {
	return nil, nil
}

func (s *concurrentReconcileStatusStore) UpdateStatus(context.Context, string, servicev1.ServiceStatus, string, time.Time) (*servicev1.Service, error) {
	panic("unexpected UpdateStatus call")
}

func (s *concurrentReconcileStatusStore) UpdateAutoscalingStatus(context.Context, string, *servicev1.ServiceAutoscalingStatus, time.Time) (*servicev1.Service, error) {
	panic("unexpected UpdateAutoscalingStatus call")
}
