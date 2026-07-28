package pgservice

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestDeriveObservedHealthReadyAndDegraded(t *testing.T) {
	ready := deriveObservedHealth(&servicev1.Service{
		Replicas:      2,
		AllocationIds: []string{"alloc-a", "alloc-b"},
	}, []*serviceAllocationStatus{
		{Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true},
		{Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true},
	})
	if ready.status != servicev1.ServiceStatus_SERVICE_STATUS_READY || ready.readyReplicas != 2 || ready.unhealthyReplicas != 0 {
		t.Fatalf("ready health = %+v, want READY 2/0", ready)
	}

	degraded := deriveObservedHealth(&servicev1.Service{
		Replicas:          1,
		AllocationIds:     []string{"alloc-b"},
		UnhealthyReplicas: 1,
	}, []*serviceAllocationStatus{
		{Status: commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING},
	})
	if degraded.status != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED || degraded.readyReplicas != 0 || degraded.unhealthyReplicas != 1 {
		t.Fatalf("degraded health = %+v, want DEGRADED 0/1", degraded)
	}
}

func TestMatchReplicaFilterByViewAndStatus(t *testing.T) {
	replica := &servicev1.ServiceReplica{
		ID:       "alloc-a",
		Status:   commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		Ended:    true,
		Outdated: true,
	}
	if !matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ENDED}) {
		t.Fatal("ended replica did not match ended view")
	}
	if !matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNHEALTHY}) {
		t.Fatal("exited replica did not match unhealthy view")
	}
	if matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT}) {
		t.Fatal("ended replica unexpectedly matched current view")
	}
	if !matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{Statuses: []commonv1.AllocationStatus{commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED}}) {
		t.Fatal("replica did not match explicit EXITED status filter")
	}
	if matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{Statuses: []commonv1.AllocationStatus{commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING}}) {
		t.Fatal("replica unexpectedly matched RUNNING status filter")
	}
	if !matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_OUTDATED}) {
		t.Fatal("outdated replica did not match outdated view")
	}
	if matchReplicaFilter(replica, &servicev1.ServiceReplicaListFilter{View: servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UPDATED}) {
		t.Fatal("outdated replica unexpectedly matched updated view")
	}
}
