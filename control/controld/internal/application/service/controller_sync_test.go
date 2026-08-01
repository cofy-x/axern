package appservice

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func releasingDeletionStatus() *servicev1.ServiceDeletionStatus {
	return &servicev1.ServiceDeletionStatus{
		Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
		VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
	}
}

func TestReconcilePendingDeletesServicesWithoutReadingEnvironment(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 30, 0, 0, time.UTC)
	store := &fakeReconcileServiceStore{services: []*servicev1.Service{
		{ID: "svc-bad", EnvironmentID: "missing", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING, DeletionStatus: releasingDeletionStatus()},
		{ID: "svc-good", EnvironmentID: "env-good", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING, DeletionStatus: releasingDeletionStatus()},
	}}
	environments := &fakeReconcileEnvironmentReader{errByID: map[string]error{"missing": errors.New("environment unavailable")}}
	statuses := &fakeReconcileStatusStore{}

	err := (&controller{
		store:        store,
		allocations:  &fakeReconcileAllocationStore{},
		statuses:     statuses,
		environments: environments,
	}).ReconcilePending(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	got := slices.Clone(statuses.syncedServiceIDs)
	slices.Sort(got)
	if want := []string{"svc-bad", "svc-good"}; !slices.Equal(got, want) {
		t.Fatalf("synced service ids = %#v, want %#v", got, want)
	}
	if environments.callCount() != 0 {
		t.Fatalf("environment reads = %d, want 0 for deleted services", environments.callCount())
	}
}

func TestAllocationLifecycleFailureDoesNotBlockServiceReconcile(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 45, 0, 0, time.UTC)
	reconcile := &fakeServiceAllocationReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:      "alloc-a",
			OwnerID:           "svc-a",
			Reason:            allocationkernel.ReconcileReasonDelete,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			ReconcileAttempts: 1,
		}},
		scheduleErr: errors.New("database unavailable"),
	}
	statuses := &fakeReconcileStatusStore{}

	controller := &controller{
		store: &fakeReconcileServiceStore{services: []*servicev1.Service{
			{ID: "svc-good", EnvironmentID: "env-good", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING, DeletionStatus: releasingDeletionStatus()},
		}},
		allocations:  &fakeReconcileAllocationStore{},
		statuses:     statuses,
		environments: &fakeReconcileEnvironmentReader{},
		reconcile:    reconcile,
		lifecycle:    &fakeServiceAllocationLifecycle{deleteErr: errors.New("node unavailable")},
	}
	_, err := controller.ReconcileAllocationBatch(context.Background(), now)
	if err == nil {
		t.Fatal("ReconcileAllocationBatch() error = nil, want schedule error")
	}
	if !errors.Is(err, reconcile.scheduleErr) {
		t.Fatalf("ReconcilePending() error = %v, want schedule error %v", err, reconcile.scheduleErr)
	}
	if err := controller.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if got, want := statuses.syncedServiceIDs, []string{"svc-good"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("synced service ids = %#v, want %#v", got, want)
	}
}

func TestSyncTreatsMissingServiceDuringStatusSyncAsTerminal(t *testing.T) {
	now := time.Date(2026, 5, 9, 13, 0, 0, 0, time.UTC)
	statuses := &fakeReconcileStatusStore{missingOnSync: map[string]bool{"svc-purged": true}}

	next, err := (&controller{
		allocations:  &fakeReconcileAllocationStore{},
		statuses:     statuses,
		environments: &fakeReconcileEnvironmentReader{},
	}).Sync(context.Background(), &servicev1.Service{
		ID:            "svc-purged",
		EnvironmentID: "env-a",
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
	}, now)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if next != nil {
		t.Fatalf("Sync() service = %v, want nil after concurrent purge", next)
	}
}

func TestVolumeReclaimDispatcherExecutesGenericTasks(t *testing.T) {
	reclaim := &privatestoragev1.VolumeReclaim{ClaimID: "claim-direct", NodeID: "node-a", BackendHandle: "claim-direct", LeaseToken: "lease-a"}
	storage := &fakeStorageCoordinator{claimReclaims: []*privatestoragev1.VolumeReclaim{reclaim}}
	lifecycle := &fakeServiceAllocationLifecycle{}
	controller := &controller{
		storage:    storage,
		lifecycle:  lifecycle,
		nodeTarget: func(nodeID string) (string, bool) { return nodeID + ":24010", true },
	}
	completed := make(chan volumeReclaimCompletion, 1)
	controller.executeVolumeReclaim(reclaim, completed)
	<-completed
	if lifecycle.volumeDeleteCalls != 1 || len(storage.reports) != 1 || !storage.reports[0].succeeded {
		t.Fatalf("reclaim calls=%d reports=%#v", lifecycle.volumeDeleteCalls, storage.reports)
	}
}

func TestVolumeReclaimDispatcherDoesNotLetSlowNodeBlockAnotherNode(t *testing.T) {
	storage := &fakeStorageCoordinator{claimReclaims: []*privatestoragev1.VolumeReclaim{
		{ClaimID: "claim-a", NodeID: "node-a", LeaseOwner: "owner", LeaseToken: "token-a", LeaseGeneration: 1},
		{ClaimID: "claim-b", NodeID: "node-b", LeaseOwner: "owner", LeaseToken: "token-b", LeaseGeneration: 1},
	}}
	lifecycle := &blockingVolumeLifecycle{
		fakeServiceAllocationLifecycle: &fakeServiceAllocationLifecycle{},
		started:                        make(chan string, 2),
		release:                        make(chan struct{}),
	}
	controller := &controller{
		storage: storage, lifecycle: lifecycle,
		nodeTarget: func(nodeID string) (string, bool) { return nodeID + ":24010", true },
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		controller.RunVolumeReclaimDispatcher(ctx, "owner", 2, 1)
		close(done)
	}()
	seen := map[string]bool{}
	for range 2 {
		select {
		case nodeID := <-lifecycle.started:
			seen[nodeID] = true
		case <-time.After(2 * time.Second):
			t.Fatal("dispatcher did not start independent node reclaim")
		}
	}
	if !seen["node-a"] || !seen["node-b"] {
		t.Fatalf("started nodes=%v, want node-a and node-b", seen)
	}
	cancel()
	close(lifecycle.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not drain on cancellation")
	}
}

