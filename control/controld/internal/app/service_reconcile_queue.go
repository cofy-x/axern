package app

import (
	"strings"
	"sync"
	"time"
)

const maxPendingServiceReconciles = 4096

type serviceReconcileItem struct {
	ServiceID  string
	FullSweep  bool
	EnqueuedAt time.Time
}

type serviceReconcileQueue struct {
	mu          sync.Mutex
	pending     map[string]time.Time
	order       []string
	fullSweep   bool
	fullSweepAt time.Time
	wake        chan struct{}
}

func newServiceReconcileQueue() *serviceReconcileQueue {
	return &serviceReconcileQueue{
		pending: make(map[string]time.Time),
		wake:    make(chan struct{}, 1),
	}
}

func (q *serviceReconcileQueue) Enqueue(serviceIDs ...string) {
	if q == nil {
		return
	}

	now := time.Now()
	queued := false
	q.mu.Lock()
	for _, serviceID := range serviceIDs {
		serviceID = strings.TrimSpace(serviceID)
		if serviceID == "" {
			continue
		}
		queued = true
		if q.fullSweep {
			continue
		}
		if _, exists := q.pending[serviceID]; exists {
			continue
		}
		if len(q.pending) >= maxPendingServiceReconciles {
			fullSweepAt := now
			if len(q.order) > 0 {
				fullSweepAt = q.pending[q.order[0]]
			}
			q.pending = make(map[string]time.Time)
			q.order = nil
			q.fullSweep = true
			q.fullSweepAt = fullSweepAt
			continue
		}
		q.pending[serviceID] = now
		q.order = append(q.order, serviceID)
	}
	q.mu.Unlock()

	if queued {
		q.signal()
	}
}

func (q *serviceReconcileQueue) Wake() <-chan struct{} {
	if q == nil {
		return nil
	}
	return q.wake
}

func (q *serviceReconcileQueue) Take() serviceReconcileItem {
	if q == nil {
		return serviceReconcileItem{}
	}

	q.mu.Lock()
	var item serviceReconcileItem
	if q.fullSweep {
		item = serviceReconcileItem{FullSweep: true, EnqueuedAt: q.fullSweepAt}
		q.fullSweep = false
		q.fullSweepAt = time.Time{}
	} else if len(q.order) > 0 {
		serviceID := q.order[0]
		q.order[0] = ""
		q.order = q.order[1:]
		item = serviceReconcileItem{ServiceID: serviceID, EnqueuedAt: q.pending[serviceID]}
		delete(q.pending, serviceID)
	}
	hasMore := q.fullSweep || len(q.order) > 0
	q.mu.Unlock()

	if hasMore {
		q.signal()
	}
	return item
}

func (q *serviceReconcileQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
