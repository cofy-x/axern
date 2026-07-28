package app

import (
	"fmt"
	"testing"
)

func TestServiceReconcileQueueFallsBackToFullSweepWhenBounded(t *testing.T) {
	queue := newServiceReconcileQueue()
	serviceIDs := make([]string, 0, maxPendingServiceReconciles+1)
	for index := 0; index <= maxPendingServiceReconciles; index++ {
		serviceIDs = append(serviceIDs, fmt.Sprintf("svc-%d", index))
	}

	queue.Enqueue(serviceIDs...)
	item := queue.Take()

	if !item.FullSweep {
		t.Fatal("service reconcile queue did not fall back to a full sweep")
	}
	if item.ServiceID != "" {
		t.Fatalf("service ID = %q, want none for full sweep", item.ServiceID)
	}
	if item.EnqueuedAt.IsZero() {
		t.Fatal("full sweep batch enqueue time is zero")
	}
}

func TestServiceReconcileQueueRequeuesEventArrivingWhileInFlight(t *testing.T) {
	queue := newServiceReconcileQueue()
	queue.Enqueue("svc-a", "svc-a")
	first := queue.Take()
	if first.ServiceID != "svc-a" {
		t.Fatalf("first service ID = %q, want svc-a", first.ServiceID)
	}

	queue.Enqueue("svc-a")
	second := queue.Take()
	if second.ServiceID != "svc-a" {
		t.Fatalf("second service ID = %q, want svc-a", second.ServiceID)
	}
}
