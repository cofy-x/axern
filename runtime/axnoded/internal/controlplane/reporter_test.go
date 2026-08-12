package controlplane

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type fakeNodeControlServer struct {
	nodev1.UnimplementedNodeControlServer

	mu            sync.Mutex
	registerCalls []*nodev1.RegisterNodeRequest
	reportCalls   []*nodev1.ReportNodeRequest
	statusCalls   []*nodev1.BatchReportAllocationStatusRequest
	memoryCalls   []*nodev1.BatchReportAllocationMemoryObservationsRequest
}

type fakeNodeControlProvider struct {
	client nodev1.NodeControlClient
}

func (p fakeNodeControlProvider) Client(context.Context) (nodev1.NodeControlClient, error) {
	return p.client, nil
}

func (p fakeNodeControlProvider) Close() error {
	return nil
}

func (s *fakeNodeControlServer) RegisterNode(ctx context.Context, req *nodev1.RegisterNodeRequest) (*nodev1.RegisterNodeResponse, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registerCalls = append(s.registerCalls, req)
	return &nodev1.RegisterNodeResponse{}, nil
}

func (s *fakeNodeControlServer) ReportNode(ctx context.Context, req *nodev1.ReportNodeRequest) (*nodev1.ReportNodeResponse, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reportCalls = append(s.reportCalls, req)
	return &nodev1.ReportNodeResponse{}, nil
}

func (s *fakeNodeControlServer) BatchReportAllocationStatus(ctx context.Context, req *nodev1.BatchReportAllocationStatusRequest) (*nodev1.BatchReportAllocationStatusResponse, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCalls = append(s.statusCalls, req)
	return &nodev1.BatchReportAllocationStatusResponse{}, nil
}

func (s *fakeNodeControlServer) BatchReportAllocationMemoryObservations(_ context.Context, req *nodev1.BatchReportAllocationMemoryObservationsRequest) (*nodev1.BatchReportAllocationMemoryObservationsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memoryCalls = append(s.memoryCalls, req)
	return &nodev1.BatchReportAllocationMemoryObservationsResponse{}, nil
}

func TestReporterSkipsHeartbeatUntilInventoryReady(t *testing.T) {
	client, fake, cleanup := newFakeNodeControlClient(t)
	defer cleanup()

	var ready atomic.Bool
	r := &Reporter{
		target:       "unused",
		nodeID:       "node-a",
		interval:     10 * time.Millisecond,
		runtimeNames: func() []string { return []string{"runsc"} },
		snapshot: func() (nodeinventory.NodeInventorySnapshot, bool) {
			if !ready.Load() {
				return nodeinventory.NewSnapshot(), false
			}
			snapshot := nodeinventory.NewSnapshot()
			snapshot.Node.CollectedAt = time.Now().UTC()
			snapshot.Components.Axnoded.Ready = true
			return snapshot, true
		},
		summaryBuilder: func(snapshot nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary {
			return &nodev1.NodeSummary{}
		},
		control: fakeNodeControlProvider{client: client},
		stopCh:  make(chan struct{}),
	}

	r.Start()
	time.Sleep(30 * time.Millisecond)
	ready.Store(true)
	time.Sleep(30 * time.Millisecond)
	r.Stop()

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.registerCalls) == 0 {
		t.Fatal("expected register call")
	}
	if len(fake.reportCalls) == 0 {
		t.Fatal("expected report call after inventory became ready")
	}
	if fake.reportCalls[0].GetSummary() == nil || fake.reportCalls[0].GetSummary().GetCollectedAt() == nil {
		t.Fatalf("expected report call to carry summary.collected_at: %#v", fake.reportCalls[0])
	}
}

func TestReporterSendsMemoryObservationsWhileInventoryIsNotReady(t *testing.T) {
	client, fake, cleanup := newFakeNodeControlClient(t)
	defer cleanup()

	r := &Reporter{
		nodeID:       "node-a",
		runtimeNames: func() []string { return []string{"runsc"} },
		snapshot: func() (nodeinventory.NodeInventorySnapshot, bool) {
			snapshot := nodeinventory.NewSnapshot()
			snapshot.AllocationMemoryObservations = []*nodev1.AllocationMemoryObservation{{
				AllocationID: "allocation-a",
			}}
			return snapshot, false
		},
		summaryBuilder: func(nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary {
			t.Fatal("summary must not be built while inventory is unavailable")
			return nil
		},
		control: fakeNodeControlProvider{client: client},
	}
	r.report()

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.reportCalls) != 0 {
		t.Fatalf("node report calls = %d, want 0", len(fake.reportCalls))
	}
	if len(fake.memoryCalls) != 1 || len(fake.memoryCalls[0].GetObservations()) != 1 {
		t.Fatalf("memory report calls = %#v, want one observation batch", fake.memoryCalls)
	}
}

