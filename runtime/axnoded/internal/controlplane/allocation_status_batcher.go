package controlplane

import (
	"context"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

const (
	allocationStatusBatchDelay        = 10 * time.Millisecond
	allocationStatusBatchLimit        = 256
	allocationStatusQueueLimit        = 4096
	allocationStatusRetryInitialDelay = 100 * time.Millisecond
	allocationStatusRetryMaxDelay     = 5 * time.Second
	allocationStatusLastErrorMaxBytes = 512
)

type allocationStatusBatchSender func(context.Context, []*nodev1.AllocationStatusObservation) error

type queuedAllocationStatus struct {
	observation *nodev1.AllocationStatusObservation
	enqueuedAt  time.Time
	sequence    uint64
}

// AllocationStatusReporterHealth is the bounded process-local status reporter
// read model exposed through axnoded diagnostics. It is not durable workload
// state; controld and node inventory remain responsible for convergence after
// a node process restart.
type AllocationStatusReporterHealth struct {
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

type allocationStatusBatcher struct {
	send              allocationStatusBatchSender
	now               func() time.Time
	jitter            func(time.Duration) time.Duration
	batchDelay        time.Duration
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration

	mu                  sync.Mutex
	pending             map[string]queuedAllocationStatus
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

func newAllocationStatusBatcher(send allocationStatusBatchSender) *allocationStatusBatcher {
	return &allocationStatusBatcher{
		send:              send,
		now:               func() time.Time { return time.Now().UTC() },
		jitter:            allocationStatusRetryJitter,
		batchDelay:        allocationStatusBatchDelay,
		retryInitialDelay: allocationStatusRetryInitialDelay,
		retryMaxDelay:     allocationStatusRetryMaxDelay,
		pending:           make(map[string]queuedAllocationStatus),
		wake:              make(chan struct{}, 1),
		stop:              make(chan struct{}),
	}
}

func (b *allocationStatusBatcher) Start() {
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

func (b *allocationStatusBatcher) Stop() {
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

func (b *allocationStatusBatcher) Enqueue(observation *nodev1.AllocationStatusObservation) {
	if b == nil || observation == nil {
		return
	}
	allocationID := strings.TrimSpace(observation.GetAllocationID())
	if allocationID == "" || observation.GetAttempt() <= 0 || !allocationStatusValid(observation.GetStatus()) {
		metrics.RecordAllocationStatusQueueEvent("invalid")
		return
	}
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		metrics.RecordAllocationStatusQueueEvent("stopped")
		return
	}
	b.sequence++
	next := queuedAllocationStatus{
		observation: proto.Clone(observation).(*nodev1.AllocationStatusObservation),
		enqueuedAt:  b.now(),
		sequence:    b.sequence,
	}
	current, exists := b.pending[allocationID]
	evictedNonterminal := false
	if !exists && len(b.pending) >= allocationStatusQueueLimit {
		if !allocationStatusEnded(next.observation.GetStatus()) || !b.evictOldestNonterminalLocked() {
			b.mu.Unlock()
			metrics.RecordAllocationStatusQueueEvent("dropped")
			return
		}
		evictedNonterminal = true
	}
	result := "ignored"
	if !exists || allocationStatusSupersedes(next, current) {
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
	pending := len(b.pending)
	b.mu.Unlock()
	if evictedNonterminal {
		metrics.RecordAllocationStatusQueueEvent("evicted_nonterminal")
	}
	metrics.RecordAllocationStatusQueueEvent(result)
	metrics.RecordAllocationStatusQueueCurrent(pending)
	b.recordHealthMetrics()
	if result == "ignored" {
		return
	}
	b.signal()
}

func (b *allocationStatusBatcher) evictOldestNonterminalLocked() bool {
	var oldestID string
	var oldest queuedAllocationStatus
	for allocationID, item := range b.pending {
		if allocationStatusEnded(item.observation.GetStatus()) {
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

func (b *allocationStatusBatcher) run() {
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
				retryBase = nextAllocationStatusRetryDelay(retryBase, b.retryInitialDelay, b.retryMaxDelay)
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

func (b *allocationStatusBatcher) flushOne() (bool, error) {
	batch := b.drain(allocationStatusBatchLimit)
	if len(batch) == 0 {
		return false, nil
	}
	observations := make([]*nodev1.AllocationStatusObservation, 0, len(batch))
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

func (b *allocationStatusBatcher) flushOnStop() {
	for {
		batch := b.drain(allocationStatusBatchLimit)
		if len(batch) == 0 {
			return
		}
		observations := make([]*nodev1.AllocationStatusObservation, 0, len(batch))
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

func (b *allocationStatusBatcher) sendBatch(observations []*nodev1.AllocationStatusObservation) error {
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

func (b *allocationStatusBatcher) drain(limit int) []queuedAllocationStatus {
	b.mu.Lock()
	if len(b.pending) == 0 {
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
	out := make([]queuedAllocationStatus, 0, len(ids))
	for _, allocationID := range ids {
		out = append(out, b.pending[allocationID])
		delete(b.pending, allocationID)
	}
	b.recomputeOldestPendingAtLocked()
	pending := len(b.pending)
	b.mu.Unlock()
	metrics.RecordAllocationStatusQueueCurrent(pending)
	return out
}

func (b *allocationStatusBatcher) requeue(batch []queuedAllocationStatus) {
	b.mu.Lock()
	for _, failed := range batch {
		allocationID := failed.observation.GetAllocationID()
		current, ok := b.pending[allocationID]
		if !ok || allocationStatusSupersedes(failed, current) {
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
	pending := len(b.pending)
	b.mu.Unlock()
	metrics.RecordAllocationStatusQueueCurrent(pending)
	b.recordHealthMetrics()
}

func (b *allocationStatusBatcher) acknowledge(batch []queuedAllocationStatus) {
	b.mu.Lock()
	for _, sent := range batch {
		allocationID := sent.observation.GetAllocationID()
		current, ok := b.pending[allocationID]
		if ok && allocationStatusSupersedes(sent, current) {
			delete(b.pending, allocationID)
		}
	}
	b.recomputeOldestPendingAtLocked()
	pending := len(b.pending)
	b.mu.Unlock()
	metrics.RecordAllocationStatusQueueCurrent(pending)
	b.recordHealthMetrics()
}

func (b *allocationStatusBatcher) recordBatchResult(batch []queuedAllocationStatus, err error) {
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
	metrics.RecordAllocationStatusBatch(result, len(batch))
	metrics.RecordAllocationStatusQueueWait(result, b.now().Sub(oldest).Seconds())
}

func (b *allocationStatusBatcher) pendingCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}

func (b *allocationStatusBatcher) Health() AllocationStatusReporterHealth {
	if b == nil {
		return AllocationStatusReporterHealth{Status: "disabled"}
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	health := AllocationStatusReporterHealth{
		Pending:             len(b.pending),
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
	case len(b.pending) > 0:
		health.Status = "queued"
	default:
		health.Status = "idle"
	}
	return health
}

func (b *allocationStatusBatcher) scheduleRetry(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	b.mu.Lock()
	b.retryDelay = delay
	b.nextRetryAt = b.now().Add(delay)
	b.mu.Unlock()
	b.recordHealthMetrics()
}

func (b *allocationStatusBatcher) clearRetry() {
	b.mu.Lock()
	b.retryDelay = 0
	b.nextRetryAt = time.Time{}
	b.mu.Unlock()
	b.recordHealthMetrics()
}

func (b *allocationStatusBatcher) recordHealthMetrics() {
	health := b.Health()
	metrics.RecordAllocationStatusReporterHealth(
		health.OldestPendingAgeSec,
		health.ConsecutiveFailures,
		health.RetryDelaySec,
	)
}

func (b *allocationStatusBatcher) updateOldestPendingAtLocked(candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	if b.oldestPendingAt.IsZero() || candidate.Before(b.oldestPendingAt) {
		b.oldestPendingAt = candidate
	}
}

func (b *allocationStatusBatcher) recomputeOldestPendingAtLocked() {
	b.oldestPendingAt = time.Time{}
	for _, item := range b.pending {
		b.updateOldestPendingAtLocked(item.enqueuedAt)
	}
}

func (b *allocationStatusBatcher) signal() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *allocationStatusBatcher) wait(delay time.Duration) bool {
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

func nextAllocationStatusRetryDelay(previous, initial, maximum time.Duration) time.Duration {
	if initial <= 0 {
		initial = allocationStatusRetryInitialDelay
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

func allocationStatusRetryJitter(base time.Duration) time.Duration {
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
	if len(message) <= allocationStatusLastErrorMaxBytes {
		return message
	}
	limit := allocationStatusLastErrorMaxBytes
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

func allocationStatusSupersedes(next, current queuedAllocationStatus) bool {
	if next.observation.GetAttempt() != current.observation.GetAttempt() {
		return next.observation.GetAttempt() > current.observation.GetAttempt()
	}
	nextEnded := allocationStatusEnded(next.observation.GetStatus())
	currentEnded := allocationStatusEnded(current.observation.GetStatus())
	if nextEnded != currentEnded {
		return nextEnded
	}
	return next.sequence > current.sequence
}

func allocationStatusEnded(status commonv1.AllocationStatus) bool {
	switch status {
	case commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED:
		return true
	default:
		return false
	}
}

func allocationStatusValid(status commonv1.AllocationStatus) bool {
	if status == commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED {
		return false
	}
	_, known := commonv1.AllocationStatus_name[int32(status)]
	return known
}