func TestSyncDeletedWaitsForPhysicalVolumeReclaimBeforeComplete(t *testing.T) {
	now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID: "svc-1", Namespace: "default", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE,
		},
	}
	storage := &fakeStorageCoordinator{deleteResponses: []*privatestoragev1.DeleteWorkloadVolumeClaimsResponse{
		{ClaimIds: []string{"claim-1"}},
		{Complete: true},
	}}
	lifecycle := &fakeServiceAllocationLifecycle{}
	statuses := &fakeReconcileStatusStore{service: service}
	controller := &controller{
		allocations: &fakeReconcileAllocationStore{history: []*servicekernel.AllocationRecord{{
			AllocationID: "alloc-1", NodeID: "node-a", NodeTarget: "node-a:24010",
			Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED,
		}}},
		statuses: statuses, storage: storage, lifecycle: lifecycle,
		nodeTarget: func(string) (string, bool) {
			return "node-a-current:24010", true
		},
	}
	next, err := controller.syncDeleted(context.Background(), service, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.GetDeletionStatus().GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES || lifecycle.volumeDeleteCalls != 0 || len(storage.reports) != 0 {
		t.Fatalf("first deletion next=%#v lifecycle=%d reports=%#v", next.GetDeletionStatus(), lifecycle.volumeDeleteCalls, storage.reports)
	}
	statuses.service = next
	next, err = controller.syncDeleted(context.Background(), next, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if next.GetDeletionStatus().GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE || next.GetDeletionStatus().GetCompletedAt() == nil {
		t.Fatalf("completed deletion = %#v", next.GetDeletionStatus())
	}
}

func TestSyncDeletedRetainDispositionReleasesClaimOwners(t *testing.T) {
	now := time.Date(2026, 8, 1, 3, 0, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID: "svc-1", Namespace: "default", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
		},
	}
	storage := &fakeStorageCoordinator{}
	controller := &controller{
		allocations: &fakeReconcileAllocationStore{history: []*servicekernel.AllocationRecord{{
			AllocationID: "alloc-1", NodeID: "node-a", NodeTarget: "node-a:24010",
			Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED,
		}}},
		statuses: &fakeReconcileStatusStore{service: service}, storage: storage,
	}
	next, err := controller.syncDeleted(context.Background(), service, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.GetDeletionStatus().GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
		t.Fatalf("retained deletion = %#v, want complete", next.GetDeletionStatus())
	}
	if storage.releaseCalls != 1 || len(storage.releasedServices) != 1 || storage.releasedServices[0] != "svc-1" {
		t.Fatalf("release calls=%d services=%v, want one release for svc-1", storage.releaseCalls, storage.releasedServices)
	}
}

func TestSyncDeletedRetainDispositionRetriesWhenReleaseFails(t *testing.T) {
	now := time.Date(2026, 8, 1, 3, 5, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID: "svc-1", Namespace: "default", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
		},
	}
	storage := &fakeStorageCoordinator{releaseErr: errors.New("storaged unavailable")}
	controller := &controller{
		allocations: &fakeReconcileAllocationStore{history: []*servicekernel.AllocationRecord{{
			AllocationID: "alloc-1", NodeID: "node-a", NodeTarget: "node-a:24010",
			Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED,
		}}},
		statuses: &fakeReconcileStatusStore{service: service}, storage: storage,
	}
	if _, err := controller.syncDeleted(context.Background(), service, now); err == nil {
		t.Fatal("syncDeleted() error = nil, want release failure to block completion")
	}
	if storage.releaseCalls != 1 {
		t.Fatalf("release calls=%d, want 1", storage.releaseCalls)
	}
}

func TestSyncDeletedMissingDeletionStatusFails(t *testing.T) {
	now := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID: "svc-1", Namespace: "default", Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
	}
	controller := &controller{
		allocations: &fakeReconcileAllocationStore{history: []*servicekernel.AllocationRecord{{
			AllocationID: "alloc-1", NodeID: "node-a", NodeTarget: "node-a:24010",
			Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED,
		}}},
		statuses: &fakeReconcileStatusStore{service: service},
	}
	if _, err := controller.syncDeleted(context.Background(), service, now); err == nil {
		t.Fatal("syncDeleted() error = nil, want missing deletion status failure")
	}
}

func TestReconcileAllocationCreateReservesStorageBeforeRetryingNodeCreate(t *testing.T) {
	now := time.Date(2026, 5, 9, 13, 15, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID:            "svc-a",
		Namespace:     "default",
		EnvironmentID: "env-a",
		Config: &commonv1.ExecutionConfig{
			VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name:   "data",
				Target: "/data",
			}},
		},
		AllocationIds: []string{"alloc-a"},
	}
	reconcile := &fakeServiceAllocationReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:  "alloc-a",
			OwnerID:       "svc-a",
			EnvironmentID: "env-a",
			Reason:        allocationkernel.ReconcileReasonCreate,
			NodeID:        "node-a",
			NodeTarget:    "node-a:24010",
		}},
	}
	volume := &privatestoragev1.ResolvedNodeVolume{ClaimID: "default/data", BindingID: "alloc-a/data", Target: "/data"}
	storage := &fakeStorageCoordinator{volumes: []*privatestoragev1.ResolvedNodeVolume{volume}}
	lifecycle := &fakeServiceAllocationLifecycle{}

	_, err := (&controller{
		store:        &fakeReconcileServiceStore{getByID: map[string]*servicev1.Service{"svc-a": service}},
		allocations:  &fakeReconcileAllocationStore{},
		environments: &fakeReconcileEnvironmentReader{},
		reconcile:    reconcile,
		lifecycle:    lifecycle,
		storage:      storage,
	}).ReconcileAllocationBatch(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if len(storage.reserveRequests) != 1 {
		t.Fatalf("reserve requests = %d, want 1", len(storage.reserveRequests))
	}
	if got := storage.reserveRequests[0]; got.AllocationID != "alloc-a" || got.NodeID != "node-a" || got.ServiceID != "svc-a" {
		t.Fatalf("reserve request = %#v, want alloc-a/node-a/svc-a", got)
	}
	if len(lifecycle.createRequests) != 1 || len(lifecycle.createRequests[0].NodeVolumes) != 1 {
		t.Fatalf("create requests = %#v, want one request with node volume", lifecycle.createRequests)
	}
	if lifecycle.createRequests[0].NodeVolumes[0].GetBindingID() != "alloc-a/data" {
		t.Fatalf("node volume = %#v, want alloc-a/data", lifecycle.createRequests[0].NodeVolumes[0])
	}
	if len(reconcile.completedCreates) != 1 || reconcile.completedCreates[0] != "alloc-a" {
		t.Fatalf("completed creates = %#v, want alloc-a", reconcile.completedCreates)
	}
}

