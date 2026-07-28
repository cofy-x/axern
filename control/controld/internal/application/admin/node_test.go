package appadmin

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestRetireNodeRejectsStorageStateBeforeMutation(t *testing.T) {
	store := &fakeNodeLifecycleStore{}
	control := NewNodeControl(store, nil, fakeNodeStorageState{bindings: []adminkernel.StorageBinding{{BindingID: "binding-a"}}}, time.Minute)
	_, err := control.RetireNode(context.Background(), "node-a", "remove failed host", time.Now())
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("RetireNode() error = %v", err)
	}
	if store.retireCalls != 0 {
		t.Fatalf("retire calls = %d, want 0", store.retireCalls)
	}
}

func TestRetireNodeUpdatesRegistryAfterDurableMutation(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	store := &fakeNodeLifecycleStore{record: &nodekernel.Record{NodeID: "node-a", Lifecycle: nodekernel.LifecycleRetired, RetiredAt: now, RetiredReason: "remove failed host"}}
	registry := nodekernel.NewRegistry()
	registry.Register("node-a", "node-a:25000", []string{"runsc"}, now)
	control := NewNodeControl(store, registry, nil, time.Minute)
	if _, err := control.RetireNode(context.Background(), "node-a", "remove failed host", now); err != nil {
		t.Fatalf("RetireNode() error = %v", err)
	}
	record, ok := registry.Get("node-a")
	if !ok || record.Lifecycle != nodekernel.LifecycleRetired || record.RetiredReason != "remove failed host" {
		t.Fatalf("registry record = %+v", record)
	}
}

type fakeNodeLifecycleStore struct {
	record      *nodekernel.Record
	retireCalls int
}

func (f *fakeNodeLifecycleStore) ListNodes(context.Context, adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	return nil, nil
}

func (f *fakeNodeLifecycleStore) RetireNode(context.Context, adminkernel.RetireNodeRequest) (*nodekernel.Record, error) {
	f.retireCalls++
	return f.record, nil
}

type fakeNodeStorageState struct {
	bindings []adminkernel.StorageBinding
	reclaims []adminkernel.StorageReclaim
}

func (f fakeNodeStorageState) ListStorageBindings(context.Context, adminkernel.StorageBindingFilter) ([]adminkernel.StorageBinding, error) {
	return f.bindings, nil
}

func (f fakeNodeStorageState) ListStorageReclaims(context.Context, adminkernel.StorageReclaimFilter) ([]adminkernel.StorageReclaim, error) {
	return f.reclaims, nil
}
