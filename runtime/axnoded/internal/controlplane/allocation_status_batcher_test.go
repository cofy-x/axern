package controlplane

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestAllocationStatusBatcherCoalescesLatestObservation(t *testing.T) {
	batches := make(chan []*nodev1.AllocationStatusObservation, 1)
	batcher := newAllocationStatusBatcher(func(_ context.Context, observations []*nodev1.AllocationStatusObservation) error {
		batches <- observations
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, false))
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true))

	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || !batch[0].GetReady() {
		t.Fatalf("batch = %#v, want one ready observation", batch)
	}
}

func TestAllocationStatusBatcherPreservesTerminalObservation(t *testing.T) {
	batches := make(chan []*nodev1.AllocationStatusObservation, 1)
	batcher := newAllocationStatusBatcher(func(_ context.Context, observations []*nodev1.AllocationStatusObservation) error {
		batches <- observations
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, false))
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true))

	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || batch[0].GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("batch = %#v, want terminal observation", batch)
	}
}

func TestAllocationStatusBatcherRetriesFailedBatch(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	batches := make(chan []*nodev1.AllocationStatusObservation, 1)
	batcher := newAllocationStatusBatcher(func(_ context.Context, observations []*nodev1.AllocationStatusObservation) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return errors.New("temporary failure")
		}
		batches <- observations
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true))
	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || batch[0].GetAllocationID() != "alloc-1" {
		t.Fatalf("batch = %#v, want retried observation", batch)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("send calls = %d, want 2", gotCalls)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		health := batcher.Health()
		if health.Status == "idle" && health.ConsecutiveFailures == 0 && health.LastSuccessAt != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("reporter did not recover: %#v", batcher.Health())
}

func TestAllocationStatusBatcherRetryKeepsTerminalOverConcurrentNonterminal(t *testing.T) {
	firstSend := make(chan struct{})
	releaseFirstSend := make(chan struct{})
	batches := make(chan []*nodev1.AllocationStatusObservation, 1)
	var calls int
	var mu sync.Mutex
	batcher := newAllocationStatusBatcher(func(_ context.Context, observations []*nodev1.AllocationStatusObservation) error {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(firstSend)
			<-releaseFirstSend
			return errors.New("temporary failure")
		}
		batches <- observations
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, false))
	select {
	case <-firstSend:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first send")
	}
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true))
	close(releaseFirstSend)

	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || batch[0].GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("retried batch = %#v, want terminal observation", batch)
	}
}

func TestAllocationStatusBatcherSuccessKeepsTerminalOverConcurrentNonterminal(t *testing.T) {
	firstSend := make(chan struct{})
	releaseFirstSend := make(chan struct{})
	batches := make(chan []*nodev1.AllocationStatusObservation, 2)
	batcher := newAllocationStatusBatcher(func(_ context.Context, observations []*nodev1.AllocationStatusObservation) error {
		batches <- observations
		select {
		case <-firstSend:
		default:
			close(firstSend)
			<-releaseFirstSend
		}
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, false))
	select {
	case <-firstSend:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first send")
	}
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true))
	close(releaseFirstSend)

	first := awaitStatusBatch(t, batches)
	if len(first) != 1 || first[0].GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("first batch = %#v, want terminal observation", first)
	}
	select {
	case batch := <-batches:
		t.Fatalf("sent state after terminal observation: %#v", batch)
	case <-time.After(100 * time.Millisecond):
	}
	if health := batcher.Health(); health.Pending != 0 {
		t.Fatalf("reporter health = %#v, want empty queue", health)
	}
}