func TestReconcileAllocationCreateClassifiesStorageReserveFailure(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID:            "svc-a",
		Namespace:     "default",
		EnvironmentID: "env-a",
		Config: &commonv1.ExecutionConfig{
			VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name:   "data",
				Target: "/data",
			}},
		},
		AllocationIds: []string{"alloc-a"},
	}
	reconcile := &fakeServiceAllocationReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:      "alloc-a",
			OwnerID:           "svc-a",
			EnvironmentID:     "env-a",
			Reason:            allocationkernel.ReconcileReasonCreate,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			ReconcileAttempts: allocationkernel.CreateRetryMaxAttempts - 1,
		}},
	}
	allocations := &fakeReconcileAllocationStore{}

	_, err := (&controller{
		store:        &fakeReconcileServiceStore{getByID: map[string]*servicev1.Service{"svc-a": service}},
		allocations:  allocations,
		environments: &fakeReconcileEnvironmentReader{},
		reconcile:    reconcile,
		lifecycle:    &fakeServiceAllocationLifecycle{},
		storage:      &fakeStorageCoordinator{reserveErr: errors.New("volume binding reserve requires claim, class, and mount")},
	}).ReconcileAllocationBatch(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if len(allocations.failedCreates) != 1 {
		t.Fatalf("failed creates = %#v, want one", allocations.failedCreates)
	}
	failed := allocations.failedCreates[0]
	if failed.allocationID != "alloc-a" || failed.message != "storage reserve failed: volume binding reserve requires claim, class, and mount" {
		t.Fatalf("failed create = %#v", failed)
	}
	if got := workloadkernel.ClassifyDiagnostic(commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, failed.message); got != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_STORAGE_RESERVE_ERROR {
		t.Fatalf("diagnostic = %v, want storage reserve", got)
	}
}

func TestReconcileAllocationCreateReportsVolumePublishFailureMessage(t *testing.T) {
	now := time.Date(2026, 5, 14, 10, 15, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID:            "svc-a",
		Namespace:     "default",
		EnvironmentID: "env-a",
		Config: &commonv1.ExecutionConfig{
			VolumeMounts: []*commonv1.ServiceVolumeMount{{
				Name:   "data",
				Target: "/data",
			}},
		},
		AllocationIds: []string{"alloc-a"},
	}
	reconcile := &fakeServiceAllocationReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:      "alloc-a",
			OwnerID:           "svc-a",
			EnvironmentID:     "env-a",
			Reason:            allocationkernel.ReconcileReasonCreate,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			ReconcileAttempts: allocationkernel.CreateRetryMaxAttempts - 1,
		}},
	}
	allocations := &fakeReconcileAllocationStore{}
	volume := &privatestoragev1.ResolvedNodeVolume{ClaimID: "default/data", BindingID: "alloc-a/data", Target: "/data"}
	storage := &fakeStorageCoordinator{volumes: []*privatestoragev1.ResolvedNodeVolume{volume}}

	_, err := (&controller{
		store:        &fakeReconcileServiceStore{getByID: map[string]*servicev1.Service{"svc-a": service}},
		allocations:  allocations,
		environments: &fakeReconcileEnvironmentReader{},
		reconcile:    reconcile,
		lifecycle:    &fakeServiceAllocationLifecycle{createErr: errors.New("volumed: volume does not support runtime class \"runsc\"")},
		storage:      storage,
	}).ReconcileAllocationBatch(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	wantMessage := "volume publish failed: volumed: volume does not support runtime class \"runsc\""
	if len(storage.publishFailures) != 1 || storage.publishFailures[0] != wantMessage {
		t.Fatalf("publish failures = %#v, want %q", storage.publishFailures, wantMessage)
	}
	if len(allocations.failedCreates) != 1 || allocations.failedCreates[0].message != wantMessage {
		t.Fatalf("failed creates = %#v, want message %q", allocations.failedCreates, wantMessage)
	}
	if got := workloadkernel.ClassifyDiagnostic(commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, allocations.failedCreates[0].message); got != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_PUBLISH_ERROR {
		t.Fatalf("diagnostic = %v, want volume publish", got)
	}
}

