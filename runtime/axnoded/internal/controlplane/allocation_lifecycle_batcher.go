package controlplane

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

const (
	allocationLifecycleBatchDelay        = 10 * time.Millisecond
	allocationLifecycleBatchLimit        = 256
	allocationLifecycleQueueLimit        = 4096
	allocationLifecycleRetryInitialDelay = 100 * time.Millisecond
	allocationLifecycleRetryMaxDelay     = 5 * time.Second
	allocationLifecycleLastErrorMaxBytes = 512
)

type allocationLifecycleBatchSender func(context.Context, []*nodev1.AllocationLifecycleObservation) error

type queuedAllocationLifecycleState struct {
	observation *nodev1.AllocationLifecycleObservation
	enqueuedAt  time.Time
	sequence    uint64
}

// AllocationLifecycleReporterHealth is the bounded process-local status reporter
// read model exposed through axnoded diagnostics. It is not durable workload
// state; controld owns admitted lifecycle state and axnoded reconstructs
// unacknowledged terminal reports from its durable node-state outbox after a
// process restart.
type AllocationLifecycleReporterHealth struct {
	Status              string     `json:"status"`
	Pending             int        `json:"pending"`
	OldestPendingAt     *time.Time `json:"oldestPendingAt,omitempty"`
	OldestPendingAgeSec float64    `json:"oldestPendingAgeSeconds"`
	InFlight            bool       `json:"inFlight"`
	LastAttemptAt       *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt       *time.Time `json:"lastSuccessAt,omitempty"`
	LastErrorAt         *time.Time `json:"lastErrorAt,omitempty"`
	LastError           string     `json:"lastError,omitempty"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	NextRetryAt         *time.Time `json:"nextRetryAt,omitempty"`
	RetryDelaySec       float64    `json:"retryDelaySeconds"`
	Stopped             bool       `json:"stopped"`
}

type allocationLifecycleBatcher struct {
	send              allocationLifecycleBatchSender
	now               func() time.Time
	jitter            func(time.Duration) time.Duration
	batchDelay        time.Duration
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration

	mu                  sync.Mutex
	pending             map[string]queuedAllocationLifecycleState
	inFlightBatch       map[string]queuedAllocationLifecycleState
	oldestPendingAt     time.Time
	sequence            uint64
	stopped             bool
	inFlight            bool
	lastAttemptAt       time.Time
	lastSuccessAt       time.Time
	lastErrorAt         time.Time
	lastError           string
	consecutiveFailures int
	nextRetryAt         time.Time
	retryDelay          time.Duration
	wake                chan struct{}
	stop                chan struct{}
	startOnce           sync.Once
	stopOnce            sync.Once
	wg                  sync.WaitGroup
}

func newAllocationLifecycleBatcher(send allocationLifecycleBatchSender) *allocationLifecycleBatcher {
	return &allocationLifecycleBatcher{
		send:              send,
		now:               func() time.Time { return time.Now().UTC() },
		jitter:            allocationLifecycleRetryJitter,
		batchDelay:        allocationLifecycleBatchDelay,
		retryInitialDelay: allocationLifecycleRetryInitialDelay,
		retryMaxDelay:     allocationLifecycleRetryMaxDelay,
		pending:           make(map[string]queuedAllocationLifecycleState),
		inFlightBatch:     make(map[string]queuedAllocationLifecycleState),
		wake:              make(chan struct{}, 1),
		stop:              make(chan struct{}),
	}
}

func (b *allocationLifecycleBatcher) Start() {
	if b == nil || b.send == nil {
		return
	}
	b.startOnce.Do(func() {
		// Publish the idle series at process start so a newly started node is
		// distinguishable from a node that does not expose reporter metrics.
		b.recordHealthMetrics()
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.run()
		}()
	})
}

func (b *allocationLifecycleBatcher) Stop() {
	if b == nil {
		return
	}
	b.Start()
	b.stopOnce.Do(func() {
		b.mu.Lock()
		b.stopped = true
		b.mu.Unlock()
		close(b.stop)
		b.wg.Wait()
	})
}

// Enqueue returns whether this observation became the current queued proof.
// A durable terminal producer uses the result to discard an obsolete outbox
// record or retry a queue-capacity failure without crossing its exit barrier.
func (b *allocationLifecycleBatcher) Enqueue(observation *nodev1.AllocationLifecycleObservation) (bool, error) {
	if b == nil || observation == nil {
		return false, errors.New("allocation lifecycle batcher and observation are required")
	}
	allocationID := strings.TrimSpace(observation.GetAllocationID())
	if allocationID == "" || !allocationLifecycleObservationValid(observation.GetState()) {
		metrics.RecordAllocationLifecycleQueueEvent("invalid")
		return false, errors.New("allocation lifecycle observation is invalid")
	}
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		metrics.RecordAllocationLifecycleQueueEvent("stopped")
		return false, errors.New("allocation lifecycle batcher is stopped")
	}
	b.sequence++
	next := queuedAllocationLifecycleState{
		observation: proto.Clone(observation).(*nodev1.AllocationLifecycleObservation),
		enqueuedAt:  b.now(),
		sequence:    b.sequence,
	}
	next.observation.AllocationID = allocationID
	current, exists := b.pending[allocationID]
	inFlightCurrent, inFlight := b.inFlightBatch[allocationID]
	if exists && sameTerminalProof(next.observation, current.observation) {
		b.mu.Unlock()
		metrics.RecordAllocationLifecycleQueueEvent("retained")
		return true, nil
	}
	if exists && conflictingTerminalProof(next.observation, current.observation) {
		b.mu.Unlock()
		metrics.RecordAllocationLifecycleQueueEvent("ignored")
		return false, nil
	}
	if !exists && inFlight && sameTerminalProof(next.observation, inFlightCurrent.observation) {
		b.mu.Unlock()
		metrics.RecordAllocationLifecycleQueueEvent("retained")
		return true, nil
	}
	if !exists && inFlight && conflictingTerminalProof(next.observation, inFlightCurrent.observation) {
		b.mu.Unlock()
		metrics.RecordAllocationLifecycleQueueEvent("ignored")
		return false, nil
	}
	evictedNonterminal := false
	if !exists && !inFlight && b.unacknowledgedCountLocked() >= allocationLifecycleQueueLimit {
		if !allocationLifecycleStopped(next.observation.GetState()) || !b.evictOldestNonterminalLocked() {
			b.mu.Unlock()
			metrics.RecordAllocationLifecycleQueueEvent("dropped")
			return false, errors.New("allocation lifecycle queue is full")
		}
		evictedNonterminal = true
	}
	result := "ignored"
	if !exists || allocationLifecycleSupersedes(next, current) {
		if exists && current.enqueuedAt.Before(next.enqueuedAt) {
			next.enqueuedAt = current.enqueuedAt
		}
		b.pending[allocationID] = next
		b.updateOldestPendingAtLocked(next.enqueuedAt)
		result = "accepted"
		if exists {
			result = "coalesced"
		}
	}
	pending := b.unacknowledgedCountLocked()
	b.mu.Unlock()
	if evictedNonterminal {
		metrics.RecordAllocationLifecycleQueueEvent("evicted_nonterminal")
	}
	metrics.RecordAllocationLifecycleQueueEvent(result)
	metrics.RecordAllocationLifecycleQueueCurrent(pending)
	b.recordHealthMetrics()
	if result == "ignored" {
		return false, nil
	}
	b.signal()
	return true, nil
}

func (b *allocationLifecycleBatcher) evictOldestNonterminalLocked() bool {
	var oldestID string
	var oldest queuedAllocationLifecycleState
	for allocationID, item := range b.pending {
		if allocationLifecycleStopped(item.observation.GetState()) {
			continue
		}
		if oldestID == "" || item.sequence < oldest.sequence {
			oldestID = allocationID
			oldest = item
		}
	}
	if oldestID == "" {
		return false
	}
	delete(b.pending, oldestID)
	b.recomputeOldestPendingAtLocked()
	return true
}

func (b *allocationLifecycleBatcher) run() {
	for {
		select {
		case <-b.wake:
			if !b.wait(b.batchDelay) {
				b.flushOnStop()
				return
			}
			var retryBase time.Duration
			for {
				processed, err := b.flushOne()
				if !processed {
					break
				}
				if err == nil {
					retryBase = 0
					b.clearRetry()
					continue
				}
				retryBase = nextAllocationLifecycleStateRetryDelay(retryBase, b.retryInitialDelay, b.retryMaxDelay)
				delay := retryBase
				if b.jitter != nil {
					delay = b.jitter(retryBase)
				}
				b.scheduleRetry(delay)
				if !b.wait(delay) {
					b.clearRetry()
					b.flushOnStop()
					return
				}
				b.clearRetry()
			}
		case <-b.stop:
			b.flushOnStop()
			return
		}
	}
}

func (b *allocationLifecycleBatcher) flushOne() (bool, error) {
	batch := b.drain(allocationLifecycleBatchLimit)
	if len(batch) == 0 {
		return false, nil
	}
	observations := make([]*nodev1.AllocationLifecycleObservation, 0, len(batch))
	for _, item := range batch {
		observations = append(observations, item.observation)
	}
	err := b.sendBatch(observations)
	b.recordBatchResult(batch, err)
	if err != nil {
		b.requeue(batch)
	} else {
		b.acknowledge(batch)
	}
	return true, err
}

func (b *allocationLifecycleBatcher) flushOnStop() {
	for {
		batch := b.drain(allocationLifecycleBatchLimit)
		if len(batch) == 0 {
			return
		}
		observations := make([]*nodev1.AllocationLifecycleObservation, 0, len(batch))
		for _, item := range batch {
			observations = append(observations, item.observation)
		}
		err := b.sendBatch(observations)
		b.recordBatchResult(batch, err)
		if err != nil {
			b.requeue(batch)
			return
		}
		b.acknowledge(batch)
	}
}

func (b *allocationLifecycleBatcher) sendBatch(observations []*nodev1.AllocationLifecycleObservation) error {
	startedAt := b.now()
	b.mu.Lock()
	b.inFlight = true
	b.lastAttemptAt = startedAt
	b.mu.Unlock()
	b.recordHealthMetrics()

	ctx, cancel := context.WithTimeout(context.Background(), reporterRPCTimeout)
	err := b.send(ctx, observations)
	cancel()
	completedAt := b.now()
	b.mu.Lock()
	b.inFlight = false
	if err == nil {
		b.lastSuccessAt = completedAt
		b.consecutiveFailures = 0
	} else {
		b.lastErrorAt = completedAt
		b.lastError = boundedReporterError(err)
		b.consecutiveFailures++
	}
	b.mu.Unlock()
	b.recordHealthMetrics()
	return err
}

func (b *allocationLifecycleBatcher) drain(limit int) []queuedAllocationLifecycleState {
	b.mu.Lock()
	if len(b.pending) == 0 || len(b.inFlightBatch) != 0 {
		b.mu.Unlock()
		return nil
	}
	ids := make([]string, 0, len(b.pending))
	for allocationID := range b.pending {
		ids = append(ids, allocationID)
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]queuedAllocationLifecycleState, 0, len(ids))
	for _, allocationID := range ids {
		item := b.pending[allocationID]
		out = append(out, item)
		b.inFlightBatch[allocationID] = item
		delete(b.pending, allocationID)
	}
	b.recomputeOldestPendingAtLocked()
	pending := b.unacknowledgedCountLocked()
	b.mu.Unlock()
	metrics.RecordAllocationLifecycleQueueCurrent(pending)
	return out
}

func (b *allocationLifecycleBatcher) requeue(batch []queuedAllocationLifecycleState) {
	b.mu.Lock()
	for _, failed := range batch {
		allocationID := failed.observation.GetAllocationID()
		b.removeInFlightLocked(allocationID, failed.sequence)
		current, ok := b.pending[allocationID]
		if !ok || allocationLifecycleSupersedes(failed, current) {
			b.pending[allocationID] = failed
			b.updateOldestPendingAtLocked(failed.enqueuedAt)
			continue
		}
		if failed.enqueuedAt.Before(current.enqueuedAt) {
			current.enqueuedAt = failed.enqueuedAt
			b.pending[allocationID] = current
			b.updateOldestPendingAtLocked(current.enqueuedAt)
		}
	}
	b.recomputeOldestPendingAtLocked()
	pending := b.unacknowledgedCountLocked()
	b.mu.Unlock()
	metrics.RecordAllocationLifecycleQueueCurrent(pending)
	b.recordHealthMetrics()
}

func (b *allocationLifecycleBatcher) acknowledge(batch []queuedAllocationLifecycleState) {
	b.mu.Lock()
	for _, sent := range batch {
		allocationID := sent.observation.GetAllocationID()
		current, ok := b.pending[allocationID]
		if ok && allocationLifecycleSupersedes(sent, current) {
			delete(b.pending, allocationID)
		}
		b.removeInFlightLocked(allocationID, sent.sequence)
	}
	b.recomputeOldestPendingAtLocked()
	pending := b.unacknowledgedCountLocked()
	b.mu.Unlock()
	metrics.RecordAllocationLifecycleQueueCurrent(pending)
	b.recordHealthMetrics()
}

func (b *allocationLifecycleBatcher) recordBatchResult(batch []queuedAllocationLifecycleState, err error) {
	if len(batch) == 0 {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	oldest := batch[0].enqueuedAt
	for _, item := range batch[1:] {
		if item.enqueuedAt.Before(oldest) {
			oldest = item.enqueuedAt
		}
	}
	metrics.RecordAllocationLifecycleBatch(result, len(batch))
	metrics.RecordAllocationLifecycleQueueWait(result, b.now().Sub(oldest).Seconds())
}

func (b *allocationLifecycleBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.unacknowledgedCountLocked()
}

// UnacknowledgedAllocationIDs returns allocation identities whose latest
// lifecycle observation has not yet received a successful control-plane RPC
// acknowledgement. Node inventory keeps these identities active so a
// short-lived allocation cannot disappear before its terminal evidence is
// durably visible to controld.
func (b *allocationLifecycleBatcher) UnacknowledgedAllocationIDs() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, b.unacknowledgedCountLocked())
	seen := make(map[string]struct{}, b.unacknowledgedCountLocked())
	for allocationID := range b.pending {
		seen[allocationID] = struct{}{}
	}
	for allocationID := range b.inFlightBatch {
		seen[allocationID] = struct{}{}
	}
	for allocationID := range seen {
		ids = append(ids, allocationID)
	}
	sort.Strings(ids)
	return ids
}

func (b *allocationLifecycleBatcher) Health() AllocationLifecycleReporterHealth {
	if b == nil {
		return AllocationLifecycleReporterHealth{Status: "disabled"}
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	health := AllocationLifecycleReporterHealth{
		Pending:             b.unacknowledgedCountLocked(),
		InFlight:            b.inFlight,
		LastAttemptAt:       timePointer(b.lastAttemptAt),
		LastSuccessAt:       timePointer(b.lastSuccessAt),
		LastErrorAt:         timePointer(b.lastErrorAt),
		LastError:           b.lastError,
		ConsecutiveFailures: b.consecutiveFailures,
		NextRetryAt:         timePointer(b.nextRetryAt),
		RetryDelaySec:       b.retryDelay.Seconds(),
		Stopped:             b.stopped,
	}
	if !b.oldestPendingAt.IsZero() {
		health.OldestPendingAt = timePointer(b.oldestPendingAt)
		health.OldestPendingAgeSec = max(0, now.Sub(b.oldestPendingAt).Seconds())
	}
	switch {
	case b.stopped:
		health.Status = "stopped"
	case b.inFlight:
		health.Status = "sending"
	case !b.nextRetryAt.IsZero():
		health.Status = "retrying"
	case b.unacknowledgedCountLocked() > 0:
		health.Status = "queued"
	default:
		health.Status = "idle"
	}
	return health
}

func (b *allocationLifecycleBatcher) scheduleRetry(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	b.mu.Lock()
	b.retryDelay = delay
	b.nextRetryAt = b.now().Add(delay)
	b.mu.Unlock()
	b.recordHealthMetrics()
}

func (b *allocationLifecycleBatcher) clearRetry() {
	b.mu.Lock()
	b.retryDelay = 0
	b.nextRetryAt = time.Time{}
	b.mu.Unlock()
	b.recordHealthMetrics()
}

func (b *allocationLifecycleBatcher) recordHealthMetrics() {
	health := b.Health()
	metrics.RecordAllocationLifecycleReporterHealth(
		health.OldestPendingAgeSec,
		health.ConsecutiveFailures,
		health.RetryDelaySec,
	)
}

func (b *allocationLifecycleBatcher) updateOldestPendingAtLocked(candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	if b.oldestPendingAt.IsZero() || candidate.Before(b.oldestPendingAt) {
		b.oldestPendingAt = candidate
	}
}

func (b *allocationLifecycleBatcher) recomputeOldestPendingAtLocked() {
	b.oldestPendingAt = time.Time{}
	for _, item := range b.pending {
		b.updateOldestPendingAtLocked(item.enqueuedAt)
	}
	for _, item := range b.inFlightBatch {
		b.updateOldestPendingAtLocked(item.enqueuedAt)
	}
}

func (b *allocationLifecycleBatcher) unacknowledgedCountLocked() int {
	count := len(b.inFlightBatch)
	for allocationID := range b.pending {
		if _, exists := b.inFlightBatch[allocationID]; !exists {
			count++
		}
	}
	return count
}

func (b *allocationLifecycleBatcher) removeInFlightLocked(allocationID string, sequence uint64) {
	current, exists := b.inFlightBatch[allocationID]
	if exists && current.sequence == sequence {
		delete(b.inFlightBatch, allocationID)
	}
}

func (b *allocationLifecycleBatcher) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *allocationLifecycleBatcher) wait(delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-b.stop:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-b.stop:
		return false
	}
}

func nextAllocationLifecycleStateRetryDelay(previous, initial, maximum time.Duration) time.Duration {
	if initial <= 0 {
		initial = allocationLifecycleRetryInitialDelay
	}
	if maximum < initial {
		maximum = initial
	}
	if previous <= 0 {
		return initial
	}
	if previous >= maximum || previous > maximum/2 {
		return maximum
	}
	return previous * 2
}

func allocationLifecycleRetryJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	spread := base / 5
	if spread <= 0 {
		return base
	}
	return base - spread + time.Duration(rand.Int63n(int64(2*spread)+1))
}

func boundedReporterError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToValidUTF8(strings.TrimSpace(err.Error()), "\uFFFD")
	if len(message) <= allocationLifecycleLastErrorMaxBytes {
		return message
	}
	limit := allocationLifecycleLastErrorMaxBytes
	for limit > 0 && !utf8.ValidString(message[:limit]) {
		limit--
	}
	return message[:limit]
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func allocationLifecycleSupersedes(next, current queuedAllocationLifecycleState) bool {
	nextEnded := allocationLifecycleStopped(next.observation.GetState())
	currentEnded := allocationLifecycleStopped(current.observation.GetState())
	if nextEnded != currentEnded {
		return nextEnded
	}
	return next.sequence > current.sequence
}

func sameTerminalProof(left, right *nodev1.AllocationLifecycleObservation) bool {
	return left != nil && right != nil &&
		allocationLifecycleStopped(left.GetState()) &&
		allocationLifecycleStopped(right.GetState()) &&
		proto.Equal(left, right)
}

func conflictingTerminalProof(left, right *nodev1.AllocationLifecycleObservation) bool {
	return left != nil && right != nil &&
		allocationLifecycleStopped(left.GetState()) &&
		allocationLifecycleStopped(right.GetState()) &&
		!proto.Equal(left, right)
}

func allocationLifecycleStopped(state commonv1.AllocationLifecycleState) bool {
	return state == commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED
}

func allocationLifecycleObservationValid(state commonv1.AllocationLifecycleState) bool {
	switch state {
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED:
		return true
	default:
		return false
	}
}
