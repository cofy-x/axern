package appadmin

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
)

func TestRetireNodeUpdatesRegistryAfterDurableMutation(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	store := &fakeNodeLifecycleStore{record: &nodekernel.Record{NodeID: "node-a", Lifecycle: nodekernel.LifecycleRetired, RetiredAt: now, RetiredReason: "remove failed host"}}
	registry := nodekernel.NewRegistry()
	registry.Report("node-a", "node-a:25000", nil, now)
	control := NewNodeControl(store, registry, time.Minute)
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

func (f *fakeNodeLifecycleStore) AdmitNode(context.Context, adminkernel.AdmitNodeRequest) (*nodekernel.Record, error) {
	return f.record, nil
}

func (f *fakeNodeLifecycleStore) ListNodes(context.Context, adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	return nil, nil
}

func (f *fakeNodeLifecycleStore) RetireNode(context.Context, adminkernel.RetireNodeRequest) (*nodekernel.Record, error) {
	f.retireCalls++
	return f.record, nil
}