func TestReconcileAllocationCreateReplacesResourceExhaustedAllocation(t *testing.T) {
	now := time.Date(2026, 7, 10, 6, 35, 0, 0, time.UTC)
	service := &servicev1.Service{
		ID:            "svc-a",
		EnvironmentID: "env-a",
		AllocationIds: []string{"alloc-a"},
	}
	reconcile := &fakeServiceAllocationReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:  "alloc-a",
			OwnerID:       "svc-a",
			EnvironmentID: "env-a",
			Reason:        allocationkernel.ReconcileReasonCreate,
			NodeID:        "node-a",
			NodeTarget:    "node-a:24010",
		}},
	}
	allocations := &fakeReconcileAllocationStore{}

	_, err := (&controller{
		store:        &fakeReconcileServiceStore{getByID: map[string]*servicev1.Service{"svc-a": service}},
		allocations:  allocations,
		environments: &fakeReconcileEnvironmentReader{},
		reconcile:    reconcile,
		lifecycle:    &fakeServiceAllocationLifecycle{createErr: status.Error(codes.ResourceExhausted, "interface pool exhausted")},
	}).ReconcileAllocationBatch(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if len(allocations.failedCreates) != 1 || allocations.failedCreates[0].allocationID != "alloc-a" {
		t.Fatalf("failed creates = %#v, want alloc-a", allocations.failedCreates)
	}
	if len(reconcile.completedCreates) != 1 || reconcile.completedCreates[0] != "alloc-a" {
		t.Fatalf("completed creates = %#v, want alloc-a", reconcile.completedCreates)
	}
	if len(reconcile.scheduledRequests) != 0 {
		t.Fatalf("scheduled retries = %#v, want none", reconcile.scheduledRequests)
	}
}

func TestStoragePublishFailureMessagePreservesNonVolumeLifecycleErrors(t *testing.T) {
	volume := &privatestoragev1.ResolvedNodeVolume{BindingID: "alloc-a/data"}
	message := storagePublishFailureMessage([]*privatestoragev1.ResolvedNodeVolume{volume}, errors.New("resolve image ref repo/app:missing: image or tag was not found"))
	if message != "resolve image ref repo/app:missing: image or tag was not found" {
		t.Fatalf("message = %q", message)
	}
	if got := workloadkernel.ClassifyDiagnostic(commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, message); got != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR {
		t.Fatalf("diagnostic = %v, want image resolution", got)
	}
}

func TestScaleUpPersistsAdmissionWithoutStartingAllocation(t *testing.T) {
	now := time.Date(2026, 7, 10, 6, 30, 0, 0, time.UTC)
	current := &servicev1.Service{
		ID:            "svc-a",
		Namespace:     "default",
		EnvironmentID: "env-a",
		Config:        &commonv1.ExecutionConfig{},
	}
	allocations := &capacityRaceAllocationStore{current: current}
	reconcile := &fakeServiceAllocationReconcileStore{}
	lifecycle := &fakeServiceAllocationLifecycle{
		createErrByNode: map[string]error{
			"node-a": status.Error(codes.ResourceExhausted, "interface pool exhausted"),
		},
	}
	controller := &controller{
		allocations: allocations,
		reconcile:   reconcile,
		selector: &fixedCandidateSelector{candidates: testPlacementCandidates(
			&nodekernel.Record{NodeID: "node-a", NodeTarget: "node-a:24010"},
			&nodekernel.Record{NodeID: "node-b", NodeTarget: "node-b:24010"},
		)},
		lifecycle: lifecycle,
	}

	next, active, err := controller.scaleUp(context.Background(), current, &environmentv1.Environment{ID: "env-a"}, nil, 1, now)
	if err != nil {
		t.Fatalf("scaleUp() error = %v", err)
	}
	if len(active) != 1 || active[0].NodeID != "node-a" {
		t.Fatalf("active allocations = %#v, want one admitted on node-a", active)
	}
	if got, want := allocations.admittedNodes, []string{"node-a"}; !slices.Equal(got, want) {
		t.Fatalf("admitted nodes = %#v, want %#v", got, want)
	}
	if len(allocations.failedAllocationIDs) != 0 {
		t.Fatalf("failed allocations = %#v, want none before async execution", allocations.failedAllocationIDs)
	}
	if len(lifecycle.createRequests) != 0 || len(reconcile.completedCreates) != 0 {
		t.Fatalf("synchronous lifecycle work = %d creates, %d completions; want none", len(lifecycle.createRequests), len(reconcile.completedCreates))
	}
	if next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED || next.GetMessage() != "" {
		t.Fatalf("service after replacement = %#v, want recovered status", next)
	}
}

func TestScaleUpAdmitsBatchWithoutWaitingForNodeLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 10, 7, 15, 0, 0, time.UTC)
	current := &servicev1.Service{
		ID:            "svc-a",
		Namespace:     "default",
		EnvironmentID: "env-a",
		Config: &commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{CpuMilli: 100, MemoryBytes: 128},
		}},
	}
	allocations := &capacityRaceAllocationStore{current: current}
	reconcile := &fakeServiceAllocationReconcileStore{}
	lifecycle := &fakeServiceAllocationLifecycle{
		createErrByNode: map[string]error{
			"node-a": status.Error(codes.ResourceExhausted, "interface pool exhausted"),
		},
	}
	controller := &controller{
		allocations: allocations,
		reconcile:   reconcile,
		selector: &sequencedCandidateSelector{responses: [][]*placementkernel.Candidate{
			testPlacementCandidates(&nodekernel.Record{NodeID: "node-a", NodeTarget: "node-a:24010"}),
			testPlacementCandidates(&nodekernel.Record{NodeID: "node-b", NodeTarget: "node-b:24010"}),
			testPlacementCandidates(&nodekernel.Record{NodeID: "node-c", NodeTarget: "node-c:24010"}),
			testPlacementCandidates(&nodekernel.Record{NodeID: "node-d", NodeTarget: "node-d:24010"}),
		}},
		lifecycle: lifecycle,
	}

	next, active, err := controller.scaleUp(context.Background(), current, &environmentv1.Environment{ID: "env-a"}, nil, 3, now)
	if err != nil {
		t.Fatalf("scaleUp() error = %v", err)
	}
	if got, want := allocationNodes(active), []string{"node-a", "node-b", "node-c"}; !slices.Equal(got, want) {
		t.Fatalf("active nodes = %#v, want %#v", got, want)
	}
	if len(allocations.failedAllocationIDs) != 0 {
		t.Fatalf("failed allocations = %#v, want none before async execution", allocations.failedAllocationIDs)
	}
	if len(lifecycle.createRequests) != 0 || len(reconcile.completedCreates) != 0 {
		t.Fatalf("synchronous lifecycle work = %d creates, %d completions; want none", len(lifecycle.createRequests), len(reconcile.completedCreates))
	}
	if next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED || next.GetMessage() != "" {
		t.Fatalf("service after replacement = %#v, want recovered status", next)
	}
}

