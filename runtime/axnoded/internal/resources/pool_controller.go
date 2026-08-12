package resources

import (
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
)

type poolControllable interface {
	setPoolController(*poolController)
}

const (
	ResourcePoolTriggerPeriodic       = "periodic"
	ResourcePoolTriggerLowWatermark   = "low_watermark"
	ResourcePoolTriggerAllocationMiss = "allocation_miss"

	ResourcePoolAllocateHit            = "hit"
	ResourcePoolAllocateMissSyncCreate = "miss_sync_create"
	ResourcePoolAllocateExhausted      = "exhausted"
	ResourcePoolAllocateError          = "error"

	resourcePoolRefillOK    = "ok"
	resourcePoolRefillError = "error"
)

type poolController struct {
	manager   resizable
	interval  time.Duration
	triggerC  chan string
	stopC     chan struct{}
	doneC     chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

type reconcileOutcome struct {
	resource string
	result   string
	record   bool
}

func newPoolController(manager resizable, interval time.Duration) *poolController {
	return &poolController{
		manager:  manager,
		interval: interval,
		triggerC: make(chan string, 1),
		stopC:    make(chan struct{}),
		doneC:    make(chan struct{}),
	}
}

func (c *poolController) start() {
	c.startOnce.Do(func() {
		go func() {
			defer close(c.doneC)
			c.reconcile(ResourcePoolTriggerPeriodic)
			syncTicker := time.NewTicker(c.interval)
			defer syncTicker.Stop()
			for {
				select {
				case trigger := <-c.triggerC:
					c.reconcile(trigger)
				case <-syncTicker.C:
					c.reconcile(ResourcePoolTriggerPeriodic)
				case <-c.stopC:
					return
				}
			}
		}()
	})
}

func (c *poolController) shutdown() {
	if c == nil {
		return
	}
	c.stopOnce.Do(func() { close(c.stopC) })
	<-c.doneC
}

func (c *poolController) request(trigger string) {
	if c == nil {
		return
	}
	select {
	case c.triggerC <- trigger:
	default:
	}
}

func (c *poolController) reconcile(trigger string) {
	startedAt := time.Now()
	outcome := reconcilePool(c.manager)
	if outcome.record {
		metrics.RecordResourcePoolRefill(outcome.resource, trigger, outcome.result, time.Since(startedAt).Seconds())
	}
}

func runManager(period time.Duration, ms ...resizable) {
	for _, m := range ms {
		logrus.Debugf("start to sync resource %s", m.ResourceName())
		recordPoolState(m)
		controller := newPoolController(m, period)
		if controllable, ok := m.(poolControllable); ok {
			controllable.setPoolController(controller)
		}
		controller.start()
	}
}

func recordPoolState(m resizable) {
	metrics.RecordResourcePoolState(string(m.ResourceName()), m.CacheNum(), m.UsingNum(), m.CacheSizeLimit())
}

func reconcilePool(m resizable) reconcileOutcome {
	maxLimit := m.MaxSizeLimit()
	target := m.CacheSizeLimit()
	using := m.UsingNum()
	cache := m.CacheNum()

	recordPoolState(m)
	logrus.Debugf("reconcile resource %s, maxLimit %d, target %d, using %d, cache %d", m.ResourceName(), maxLimit, target, using, cache)

	if target <= 0 || cache >= target {
		return reconcileOutcome{}
	}

	available := maxLimit - (using + cache)
	if available <= 0 {
		return reconcileOutcome{}
	}

	toAdd := min(target-cache, available)
	if toAdd <= 0 {
		return reconcileOutcome{}
	}

	logrus.Debugf("add resource %s", m.ResourceName())
	added := m.Add(toAdd)
	recordPoolState(m)
	result := resourcePoolRefillOK
	if added < toAdd {
		result = resourcePoolRefillError
	}
	return reconcileOutcome{
		resource: string(m.ResourceName()),
		result:   result,
		record:   true,
	}
}