func TestReporterCoalescesInventoryChangeReports(t *testing.T) {
	client, fake, cleanup := newFakeNodeControlClient(t)
	defer cleanup()

	var refreshes atomic.Int32
	r := &Reporter{
		target:       "unused",
		nodeID:       "node-a",
		interval:     time.Hour,
		runtimeNames: func() []string { return []string{"runsc"} },
		snapshot: func() (nodeinventory.NodeInventorySnapshot, bool) {
			snapshot := nodeinventory.NewSnapshot()
			snapshot.Node.CollectedAt = time.Now().UTC()
			return snapshot, true
		},
		summaryBuilder: func(nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary {
			return &nodev1.NodeSummary{}
		},
		refreshInventory: func() { refreshes.Add(1) },
		control:          fakeNodeControlProvider{client: client},
		stopCh:           make(chan struct{}),
	}
	r.Start()
	for range 32 {
		r.NotifyInventoryChanged()
	}
	deadline := time.Now().Add(time.Second)
	for refreshes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r.Stop()

	if got := refreshes.Load(); got != 1 {
		t.Fatalf("inventory refreshes = %d, want 1", got)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if got := len(fake.reportCalls); got != 2 {
		t.Fatalf("node reports = %d, want initial plus one coalesced report", got)
	}
}

func TestReporterCanUseRealGRPCClient(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lis.Close()

	server := grpc.NewServer()
	fake := &fakeNodeControlServer{}
	nodev1.RegisterNodeControlServer(server, fake)
	defer server.Stop()
	go server.Serve(lis)

	r := NewReporter(
		lis.Addr().String(),
		"node-b",
		"127.0.0.1:25001",
		"node-token",
		"",
		"",
		"",
		10*time.Millisecond,
		func() []string { return []string{"runsc"} },
		func() (nodeinventory.NodeInventorySnapshot, bool) {
			snapshot := nodeinventory.NewSnapshot()
			snapshot.Node.CollectedAt = time.Now().UTC()
			return snapshot, true
		},
		func(snapshot nodeinventory.NodeInventorySnapshot) *nodev1.NodeSummary {
			_ = snapshot
			return &nodev1.NodeSummary{}
		},
		nil,
	)
	if r == nil {
		t.Fatal("expected reporter")
	}
	r.Start()
	time.Sleep(30 * time.Millisecond)
	r.Stop()

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.registerCalls) == 0 {
		t.Fatal("expected register call")
	}
	if len(fake.reportCalls) == 0 {
		t.Fatal("expected report call")
	}
	if fake.reportCalls[0].GetSummary() == nil || fake.reportCalls[0].GetSummary().GetCollectedAt() == nil {
		t.Fatalf("expected report call to carry summary.collected_at: %#v", fake.reportCalls[0])
	}
}

func TestReporterAllocationStatusPreservesObservedSemantics(t *testing.T) {
	client, fake, cleanup := newFakeNodeControlClient(t)
	defer cleanup()

	outbox := NewAllocationStatusOutbox(storetest.NewMockStore())
	r := &Reporter{
		target:       "unused",
		nodeID:       "node-a",
		control:      fakeNodeControlProvider{client: client},
		statusOutbox: outbox,
	}
	r.ensureStatusBatcher().Start()
	defer r.ensureStatusBatcher().Stop()

	observedAt := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	r.ReportAllocationStatus(AllocationStatusReport{
		AllocationID:     " alloc-1 ",
		Attempt:          2,
		Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		ExitCode:         0,
		ExitCodeKnown:    false,
		Ready:            false,
		ReadinessMessage: "warming up",
		ObservedAt:       observedAt,
	})
	r.ReportAllocationStatus(AllocationStatusReport{
		AllocationID:   "alloc-2",
		Attempt:        2,
		Status:         commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		ExitCode:       17,
		ExitCodeKnown:  true,
		Message:        "process exited",
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED,
		ObservedAt:     observedAt,
	})

	awaitStatusCalls(t, fake, 1)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		replayed, err := outbox.Replay()
		if err != nil {
			t.Fatalf("Replay() error = %v", err)
		}
		if len(replayed) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if replayed, err := outbox.Replay(); err != nil || len(replayed) != 0 {
		t.Fatalf("terminal outbox after RPC acknowledgement = %#v, error %v", replayed, err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.statusCalls) != 1 {
		t.Fatalf("status call count = %d, want 1", len(fake.statusCalls))
	}
	observations := fake.statusCalls[0].GetObservations()
	if len(observations) != 2 {
		t.Fatalf("observation count = %d, want 2", len(observations))
	}
	if observations[0].GetAllocationID() != "alloc-1" {
		t.Fatalf("allocation_id = %q, want alloc-1", observations[0].GetAllocationID())
	}
	if observations[0].GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING || observations[0].GetReady() {
		t.Fatalf("first observation = status:%v ready:%t, want RUNNING/false", observations[0].GetStatus(), observations[0].GetReady())
	}
	if observations[0].GetReadinessMessage() != "warming up" {
		t.Fatalf("readiness_message = %q, want warming up", observations[0].GetReadinessMessage())
	}
	if observations[1].GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED {
		t.Fatalf("diagnostic_code = %v, want MEMORY_LIMIT_EXCEEDED", observations[1].GetDiagnosticCode())
	}
	if observations[1].GetAllocationID() != "alloc-2" || observations[1].GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("second observation = id:%q status:%v, want alloc-2/EXITED", observations[1].GetAllocationID(), observations[1].GetStatus())
	}
	if observations[1].GetMessage() != "process exited" {
		t.Fatalf("message = %q, want process exited", observations[1].GetMessage())
	}
}

func TestReporterAllocationStatusSanitizesRuntimeMessages(t *testing.T) {
	client, fake, cleanup := newFakeNodeControlClient(t)
	defer cleanup()

	r := &Reporter{
		target:  "unused",
		nodeID:  "node-a",
		control: fakeNodeControlProvider{client: client},
	}
	r.ensureStatusBatcher().Start()
	defer r.ensureStatusBatcher().Stop()

	r.ReportAllocationStatus(AllocationStatusReport{
		AllocationID:     "alloc-invalid-utf8",
		Attempt:          1,
		Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		ReadinessMessage: "probe \xff failed",
		Message:          "runtime \xfe output",
		ObservedAt:       time.Now(),
	})

	awaitStatusCalls(t, fake, 1)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	observation := fake.statusCalls[0].GetObservations()[0]
	if !utf8.ValidString(observation.GetReadinessMessage()) || !utf8.ValidString(observation.GetMessage()) {
		t.Fatalf("reported messages must be valid UTF-8: %#v", observation)
	}
	if observation.GetReadinessMessage() != "probe \uFFFD failed" {
		t.Fatalf("readiness_message = %q, want replacement character", observation.GetReadinessMessage())
	}
	if observation.GetMessage() != "runtime \uFFFD output" {
		t.Fatalf("message = %q, want replacement character", observation.GetMessage())
	}
}

func TestAllocationStatusObservationRejectsOutOfRangeTime(t *testing.T) {
	_, err := AllocationStatusObservationFromReport(AllocationStatusReport{
		AllocationID: "alloc-invalid-time",
		Attempt:      1,
		Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		ObservedAt:   time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("AllocationStatusObservationFromReport() error = nil, want invalid timestamp")
	}
}

func awaitStatusCalls(t *testing.T, fake *fakeNodeControlServer, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		got := len(fake.statusCalls)
		fake.mu.Unlock()
		if got >= count {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d allocation status calls", count)
}

func newFakeNodeControlClient(t *testing.T) (nodev1.NodeControlClient, *fakeNodeControlServer, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	fake := &fakeNodeControlServer{}
	nodev1.RegisterNodeControlServer(server, fake)
	go server.Serve(lis)

	conn, err := grpcclient.NewReadyClient(
		context.Background(),
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cleanup := func() {
		conn.Close()
		server.Stop()
		lis.Close()
	}
	return nodev1.NewNodeControlClient(conn), fake, cleanup
}