func TestDeleteAndConfirmAllocationRetriesLostDeleteResponseEvenWhenNodeConfirmsDeleted(t *testing.T) {
	lifecycle := &fakeServiceAllocationLifecycle{
		deleteErr:         status.Error(codes.Unavailable, "error reading server preface: EOF"),
		allocationDeleted: true,
	}

	deleted, err := (&controller{lifecycle: lifecycle}).deleteAndConfirmAllocation(context.Background(), &servicekernel.AllocationRecord{
		AllocationID: "alloc-a",
		NodeID:       "node-a",
		NodeTarget:   "node-a:24010",
		Attempt:      1,
	})
	if err == nil {
		t.Fatal("deleteAndConfirmAllocation() error = nil, want delete retry error")
	}
	if deleted {
		t.Fatal("deleteAndConfirmAllocation() deleted = true, want retry")
	}
	if lifecycle.deleteCalls != 1 || lifecycle.statusCalls != 1 {
		t.Fatalf("calls delete/status = %d/%d, want 1/1", lifecycle.deleteCalls, lifecycle.statusCalls)
	}
}

func TestDeleteAndConfirmAllocationReportsReleaseObservationsAfterConfirmedDelete(t *testing.T) {
	releaseObservations := []*privatestoragev1.VolumeReleaseObservation{{BindingID: "binding-1"}}
	lifecycle := &fakeServiceAllocationLifecycle{
		allocationDeleted:   true,
		releaseObservations: releaseObservations,
	}
	storage := &fakeStorageCoordinator{}

	deleted, err := (&controller{lifecycle: lifecycle, storage: storage}).deleteAndConfirmAllocation(context.Background(), &servicekernel.AllocationRecord{
		AllocationID: "alloc-a",
		NodeID:       "node-a",
		NodeTarget:   "node-a:24010",
		Attempt:      1,
	})
	if err != nil {
		t.Fatalf("deleteAndConfirmAllocation() error = %v", err)
	}
	if !deleted {
		t.Fatal("deleteAndConfirmAllocation() deleted = false, want true")
	}
	if len(storage.releaseObservations) != 1 || storage.releaseObservations[0].GetBindingID() != "binding-1" {
		t.Fatalf("release observations = %#v, want binding-1", storage.releaseObservations)
	}
}

func TestDeleteAndConfirmAllocationReturnsNonRecoverableDeleteErrorWithoutConfirming(t *testing.T) {
	lifecycle := &fakeServiceAllocationLifecycle{
		deleteErr: status.Error(codes.PermissionDenied, "wrong node"),
	}

	deleted, err := (&controller{lifecycle: lifecycle}).deleteAndConfirmAllocation(context.Background(), &servicekernel.AllocationRecord{
		AllocationID: "alloc-a",
		NodeID:       "node-a",
		NodeTarget:   "node-a:24010",
		Attempt:      1,
	})
	if err == nil {
		t.Fatal("deleteAndConfirmAllocation() error = nil, want delete error")
	}
	if deleted {
		t.Fatal("deleteAndConfirmAllocation() deleted = true, want false")
	}
	if lifecycle.statusCalls != 0 {
		t.Fatalf("status calls = %d, want 0", lifecycle.statusCalls)
	}
}

