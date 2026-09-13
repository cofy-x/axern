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

func TestAllocationLifecycleStateBatcherCoalescesLatestObservation(t *testing.T) {
	batches := make(chan []*nodev1.AllocationLifecycleObservation, 1)
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, observations []*nodev1.AllocationLifecycleObservation) error {
		batches <- observations
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, false))
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, true))

	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || !batch[0].GetReady() {
		t.Fatalf("batch = %#v, want one ready observation", batch)
	}
}

func TestAllocationLifecycleStateBatcherPreservesTerminalObservation(t *testing.T) {
	batches := make(chan []*nodev1.AllocationLifecycleObservation, 1)
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, observations []*nodev1.AllocationLifecycleObservation) error {
		batches <- observations
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false))
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, true))

	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || batch[0].GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
		t.Fatalf("batch = %#v, want terminal observation", batch)
	}
}

func TestAllocationLifecycleStateBatcherRetriesFailedBatch(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	batches := make(chan []*nodev1.AllocationLifecycleObservation, 1)
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, observations []*nodev1.AllocationLifecycleObservation) error {
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

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, true))
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

func TestAllocationLifecycleStateBatcherRetryKeepsTerminalOverConcurrentNonterminal(t *testing.T) {
	firstSend := make(chan struct{})
	releaseFirstSend := make(chan struct{})
	batches := make(chan []*nodev1.AllocationLifecycleObservation, 1)
	var calls int
	var mu sync.Mutex
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, observations []*nodev1.AllocationLifecycleObservation) error {
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

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false))
	select {
	case <-firstSend:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first send")
	}
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, true))
	close(releaseFirstSend)

	batch := awaitStatusBatch(t, batches)
	if len(batch) != 1 || batch[0].GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
		t.Fatalf("retried batch = %#v, want terminal observation", batch)
	}
}

func TestAllocationLifecycleStateBatcherSuccessKeepsTerminalOverConcurrentNonterminal(t *testing.T) {
	firstSend := make(chan struct{})
	releaseFirstSend := make(chan struct{})
	batches := make(chan []*nodev1.AllocationLifecycleObservation, 2)
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, observations []*nodev1.AllocationLifecycleObservation) error {
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

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false))
	select {
	case <-firstSend:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first send")
	}
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, true))
	close(releaseFirstSend)

	first := awaitStatusBatch(t, batches)
	if len(first) != 1 || first[0].GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
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

func TestAllocationLifecycleStateBatcherStartPublishesIdleHealthMetrics(t *testing.T) {
	metrics.ResetForTest()
	batcher := newAllocationLifecycleBatcher(func(context.Context, []*nodev1.AllocationLifecycleObservation) error {
		return nil
	})
	batcher.Start()
	defer batcher.Stop()

	want := map[string]bool{
		metrics.MetricAllocationLifecycleOldestPendingAge:    false,
		metrics.MetricAllocationLifecycleConsecutiveFailures: false,
		metrics.MetricAllocationLifecycleRetryDelay:          false,
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

func TestAllocationLifecycleStateBatcherBacksOffWithoutNewEventBypass(t *testing.T) {
	metrics.ResetForTest()
	calls := make(chan time.Time, 8)
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, _ []*nodev1.AllocationLifecycleObservation) error {
		calls <- time.Now()
		return errors.New("control plane unavailable")
	})
	batcher.batchDelay = 0
	batcher.retryInitialDelay = 120 * time.Millisecond
	batcher.retryMaxDelay = 240 * time.Millisecond
	batcher.jitter = func(delay time.Duration) time.Duration { return delay }
	batcher.Start()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, true))
	first := <-calls
	batcher.Enqueue(statusObservation("alloc-2", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false))

	deadline := time.Now().Add(time.Second)
	var health AllocationLifecycleReporterHealth
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
	if got := metrics.GaugeValueForTest(metrics.MetricAllocationLifecycleConsecutiveFailures, nil); got != 1 {
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

func TestNextAllocationLifecycleStateRetryDelayIsBounded(t *testing.T) {
	initial := 100 * time.Millisecond
	maximum := 800 * time.Millisecond
	got := make([]time.Duration, 0, 6)
	var previous time.Duration
	for range 6 {
		previous = nextAllocationLifecycleStateRetryDelay(previous, initial, maximum)
		got = append(got, previous)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 800 * time.Millisecond, 800 * time.Millisecond}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("retry delay[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestAllocationLifecycleStateRetryJitterIsBounded(t *testing.T) {
	base := time.Second
	for range 1000 {
		got := allocationLifecycleRetryJitter(base)
		if got < 800*time.Millisecond || got > 1200*time.Millisecond {
			t.Fatalf("jittered delay = %s, want [800ms, 1.2s]", got)
		}
	}
}

func TestBoundedReporterErrorLimitsUTF8Bytes(t *testing.T) {
	got := boundedReporterError(errors.New(strings.Repeat("错", allocationLifecycleLastErrorMaxBytes)))
	if len(got) > allocationLifecycleLastErrorMaxBytes {
		t.Fatalf("last error bytes = %d, want <= %d", len(got), allocationLifecycleLastErrorMaxBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("last error is not valid UTF-8: %q", got)
	}

	got = boundedReporterError(errors.New("valid\xfftail"))
	if !utf8.ValidString(got) {
		t.Fatalf("sanitized last error is not valid UTF-8: %q", got)
	}
}

func TestAllocationLifecycleStateBatcherDoesNotBlockProducer(t *testing.T) {
	sendStarted := make(chan struct{})
	releaseSend := make(chan struct{})
	var sendStartedOnce sync.Once
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, _ []*nodev1.AllocationLifecycleObservation) error {
		sendStartedOnce.Do(func() { close(sendStarted) })
		<-releaseSend
		return nil
	})
	batcher.Start()

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, false))
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
		batcher.Enqueue(statusObservation("alloc-2", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, false))
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

