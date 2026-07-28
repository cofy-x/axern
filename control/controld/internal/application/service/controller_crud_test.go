package appservice

import (
	"context"
	"testing"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDeletePersistsIntentAndWakesReconciler(t *testing.T) {
	now := time.Date(2026, 7, 13, 10, 30, 0, 0, time.UTC)
	store := &fakeReconcileServiceStore{
		deleteResult: &servicev1.Service{
			ID:     "svc-a",
			Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETING,
		},
		deleteOK: true,
	}
	wake := make(chan string, 1)
	controller := &controller{
		store: store,
		notifyReconcile: func(serviceIDs ...string) {
			for _, serviceID := range serviceIDs {
				wake <- serviceID
			}
		},
	}

	service, ok, err := controller.Delete(context.Background(), servicekernel.DeleteParams{ServiceID: "svc-a"}, now)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !ok || service.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("Delete() = (%v, %t), want persisted deleting service", service, ok)
	}
	select {
	case serviceID := <-wake:
		if serviceID != "svc-a" {
			t.Fatalf("woken service = %q, want svc-a", serviceID)
		}
	default:
		t.Fatal("Delete() did not wake the service reconciler")
	}
}

func TestPurgeDelegatesToFinalizer(t *testing.T) {
	now := time.Date(2026, 7, 13, 10, 35, 0, 0, time.UTC)
	store := &fakeReconcileServiceStore{purgeResult: "svc-a", purgeOK: true}
	controller := &controller{store: store}

	serviceID, ok, err := controller.Purge(context.Background(), "svc-a", now)
	if err != nil {
		t.Fatalf("Purge() error = %v", err)
	}
	if !ok || serviceID != "svc-a" {
		t.Fatalf("Purge() = (%q, %t), want (svc-a, true)", serviceID, ok)
	}
}

func TestPurgeDoesNotPerformNodeCleanup(t *testing.T) {
	now := time.Date(2026, 7, 13, 10, 35, 0, 0, time.UTC)
	store := &fakeReconcileServiceStore{
		purgeErr: status.Error(codes.FailedPrecondition, "allocation pending release"),
	}
	lifecycle := &fakeServiceAllocationLifecycle{allocationDeleted: true}
	controller := &controller{store: store, lifecycle: lifecycle}

	if _, _, err := controller.Purge(context.Background(), "svc-a", now); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Purge() code = %v, want FailedPrecondition", status.Code(err))
	}
	if lifecycle.deleteCalls != 0 || lifecycle.statusCalls != 0 {
		t.Fatalf("purge node calls = delete %d, status %d, want 0", lifecycle.deleteCalls, lifecycle.statusCalls)
	}
}