func TestAllocationStatusBatcherStartPublishesIdleHealthMetrics(t *testing.T) {
	metrics.ResetForTest()
	batcher := newAllocationStatusBatcher(func(context.Context, []*nodev1.AllocationStatusObservation) error {
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	want := map[string]bool{
		metrics.MetricAllocationStatusOldestPendingAge:    false,
		metrics.MetricAllocationStatusConsecutiveFailures: false,
		metrics.MetricAllocationStatusRetryDelay:          false,
	}
	for _, point := range metrics.SnapshotCurrent().Points {
		if _, ok := want[point.Name]; ok {
			if point.Type != metrics.TypeGauge || point.Value != 0 {
				t.Fatalf("idle health point = %#v, want zero gauge", point)
			}
			want[point.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("idle health metric %q was not published", name)
		}
	}
}

func TestAllocationStatusBatcherBacksOffWithoutNewEventBypass(t *testing.T) {
	metrics.ResetForTest()
	calls := make(chan time.Time, 8)
	batcher := newAllocationStatusBatcher(func(_ context.Context, _ []*nodev1.AllocationStatusObservation) error {
		calls <- time.Now()
		return errors.New("control plane unavailable")
	})
	batcher.batchDelay = 0
	batcher.retryInitialDelay = 120 * time.Millisecond
	batcher.retryMaxDelay = 240 * time.Millisecond
	batcher.jitter = func(delay time.Duration) time.Duration { return delay }
	batcher.Start()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true))
	first := <-calls
	batcher.Enqueue(statusObservation("alloc-2", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, false))

	deadline := time.Now().Add(time.Second)
	var health AllocationStatusReporterHealth
	for time.Now().Before(deadline) {
		health = batcher.Health()
		if health.Status == "retrying" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if health.Status != "retrying" || health.ConsecutiveFailures != 1 || health.Pending != 2 {
		t.Fatalf("health during retry = %#v", health)
	}
	if got := batcher.UnacknowledgedAllocationIDs(); len(got) != 2 || got[0] != "alloc-1" || got[1] != "alloc-2" {
		t.Fatalf("retry allocation ids = %#v, want [alloc-1 alloc-2]", got)
	}
	if health.RetryDelaySec != 0.12 || health.NextRetryAt == nil || health.LastError == "" {
		t.Fatalf("retry diagnostics = %#v", health)
	}
	if got := metrics.GaugeValueForTest(metrics.MetricAllocationStatusConsecutiveFailures, nil); got != 1 {
		t.Fatalf("consecutive failure gauge = %v, want 1", got)
	}

	select {
	case second := <-calls:
		t.Fatalf("new event bypassed retry delay after %s", second.Sub(first))
	case <-time.After(60 * time.Millisecond):
	}
	second := <-calls
	if elapsed := second.Sub(first); elapsed < 100*time.Millisecond {
		t.Fatalf("retry elapsed = %s, want bounded backoff", elapsed)
	}
	batcher.Stop()
	if health := batcher.Health(); health.Status != "stopped" || health.Pending == 0 {
		t.Fatalf("health after failed shutdown flush = %#v", health)
	}
}

func TestNextAllocationStatusRetryDelayIsBounded(t *testing.T) {
	initial := 100 * time.Millisecond
	maximum := 800 * time.Millisecond
	got := make([]time.Duration, 0, 6)
	var previous time.Duration
	for range 6 {
		previous = nextAllocationStatusRetryDelay(previous, initial, maximum)
		got = append(got, previous)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 800 * time.Millisecond, 800 * time.Millisecond}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("retry delay[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestAllocationStatusRetryJitterIsBounded(t *testing.T) {
	base := time.Second
	for range 1000 {
		got := allocationStatusRetryJitter(base)
		if got < 800*time.Millisecond || got > 1200*time.Millisecond {
			t.Fatalf("jittered delay = %s, want [800ms, 1.2s]", got)
		}
	}
}

func TestBoundedReporterErrorLimitsUTF8Bytes(t *testing.T) {
	got := boundedReporterError(errors.New(strings.Repeat("错", allocationStatusLastErrorMaxBytes)))
	if len(got) > allocationStatusLastErrorMaxBytes {
		t.Fatalf("last error bytes = %d, want <= %d", len(got), allocationStatusLastErrorMaxBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("last error is not valid UTF-8: %q", got)
	}

	got = boundedReporterError(errors.New("valid\xfftail"))
	if !utf8.ValidString(got) {
		t.Fatalf("sanitized last error is not valid UTF-8: %q", got)
	}
}

func TestAllocationStatusBatcherDoesNotBlockProducer(t *testing.T) {
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	var sendStartedOnce sync.Once
	batcher := newAllocationStatusBatcher(func(_ context.Context, _ []*nodev1.AllocationStatusObservation) error {
		sendStartedOnce.Do(func() { close(sendStarted) })
		<-releaseSend
		return nil
	})
	batcher.Start()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, false))
	select {
	case <-sendStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sender")
	}
	if got := batcher.UnacknowledgedAllocationIDs(); len(got) != 1 || got[0] != "alloc-1" {
		t.Fatalf("in-flight allocation ids = %#v, want [alloc-1]", got)
	}
	if health := batcher.Health(); health.Pending != 1 || !health.InFlight {
		t.Fatalf("in-flight health = %#v, want one unacknowledged observation", health)
	}
	done := make(chan struct{})
	go func() {
		batcher.Enqueue(statusObservation("alloc-2", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, false))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("enqueue blocked on in-flight sender")
	}
	if got := batcher.UnacknowledgedAllocationIDs(); len(got) != 2 || got[0] != "alloc-1" || got[1] != "alloc-2" {
		t.Fatalf("queued and in-flight allocation ids = %#v, want [alloc-1 alloc-2]", got)
	}
	close(releaseSend)
	batcher.Stop()
	if got := batcher.UnacknowledgedAllocationIDs(); len(got) != 0 {
		t.Fatalf("allocation ids after acknowledgement = %#v, want empty", got)
	}
}

func TestAllocationStatusBatcherAcknowledgementDoesNotDropNewerPendingStatus(t *testing.T) {
	batcher := newAllocationStatusBatcher(func(context.Context, []*nodev1.AllocationStatusObservation) error {
		return nil
	})
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, false))
	first := batcher.drain(1)
	if len(first) != 1 {
		t.Fatalf("first batch length = %d, want 1", len(first))
	}

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, true))
	batcher.acknowledge(first)

	if got := batcher.UnacknowledgedAllocationIDs(); len(got) != 1 || got[0] != "alloc-1" {
		t.Fatalf("allocation ids after old acknowledgement = %#v, want [alloc-1]", got)
	}
	if pending := batcher.pendingCount(); pending != 1 {
		t.Fatalf("pending after old acknowledgement = %d, want 1", pending)
	}
	latest := batcher.drain(1)
	if len(latest) != 1 || latest[0].observation.GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("latest batch = %#v, want terminal observation", latest)
	}
}