func TestAllocationsRequiringReleaseExcludesAlreadyScheduledAndReleased(t *testing.T) {
	pending := allocationsRequiringRelease([]*servicekernel.AllocationRecord{
		nil,
		{AllocationID: "running", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
		{AllocationID: "failed", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED},
		{AllocationID: "exited", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED},
		{AllocationID: "releasing", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING},
		{AllocationID: "released", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED},
	})
	got := make([]string, 0, len(pending))
	for _, allocation := range pending {
		got = append(got, allocation.AllocationID)
	}
	if !slices.Equal(got, []string{"running", "failed", "exited"}) {
		t.Fatalf("pending allocation ids = %v, want running, failed, and exited", got)
	}
}

type fakeReconcileServiceStore struct {
	services     []*servicev1.Service
	getByID      map[string]*servicev1.Service
	deleteResult *servicev1.Service
	deleteOK     bool
	deleteErr    error
	purgeResult  string
	purgeOK      bool
	purgeErr     error
	createResult *servicev1.Service
	createErr    error
}

func (f *fakeReconcileServiceStore) List(context.Context, *servicev1.ServiceListFilter) ([]*servicev1.Service, error) {
	return f.services, nil
}

func (f *fakeReconcileServiceStore) Get(_ context.Context, id string) (*servicev1.Service, bool, error) {
	if f.getByID != nil {
		service, ok := f.getByID[id]
		return service, ok, nil
	}
	for _, service := range f.services {
		if service.GetID() == id {
			return service, true, nil
		}
	}
	return nil, false, nil
}

func (f *fakeReconcileServiceStore) Create(context.Context, servicekernel.CreateParams, time.Time) (*servicev1.Service, error) {
	if f.createResult == nil && f.createErr == nil {
		panic("unexpected Create call")
	}
	return f.createResult, f.createErr
}

func (f *fakeReconcileServiceStore) Update(context.Context, *servicev1.UpdateServiceRequest, time.Time) (*servicev1.Service, error) {
	panic("unexpected Update call")
}

func (f *fakeReconcileServiceStore) Delete(context.Context, servicekernel.DeleteParams, time.Time) (*servicev1.Service, bool, error) {
	if f.deleteResult == nil && !f.deleteOK && f.deleteErr == nil {
		panic("unexpected Delete call")
	}
	return f.deleteResult, f.deleteOK, f.deleteErr
}

func (f *fakeReconcileServiceStore) Purge(context.Context, string, time.Time) (string, bool, error) {
	if f.purgeResult == "" && !f.purgeOK && f.purgeErr == nil {
		panic("unexpected Purge call")
	}
	return f.purgeResult, f.purgeOK, f.purgeErr
}

func (f *fakeReconcileServiceStore) AcquireLease(context.Context, string, string, time.Duration, time.Time) (*servicev1.Service, *commonv1.ExecutionLease, error) {
	panic("unexpected AcquireLease call")
}

func (f *fakeReconcileServiceStore) GetReplica(context.Context, string, string) (*servicev1.ServiceReplica, bool, error) {
	panic("unexpected GetReplica call")
}

func (f *fakeReconcileServiceStore) ListReplicas(context.Context, string, *servicev1.ServiceReplicaListFilter) ([]*servicev1.ServiceReplica, error) {
	panic("unexpected ListReplicas call")
}

func (f *fakeReconcileServiceStore) ListEvents(context.Context, string, int32) ([]*servicev1.ServiceEvent, error) {
	panic("unexpected ListEvents call")
}

type fakeReconcileEnvironmentReader struct {
	mu      sync.Mutex
	calls   int
	errByID map[string]error
}

func (f *fakeReconcileEnvironmentReader) GetEnvironment(_ context.Context, id string) (*environmentv1.Environment, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.errByID != nil && f.errByID[id] != nil {
		return nil, f.errByID[id]
	}
	return &environmentv1.Environment{ID: id}, nil
}

func (f *fakeReconcileEnvironmentReader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type failedCreateRecord struct {
	serviceID    string
	allocationID string
	message      string
}

type fakeReconcileAllocationStore struct {
	failedCreates []failedCreateRecord
	history       []*servicekernel.AllocationRecord
}

type fixedCandidateSelector struct {
	candidates []*placementkernel.Candidate
}

func (f *fixedCandidateSelector) SelectCandidates(context.Context, *environmentv1.Environment, *commonv1.ExecutionConfig) ([]*placementkernel.Candidate, error) {
	return f.candidates, nil
}

type sequencedCandidateSelector struct {
	calls     int
	responses [][]*placementkernel.Candidate
}

func (s *sequencedCandidateSelector) SelectCandidates(context.Context, *environmentv1.Environment, *commonv1.ExecutionConfig) ([]*placementkernel.Candidate, error) {
	return s.next()
}

func (s *sequencedCandidateSelector) next() ([]*placementkernel.Candidate, error) {
	if s.calls >= len(s.responses) {
		return nil, status.Error(codes.FailedPrecondition, "no eligible node")
	}
	response := s.responses[s.calls]
	s.calls++
	return response, nil
}

func testPlacementCandidates(records ...*nodekernel.Record) []*placementkernel.Candidate {
	out := make([]*placementkernel.Candidate, 0, len(records))
	for _, record := range records {
		out = append(out, &placementkernel.Candidate{Record: record})
	}
	return out
}

func allocationNodes(allocations []*servicekernel.AllocationRecord) []string {
	out := make([]string, 0, len(allocations))
	for _, alloc := range allocations {
		out = append(out, alloc.NodeID)
	}
	return out
}

type capacityRaceAllocationStore struct {
	current             *servicev1.Service
	admittedNodes       []string
	failedAllocationIDs []string
}

func (f *capacityRaceAllocationStore) CurrentServiceAllocations(context.Context, string) ([]*servicekernel.AllocationRecord, error) {
	panic("unexpected CurrentServiceAllocations call")
}

func (f *capacityRaceAllocationStore) ServiceAllocationHistory(context.Context, string) ([]*servicekernel.AllocationRecord, error) {
	panic("unexpected ServiceAllocationHistory call")
}

func (f *capacityRaceAllocationStore) AdmitAllocation(_ context.Context, _ string, _ *commonv1.ExecutionConfig, candidates []*placementkernel.Candidate, _ time.Time) (*servicev1.Service, *servicekernel.AllocationRecord, error) {
	if len(candidates) == 0 {
		return f.current, nil, status.Error(codes.ResourceExhausted, "no candidates")
	}
	selected := candidates[0]
	allocationID := fmt.Sprintf("alloc-%d", len(f.admittedNodes)+1)
	f.admittedNodes = append(f.admittedNodes, selected.NodeID)
	f.current = servicekernel.CloneService(f.current)
	f.current.AllocationIds = append(f.current.AllocationIds, allocationID)
	f.current.Status = servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING
	f.current.Message = ""
	return f.current, &servicekernel.AllocationRecord{
		AllocationID: allocationID,
		NodeID:       selected.NodeID,
		NodeTarget:   selected.NodeTarget,
		Attempt:      1,
	}, nil
}

func (f *capacityRaceAllocationStore) BeginAllocationRelease(context.Context, string, string, time.Time) (*servicev1.Service, *servicekernel.AllocationRecord, error) {
	panic("unexpected BeginAllocationRelease call")
}

func (f *capacityRaceAllocationStore) MarkAllocationCreateFailed(_ context.Context, _ string, allocationID, message string, _ time.Time) (*servicev1.Service, error) {
	f.failedAllocationIDs = append(f.failedAllocationIDs, allocationID)
	f.current = servicekernel.CloneService(f.current)
	f.current.AllocationIds = removeAllocationIDForTest(f.current.AllocationIds, allocationID)
	f.current.Status = servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED
	f.current.Message = message
	return f.current, nil
}

func (f *capacityRaceAllocationStore) RecordWorkspacePreparation(context.Context, string, string, int64, *commonv1.WorkspacePreparationFacts, time.Time) error {
	return nil
}

func (f *capacityRaceAllocationStore) CompleteAllocationRelease(context.Context, string, time.Time) error {
	panic("unexpected CompleteAllocationRelease call")
}

func (f *capacityRaceAllocationStore) CompleteClaimedAllocationRelease(context.Context, string, string, time.Time) (bool, error) {
	panic("unexpected CompleteClaimedAllocationRelease call")
}

func removeAllocationIDForTest(ids []string, target string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != target {
			out = append(out, id)
		}
	}
	return out
}

func (f *fakeReconcileAllocationStore) CurrentServiceAllocations(context.Context, string) ([]*servicekernel.AllocationRecord, error) {
	return nil, nil
}

func (f *fakeReconcileAllocationStore) ServiceAllocationHistory(context.Context, string) ([]*servicekernel.AllocationRecord, error) {
	return f.history, nil
}

func (f *fakeReconcileAllocationStore) AdmitAllocation(context.Context, string, *commonv1.ExecutionConfig, []*placementkernel.Candidate, time.Time) (*servicev1.Service, *servicekernel.AllocationRecord, error) {
	panic("unexpected AdmitAllocation call")
}

func (f *fakeReconcileAllocationStore) BeginAllocationRelease(context.Context, string, string, time.Time) (*servicev1.Service, *servicekernel.AllocationRecord, error) {
	panic("unexpected BeginAllocationRelease call")
}

func (f *fakeReconcileAllocationStore) MarkAllocationCreateFailed(_ context.Context, serviceID, allocationID, message string, _ time.Time) (*servicev1.Service, error) {
	f.failedCreates = append(f.failedCreates, failedCreateRecord{
		serviceID:    serviceID,
		allocationID: allocationID,
		message:      message,
	})
	return &servicev1.Service{ID: serviceID, Status: servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED, Message: message}, nil
}

func (f *fakeReconcileAllocationStore) RecordWorkspacePreparation(context.Context, string, string, int64, *commonv1.WorkspacePreparationFacts, time.Time) error {
	return nil
}

func (f *fakeReconcileAllocationStore) CompleteAllocationRelease(context.Context, string, time.Time) error {
	panic("unexpected CompleteAllocationRelease call")
}

func (f *fakeReconcileAllocationStore) CompleteClaimedAllocationRelease(context.Context, string, string, time.Time) (bool, error) {
	return true, nil
}

type fakeReconcileStatusStore struct {
	mu               sync.Mutex
	syncedServiceIDs []string
	missingOnSync    map[string]bool
	service          *servicev1.Service
}

func (f *fakeReconcileStatusStore) SyncObservedStatus(_ context.Context, serviceID string, _ time.Time) (*servicev1.Service, error) {
	f.mu.Lock()
	f.syncedServiceIDs = append(f.syncedServiceIDs, serviceID)
	missing := f.missingOnSync[serviceID]
	f.mu.Unlock()
	if missing {
		return nil, nil
	}
	if f.service != nil {
		return servicekernel.CloneService(f.service), nil
	}
	return &servicev1.Service{
		ID: serviceID, Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
		DeletionStatus: &servicev1.ServiceDeletionStatus{
			Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE,
			VolumeDisposition: servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
		},
	}, nil
}

func (f *fakeReconcileStatusStore) UpdateDeletionStatus(_ context.Context, serviceID string, deletion *servicev1.ServiceDeletionStatus, now time.Time) (*servicev1.Service, error) {
	service := &servicev1.Service{ID: serviceID, Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING}
	if f.service != nil {
		service = servicekernel.CloneService(f.service)
	}
	service = servicekernel.ApplyDeletionProgress(service, deletion, now)
	f.service = service
	return servicekernel.CloneService(service), nil
}

func (f *fakeReconcileStatusStore) UpdateStatus(context.Context, string, servicev1.ServiceStatus, string, time.Time) (*servicev1.Service, error) {
	panic("unexpected UpdateStatus call")
}

func (f *fakeReconcileStatusStore) UpdateAutoscalingStatus(context.Context, string, *servicev1.ServiceAutoscalingStatus, time.Time) (*servicev1.Service, error) {
	panic("unexpected UpdateAutoscalingStatus call")
}

type fakeServiceAllocationReconcileStore struct {
	mu                sync.Mutex
	items             []allocationkernel.ReconcileItem
	scheduleErr       error
	scheduledRequests []allocationkernel.ScheduleReconcileRequest
	completedCreates  []string
}

func (f *fakeServiceAllocationReconcileStore) DueReconcileItems(context.Context, int, time.Time) ([]allocationkernel.ReconcileItem, error) {
	return f.items, nil
}

func (f *fakeServiceAllocationReconcileStore) ClaimDueReconcileItems(_ context.Context, owner string, limit int, _ time.Time, _ time.Duration) ([]allocationkernel.ReconcileItem, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit > len(f.items) {
		limit = len(f.items)
	}
	items := append([]allocationkernel.ReconcileItem(nil), f.items[:limit]...)
	f.items = f.items[limit:]
	for index := range items {
		items[index].ClaimOwner = owner
	}
	return items, nil
}

func (f *fakeServiceAllocationReconcileStore) RenewReconcileClaim(context.Context, string, string, time.Time, time.Duration) (bool, error) {
	return true, nil
}

func (f *fakeServiceAllocationReconcileStore) ScheduleReconcile(_ context.Context, req allocationkernel.ScheduleReconcileRequest, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scheduledRequests = append(f.scheduledRequests, req)
	return f.scheduleErr
}

func (f *fakeServiceAllocationReconcileStore) CompleteAllocationCreate(_ context.Context, allocationID string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completedCreates = append(f.completedCreates, allocationID)
	return nil
}

func (f *fakeServiceAllocationReconcileStore) ScheduleClaimedReconcile(ctx context.Context, req allocationkernel.ScheduleReconcileRequest, _ string, now time.Time) (bool, error) {
	return true, f.ScheduleReconcile(ctx, req, now)
}

func (f *fakeServiceAllocationReconcileStore) CompleteClaimedAllocationCreate(ctx context.Context, allocationID, _ string, now time.Time) (bool, error) {
	return true, f.CompleteAllocationCreate(ctx, allocationID, now)
}

type fakeServiceAllocationLifecycle struct {
	mu                  sync.Mutex
	deleteErr           error
	allocationDeleted   bool
	allocationErr       error
	deleteCalls         int
	statusCalls         int
	createErr           error
	createErrByNode     map[string]error
	createStarted       chan struct{}
	createRelease       chan struct{}
	releaseObservations []*privatestoragev1.VolumeReleaseObservation
	createRequests      []servicekernel.CreateResolvedAllocationRequest
	volumeDeleteCalls   int
	volumeDeleteTargets []string
}

func (f *fakeServiceAllocationLifecycle) CreateResolvedAllocation(_ context.Context, req servicekernel.CreateResolvedAllocationRequest) (*servicekernel.CreateResolvedAllocationResult, error) {
	f.mu.Lock()
	f.createRequests = append(f.createRequests, req)
	err := f.createErr
	if f.createErrByNode != nil {
		if nodeErr := f.createErrByNode[req.NodeID]; nodeErr != nil {
			err = nodeErr
		}
	}
	started := f.createStarted
	release := f.createRelease
	f.mu.Unlock()
	if started != nil {
		started <- struct{}{}
	}
	if release != nil {
		<-release
	}
	return &servicekernel.CreateResolvedAllocationResult{}, err
}

func (f *fakeServiceAllocationLifecycle) DeleteResolvedAllocation(context.Context, string, string, int64, string) ([]*privatestoragev1.VolumeReleaseObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	return f.releaseObservations, f.deleteErr
}

func (f *fakeServiceAllocationLifecycle) AllocationDeleted(context.Context, string, string, int64, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls++
	return f.allocationDeleted, f.allocationErr
}

func (f *fakeServiceAllocationLifecycle) DeleteVolume(_ context.Context, target string, _ *privatestoragev1.VolumeReclaim) error {
	f.volumeDeleteCalls++
	f.volumeDeleteTargets = append(f.volumeDeleteTargets, target)
	return nil
}

type reclaimReport struct {
	claimID   string
	succeeded bool
	message   string
}

type fakeStorageCoordinator struct {
	mu                  sync.Mutex
	volumes             []*privatestoragev1.ResolvedNodeVolume
	reserveErr          error
	reserveRequests     []servicekernel.StorageReserveRequest
	releaseObservations []*privatestoragev1.VolumeReleaseObservation
	publishFailures     []string
	deleteResponses     []*privatestoragev1.DeleteWorkloadVolumeClaimsResponse
	deleteCalls         int
	releaseErr          error
	releaseCalls        int
	releasedServices    []string
	claimReclaims       []*privatestoragev1.VolumeReclaim
	reports             []reclaimReport
}

func (f *fakeStorageCoordinator) ClaimVolumeReclaims(_ context.Context, _ string, excludedNodeIDs []string) (*privatestoragev1.VolumeReclaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.claimReclaims) == 0 {
		return nil, nil
	}
	excluded := map[string]bool{}
	for _, nodeID := range excludedNodeIDs {
		excluded[nodeID] = true
	}
	for index, reclaim := range f.claimReclaims {
		if excluded[reclaim.GetNodeID()] {
			continue
		}
		f.claimReclaims = append(f.claimReclaims[:index], f.claimReclaims[index+1:]...)
		return reclaim, nil
	}
	return nil, nil
}