func TestAllocationLifecycleStateBatcherAcknowledgementDoesNotDropNewerPendingStatus(t *testing.T) {
	batcher := newAllocationLifecycleBatcher(func(context.Context, []*nodev1.AllocationLifecycleObservation) error {
		return nil
	})
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, false))
	first := batcher.drain(1)
	if len(first) != 1 {
		t.Fatalf("first batch length = %d, want 1", len(first))
	}

	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, true))
	batcher.acknowledge(first)

	if got := batcher.UnacknowledgedAllocationIDs(); len(got) != 1 || got[0] != "alloc-1" {
		t.Fatalf("allocation ids after old acknowledgement = %#v, want [alloc-1]", got)
	}
	if pending := batcher.pendingCount(); pending != 1 {
		t.Fatalf("pending after old acknowledgement = %d, want 1", pending)
	}
	latest := batcher.drain(1)
	if len(latest) != 1 || latest[0].observation.GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
		t.Fatalf("latest batch = %#v, want terminal observation", latest)
	}
}

func TestAllocationLifecycleStateBatcherRetainsFirstTerminalProofForAllocation(t *testing.T) {
	batcher := newAllocationLifecycleBatcher(func(context.Context, []*nodev1.AllocationLifecycleObservation) error {
		return nil
	})
	terminal := statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false)
	accepted, err := batcher.Enqueue(terminal)
	if err != nil || !accepted {
		t.Fatalf("Enqueue(terminal) = accepted %v, error %v", accepted, err)
	}
	accepted, err = batcher.Enqueue(terminal)
	if err != nil || !accepted {
		t.Fatalf("Enqueue(duplicate) = accepted %v, error %v", accepted, err)
	}
	conflict := statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false)
	conflict.Message = "conflicting terminal proof"
	if accepted, err = batcher.Enqueue(conflict); err != nil || accepted {
		t.Fatalf("Enqueue(conflict) = accepted %v, error %v; want ignored", accepted, err)
	}
	if got := batcher.pendingCount(); got != 1 {
		t.Fatalf("pending count = %d, want one immutable terminal proof", got)
	}
}

func TestAllocationLifecycleStateBatcherBoundsDistinctPendingAllocations(t *testing.T) {
	batcher := newAllocationLifecycleBatcher(func(context.Context, []*nodev1.AllocationLifecycleObservation) error {
		return nil
	})
	for i := 0; i < allocationLifecycleQueueLimit+1; i++ {
		batcher.Enqueue(statusObservation(
			fmt.Sprintf("alloc-%d", i),
			1,
			commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
			false,
		))
	}
	if got := batcher.pendingCount(); got != allocationLifecycleQueueLimit {
		t.Fatalf("pending count = %d, want %d", got, allocationLifecycleQueueLimit)
	}
	batcher.Stop()
}

func TestAllocationLifecycleStateBatcherPreservesNewTerminalAtQueueLimit(t *testing.T) {
	batcher := newAllocationLifecycleBatcher(func(context.Context, []*nodev1.AllocationLifecycleObservation) error {
		return nil
	})
	for i := range allocationLifecycleQueueLimit {
		batcher.Enqueue(statusObservation(
			fmt.Sprintf("alloc-%d", i),
			1,
			commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
			false,
		))
	}

	batcher.Enqueue(statusObservation("terminal", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, false))

	batcher.mu.Lock()
	_, terminalPresent := batcher.pending["terminal"]
	pending := len(batcher.pending)
	batcher.mu.Unlock()
	if !terminalPresent {
		t.Fatal("terminal observation was dropped at queue limit")
	}
	if pending != allocationLifecycleQueueLimit {
		t.Fatalf("pending count = %d, want %d", pending, allocationLifecycleQueueLimit)
	}
	batcher.Stop()
}

func TestAllocationLifecycleStateBatcherCopiesEnqueuedObservation(t *testing.T) {
	batches := make(chan []*nodev1.AllocationLifecycleObservation, 1)
	batcher := newAllocationLifecycleBatcher(func(_ context.Context, observations []*nodev1.AllocationLifecycleObservation) error {
		batches <- observations
		return nil
	})
	observation := statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, false)
	batcher.Enqueue(observation)
	observation.State = commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_UNSPECIFIED
	batcher.Start()
	defer batcher.Stop()

	batch := awaitStatusBatch(t, batches)
	if got := batch[0].GetState(); got != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE {
		t.Fatalf("queued lifecycle observation = %v, want active", got)
	}
}

func TestAllocationLifecycleStateBatcherRejectsUnknownStatus(t *testing.T) {
	batcher := newAllocationLifecycleBatcher(func(context.Context, []*nodev1.AllocationLifecycleObservation) error {
		t.Fatal("invalid allocation lifecycle was sent")
		return nil
	})
	batcher.Enqueue(statusObservation("alloc-1", 1, commonv1.AllocationLifecycleState(999), false))
	if got := batcher.pendingCount(); got != 0 {
		t.Fatalf("pending count = %d, want 0", got)
	}
	batcher.Stop()
}

func statusObservation(allocationID string, attempt int64, status commonv1.AllocationLifecycleState, ready bool) *nodev1.AllocationLifecycleObservation {
	_ = attempt
	return &nodev1.AllocationLifecycleObservation{
		AllocationID: allocationID,
		State:        status,
		Ready:        ready,
	}
}

func awaitStatusBatch(t *testing.T, batches <-chan []*nodev1.AllocationLifecycleObservation) []*nodev1.AllocationLifecycleObservation {
	t.Helper()
	select {
	case batch := <-batches:
		return batch
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for allocation lifecycle batch")
		return nil
	}
}
