package controlplane

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

type allocationConditionBatchSender func(context.Context, []*nodev1.AllocationCapabilityConditionReport) error

type allocationConditionBatcher struct {
	send              allocationConditionBatchSender
	now               func() time.Time
	batchDelay        time.Duration
	retryInitialDelay time.Duration
	retryMaxDelay     time.Duration
	jitter            func(time.Duration) time.Duration

	mu        sync.Mutex
	pending   map[string]*nodev1.AllocationCapabilityConditionReport
	stopped   bool
	wake      chan struct{}
	stop      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

func newAllocationConditionBatcher(send allocationConditionBatchSender) *allocationConditionBatcher {
	return &allocationConditionBatcher{
		send: send, now: func() time.Time { return time.Now().UTC() },
		batchDelay: allocationStatusBatchDelay, retryInitialDelay: allocationStatusRetryInitialDelay,
		retryMaxDelay: allocationStatusRetryMaxDelay, jitter: allocationStatusRetryJitter,
		pending: make(map[string]*nodev1.AllocationCapabilityConditionReport),
		wake:    make(chan struct{}, 1), stop: make(chan struct{}),
	}
}

func (b *allocationConditionBatcher) Start() {
	if b == nil || b.send == nil {
		return
	}
	b.startOnce.Do(func() {
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.run()
		}()
	})
}

func (b *allocationConditionBatcher) Stop() {
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

func (b *allocationConditionBatcher) Enqueue(report *nodev1.AllocationCapabilityConditionReport) {
	if b == nil || report == nil {
		return
	}
	allocationID := strings.TrimSpace(report.GetAllocationID())
	if allocationID == "" || report.GetAttempt() <= 0 || capabilitycontract.ValidateConditionSet(report.GetConditionSet(), b.now()) != nil {
		return
	}
	next := proto.Clone(report).(*nodev1.AllocationCapabilityConditionReport)
	next.AllocationID = allocationID
	b.mu.Lock()
	if b.stopped {
		b.mu.Unlock()
		return
	}
	current := b.pending[allocationID]
	if !conditionReportSupersedes(next, current) {
		b.mu.Unlock()
		return
	}
	b.pending[allocationID] = next
	b.mu.Unlock()
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

func (b *allocationConditionBatcher) run() {
	for {
		select {
		case <-b.wake:
			if !b.wait(b.batchDelay) {
				b.flushOnce()
				return
			}
			retry := time.Duration(0)
			for {
				batch := b.drain(allocationStatusBatchLimit)
				if len(batch) == 0 {
					break
				}
				if err := b.sendBatch(batch); err != nil {
					b.requeue(batch)
					retry = nextAllocationStatusRetryDelay(retry, b.retryInitialDelay, b.retryMaxDelay)
					delay := retry
					if b.jitter != nil {
						delay = b.jitter(retry)
					}
					if !b.wait(delay) {
						b.flushOnce()
						return
					}
				} else {
					retry = 0
				}
			}
		case <-b.stop:
			b.flushOnce()
			return
		}
	}
}

func (b *allocationConditionBatcher) drain(limit int) []*nodev1.AllocationCapabilityConditionReport {
	b.mu.Lock()
	defer b.mu.Unlock()
	ids := make([]string, 0, len(b.pending))
	for allocationID := range b.pending {
		ids = append(ids, allocationID)
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]*nodev1.AllocationCapabilityConditionReport, 0, len(ids))
	for _, allocationID := range ids {
		out = append(out, b.pending[allocationID])
		delete(b.pending, allocationID)
	}
	return out
}

func (b *allocationConditionBatcher) requeue(batch []*nodev1.AllocationCapabilityConditionReport) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, failed := range batch {
		allocationID := failed.GetAllocationID()
		if current := b.pending[allocationID]; conditionReportSupersedes(failed, current) {
			b.pending[allocationID] = failed
		}
	}
}

func (b *allocationConditionBatcher) sendBatch(batch []*nodev1.AllocationCapabilityConditionReport) error {
	ctx, cancel := context.WithTimeout(context.Background(), reporterRPCTimeout)
	defer cancel()
	return b.send(ctx, batch)
}

func (b *allocationConditionBatcher) flushOnce() {
	batch := b.drain(allocationStatusBatchLimit)
	if len(batch) == 0 {
		return
	}
	if err := b.sendBatch(batch); err != nil {
		b.requeue(batch)
	}
}

func (b *allocationConditionBatcher) wait(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-b.stop:
		return false
	}
}

func conditionReportSupersedes(next, current *nodev1.AllocationCapabilityConditionReport) bool {
	if next == nil {
		return false
	}
	if current == nil || next.GetAttempt() > current.GetAttempt() {
		return true
	}
	return next.GetAttempt() == current.GetAttempt() && next.GetConditionSet().GetRevision() > current.GetConditionSet().GetRevision()
}
