package appnode

import (
	"context"
	"errors"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

type reportStoreStub struct {
	record *nodekernel.Record
	err    error
}

func (s reportStoreStub) Report(context.Context, nodekernel.ReportParams) (*nodekernel.Record, error) {
	return s.record, s.err
}

type reportRegistrySpy struct {
	calls   int
	nodeID  string
	summary *nodev1.NodeSummary
}

func (r *reportRegistrySpy) Report(nodeID, _ string, summary *nodev1.NodeSummary, _ time.Time) {
	r.calls++
	r.nodeID = nodeID
	r.summary = summary
}

func TestReporterDoesNotPublishUncommittedReport(t *testing.T) {
	registry := &reportRegistrySpy{}
	reporter := NewReporter(reportStoreStub{err: errors.New("commit failed")}, registry, nil, time.Now)

	if _, err := reporter.Report(context.Background(), nodekernel.ReportParams{NodeID: "node-a"}); err == nil {
		t.Fatal("Report() error = nil, want store failure")
	}
	if registry.calls != 0 {
		t.Fatalf("registry Report() calls = %d, want 0", registry.calls)
	}
}

func TestReporterPublishesCommittedReport(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	summary := &nodev1.NodeSummary{}
	record := &nodekernel.Record{NodeID: "node-a", NodeTarget: "node-a:25001", Summary: summary, LastHeartbeatAt: now}
	registry := &reportRegistrySpy{}
	reporter := NewReporter(reportStoreStub{record: record}, registry, nil, func() time.Time { return now })

	if _, err := reporter.Report(context.Background(), nodekernel.ReportParams{NodeID: "node-a", Now: now}); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if registry.calls != 1 || registry.nodeID != "node-a" || registry.summary != summary {
		t.Fatalf("registry report = calls:%d node:%q summary:%p, want committed node-a summary:%p", registry.calls, registry.nodeID, registry.summary, summary)
	}
}

func TestReporterRenewsBoundAllocationsWhileNodeRecovers(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	summary := &nodev1.NodeSummary{} // A restarting axnoded is not ready yet.
	record := &nodekernel.Record{NodeID: "node-a", NodeTarget: "node-a:25001", Summary: summary, LastHeartbeatAt: now}
	registry := &reportRegistrySpy{}
	allocations := &fakeAvailabilityAllocations{executionIDs: []string{"allocation-a"}}
	reporter := NewReporter(reportStoreStub{record: record}, registry, allocations, func() time.Time { return now })

	got, err := reporter.Report(context.Background(), nodekernel.ReportParams{NodeID: "node-a", Now: now})
	if err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if len(got) != 1 || got[0] != "allocation-a" {
		t.Fatalf("execution authority = %#v, want allocation-a", got)
	}
	if allocations.executionListCalls != 1 {
		t.Fatalf("execution authority reads = %d, want 1", allocations.executionListCalls)
	}
	if allocations.inventoryCalls != 0 {
		t.Fatalf("inventory reconciliations = %d, want 0 while recovering", allocations.inventoryCalls)
	}
}