func TestAllocationStatusBatcherRetainsFirstTerminalProofForAttempt(t *testing.T) {
	batcher := newAllocationStatusBatcher(func(context.Context, []*nodev1.AllocationStatusObservation) error {
		return nil
	})
	terminal := statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, false)
	accepted, err := batcher.Enqueue(terminal)
	if err != nil || !accepted {
		t.Fatalf("Enqueue(terminal) = accepted %v, error %v", accepted, err)
	}
	accepted, err = batcher.Enqueue(terminal)
	if err != nil || !accepted {
		t.Fatalf("Enqueue(duplicate) = accepted %v, error %v", accepted, err)
	}
	conflict := statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, false)
	if accepted, err = batcher.Enqueue(conflict); err != nil || accepted {
		t.Fatalf("Enqueue(conflict) = accepted %v, error %v; want ignored", accepted, err)
	}
	if got := batcher.pendingCount(); got != 1 {
		t.Fatalf("pending count = %d, want one immutable terminal proof", got)
	}
}

func TestAllocationStatusBatcherBoundsDistinctPendingAllocations(t *testing.T) {
	batcher := newAllocationStatusBatcher(func(context.Context, []*nodev1.AllocationStatusObservation) error {
		return nil
	})
	for i := 0; i < allocationStatusQueueLimit+1; i++ {
		batcher.Enqueue(statusObservation(
			fmt.Sprintf("alloc-%d", i),
			1,
			commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			false,
		))
	}
	if got := batcher.pendingCount(); got != allocationStatusQueueLimit {
		t.Fatalf("pending count = %d, want %d", got, allocationStatusQueueLimit)
	}
	batcher.Stop()
}

func TestAllocationStatusBatcherPreservesNewTerminalAtQueueLimit(t *testing.T) {
	batcher := newAllocationStatusBatcher(func(context.Context, []*nodev1.AllocationStatusObservation) error {
		return nil
	})
	for i := range allocationStatusQueueLimit {
		batcher.Enqueue(statusObservation(
			fmt.Sprintf("alloc-%d", i),
			1,
			commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			false,
		))
	}

	batcher.Enqueue(statusObservation("terminal", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, false))

	batcher.mu.Lock()
	_, terminalPresent := batcher.pending["terminal"]
	pending := len(batcher.pending)
	batcher.mu.Unlock()
	if !terminalPresent {
		t.Fatal("terminal observation was dropped at queue limit")
	}
	if pending != allocationStatusQueueLimit {
		t.Fatalf("pending count = %d, want %d", pending, allocationStatusQueueLimit)
	}
	batcher.Stop()
}

func TestAllocationStatusBatcherCopiesEnqueuedObservation(t *testing.T) {
	batches := make(chan []*nodev1.AllocationStatusObservation, 1)
	batcher := newAllocationStatusBatcher(func(_ context.Context, observations []*nodev1.AllocationStatusObservation) error {
		batches <- observations
		return nil
	})
	observation := statusObservation("alloc-1", 1, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, false)
	batcher.Enqueue(observation)
	observation.Status = commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED
	batcher.Start()
	defer batcher.Stop()

	batch := awaitStatusBatch(t, batches)
	if got := batch[0].GetStatus(); got != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
		t.Fatalf("queued status = %v, want running", got)
	}
}

func TestAllocationStatusBatcherRejectsUnknownStatus(t *testing.T) {
	batcher := newAllocationStatusBatcher(func(context.Context, []*nodev1.AllocationStatusObservation) error {
		t.Fatal("invalid allocation status was sent")
		return nil
	})
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationStatus(999), false))
	if got := batcher.pendingCount(); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
	batcher.Stop()
}

func statusObservation(allocationID string, attempt int64, status commonv1.AllocationStatus, ready bool) *nodev1.AllocationStatusObservation {
	return &nodev1.AllocationStatusObservation{
		AllocationID: allocationID,
		Attempt:      attempt,
		Status:       status,
		Ready:        ready,
	}
}

func awaitStatusBatch(t *testing.T, batches <-chan []*nodev1.AllocationStatusObservation) []*nodev1.AllocationStatusObservation {
	t.Helper()
	select {
	case batch := <-batches:
		return batch
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for allocation status batch")
		return nil
	}
}
