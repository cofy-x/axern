package tunnel

import (
	"context"
	"reflect"
	"testing"
	"time"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
)

type contextKey string

type fakeServiceClient struct {
	replicas []*servicev1.ServiceReplica
}

func (f fakeServiceClient) ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error) {
	return &servicev1.ListServiceReplicasResponse{Replicas: f.replicas}, nil
}

func TestReadyAllocationIDsFiltersAndSorts(t *testing.T) {
	got := readyAllocationIDs([]*servicev1.ServiceReplica{
		{ID: "alloc-c", Ready: true},
		{ID: "alloc-ended", Ready: true, Ended: true},
		{ID: "alloc-outdated", Ready: true, Outdated: true},
		{ID: "alloc-not-ready"},
		{ID: "alloc-a", Ready: true},
	})
	want := []string{"alloc-a", "alloc-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("readyAllocationIDs() = %v, want %v", got, want)
	}
}

func TestSelectReadyServiceAllocationDefaultsToFirstStableReadyReplica(t *testing.T) {
	got, err := SelectReadyServiceAllocation(context.Background(), fakeServiceClient{replicas: []*servicev1.ServiceReplica{
		{ID: "alloc-b", NodeID: "node-b", Ready: true},
		{ID: "alloc-a", NodeID: "node-a", Ready: true},
	}}, ServiceAllocationSelectionParams{ServiceID: "svc-1"})
	if err != nil {
		t.Fatalf("SelectReadyServiceAllocation returned error: %v", err)
	}
	if got.AllocationID != "alloc-a" || got.NodeID != "node-a" || got.Reason != "stable first ready allocation" || got.ReadyReplicaCount != 2 {
		t.Fatalf("selected allocation = %+v, want alloc-a/node-a stable selection", got)
	}
}

func TestSelectReadyServiceAllocationRequiresRequestedReadyReplica(t *testing.T) {
	_, err := SelectReadyServiceAllocation(context.Background(), fakeServiceClient{replicas: []*servicev1.ServiceReplica{
		{ID: "alloc-a", Ready: true},
	}}, ServiceAllocationSelectionParams{ServiceID: "svc-1", AllocationID: "alloc-b"})
	if err == nil {
		t.Fatal("expected missing requested allocation error")
	}
}

func TestSelectReadyServiceAllocationByNodeID(t *testing.T) {
	got, err := SelectReadyServiceAllocation(context.Background(), fakeServiceClient{replicas: []*servicev1.ServiceReplica{
		{ID: "alloc-a", NodeID: "node-a", Ready: true},
		{ID: "alloc-b", NodeID: "node-b", Ready: true},
	}}, ServiceAllocationSelectionParams{ServiceID: "svc-1", NodeID: "node-b"})
	if err != nil {
		t.Fatalf("SelectReadyServiceAllocation returned error: %v", err)
	}
	if got.AllocationID != "alloc-b" || got.NodeID != "node-b" || got.Reason != "explicit node id" {
		t.Fatalf("selected allocation = %+v, want alloc-b/node-b explicit node", got)
	}
}

func TestSelectReadyServiceAllocationRejectsConflictingSelectors(t *testing.T) {
	_, err := SelectReadyServiceAllocation(context.Background(), fakeServiceClient{replicas: []*servicev1.ServiceReplica{
		{ID: "alloc-a", NodeID: "node-a", Ready: true},
	}}, ServiceAllocationSelectionParams{ServiceID: "svc-1", AllocationID: "alloc-a", NodeID: "node-a"})
	if err == nil {
		t.Fatal("expected conflicting selector error")
	}
}

func TestForwardServiceUsesCreateContextForReplicaSelection(t *testing.T) {
	const key contextKey = "setup"
	client := &fakeTunnelClient{}
	serviceClient := fakeServiceClientWithContext{
		t:    t,
		key:  key,
		want: "create-context",
		replicas: []*servicev1.ServiceReplica{
			{ID: "alloc-a", Ready: true},
		},
	}
	err := New(client).ForwardService(context.WithValue(context.Background(), key, "foreground-context"), serviceClient, ServiceForwardParams{
		CreateContext: context.WithValue(context.Background(), key, "create-context"),
		ServiceID:     "svc-1",
		LocalTarget:   "127.0.0.1:8080",
		TTL:           time.Hour,
		ReadyTimeout:  30 * time.Second,
		DisableRenew:  true,
		ConnectorRunner: func(context.Context, *tunnelv1.TunnelSession, string, string, RelayDialConfig, ConnectorConfig) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ForwardService returned error: %v", err)
	}
	if client.created == nil || client.created.GetAllocationID() != "alloc-a" {
		t.Fatalf("unexpected create request: %+v", client.created)
	}
}

type fakeServiceClientWithContext struct {
	t        *testing.T
	key      contextKey
	want     string
	replicas []*servicev1.ServiceReplica
}

func (f fakeServiceClientWithContext) ListServiceReplicas(ctx context.Context, _ *servicev1.ListServiceReplicasRequest, _ ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error) {
	f.t.Helper()
	if got := ctx.Value(f.key); got != f.want {
		f.t.Fatalf("ListServiceReplicas context value = %v, want %s", got, f.want)
	}
	return &servicev1.ListServiceReplicasResponse{Replicas: f.replicas}, nil
}