func (f *fakeStorageCoordinator) ResolveRequirements(context.Context, string, string, *commonv1.ExecutionConfig) ([]*privatestoragev1.VolumeRequirement, error) {
	return nil, nil
}

func (f *fakeStorageCoordinator) ReserveBindings(_ context.Context, req servicekernel.StorageReserveRequest) ([]*privatestoragev1.ResolvedNodeVolume, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reserveRequests = append(f.reserveRequests, req)
	return f.volumes, f.reserveErr
}

func (f *fakeStorageCoordinator) ReportBindingRelease(_ context.Context, _, _ string, observations []*privatestoragev1.VolumeReleaseObservation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseObservations = observations
	return nil
}

func (f *fakeStorageCoordinator) DeleteWorkloadVolumeClaims(context.Context, string, string) (*privatestoragev1.DeleteWorkloadVolumeClaimsResponse, error) {
	if f.deleteCalls < len(f.deleteResponses) {
		response := f.deleteResponses[f.deleteCalls]
		f.deleteCalls++
		return response, nil
	}
	return &privatestoragev1.DeleteWorkloadVolumeClaimsResponse{Complete: true}, nil
}

func (f *fakeStorageCoordinator) ReleaseWorkloadVolumeClaims(_ context.Context, _ string, serviceID string) (*privatestoragev1.ReleaseWorkloadVolumeClaimsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls++
	f.releasedServices = append(f.releasedServices, serviceID)
	if f.releaseErr != nil {
		return nil, f.releaseErr
	}
	return &privatestoragev1.ReleaseWorkloadVolumeClaimsResponse{}, nil
}

func (f *fakeStorageCoordinator) ReportVolumeReclaim(_ context.Context, reclaim *privatestoragev1.VolumeReclaim, succeeded bool, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, reclaimReport{claimID: reclaim.GetClaimID(), succeeded: succeeded, message: message})
	return nil
}

type blockingVolumeLifecycle struct {
	*fakeServiceAllocationLifecycle
	started chan string
	release chan struct{}
}

func (f *blockingVolumeLifecycle) DeleteVolume(ctx context.Context, target string, _ *privatestoragev1.VolumeReclaim) error {
	nodeID := strings.TrimSuffix(target, ":24010")
	select {
	case f.started <- nodeID:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeStorageCoordinator) ReportBindingPublish(context.Context, string, string, []*privatestoragev1.PublishedNodeVolume) error {
	return nil
}

func (f *fakeStorageCoordinator) ReportBindingPublishFailed(_ context.Context, _, _ string, _ []*privatestoragev1.ResolvedNodeVolume, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishFailures = append(f.publishFailures, message)
	return nil
}
