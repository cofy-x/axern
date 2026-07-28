package pgservice

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

const (
	watchReconnectMinDelay = 100 * time.Millisecond
	watchReconnectMaxDelay = 2 * time.Second
	watchCleanupTimeout    = 2 * time.Second
)

var errWatchListenerUnavailable = errors.New("service watch listener unavailable")

type watchHub struct {
	connConfig *pgx.ConnConfig
	ctx        context.Context
	cancel     context.CancelFunc

	startOnce sync.Once
	wg        sync.WaitGroup

	mu          sync.Mutex
	ready       bool
	readyCh     chan struct{}
	closed      bool
	nextID      uint64
	subscribers map[uint64]*watchSubscription
}

type WatchStats struct {
	Active        int
	ListenerReady bool
}

func newWatchHub(pool *pgxpool.Pool) *watchHub {
	ctx, cancel := context.WithCancel(context.Background())
	hub := &watchHub{
		connConfig:  pool.Config().ConnConfig.Copy(),
		ctx:         ctx,
		cancel:      cancel,
		readyCh:     make(chan struct{}),
		subscribers: make(map[uint64]*watchSubscription),
	}
	hub.start()
	return hub
}

func (h *watchHub) subscribe(ctx context.Context, serviceID string) (*watchSubscription, error) {
	h.start()

	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errWatchListenerUnavailable
		}
		if h.ready {
			h.nextID++
			subscription := &watchSubscription{
				id:        h.nextID,
				serviceID: serviceID,
				hub:       h,
				changes:   make(chan struct{}, 1),
				done:      make(chan struct{}),
			}
			h.subscribers[subscription.id] = subscription
			h.mu.Unlock()
			return subscription, nil
		}
		readyCh := h.readyCh
		h.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.ctx.Done():
			return nil, errWatchListenerUnavailable
		case <-readyCh:
		}
	}
}

func (h *watchHub) start() {
	h.startOnce.Do(func() {
		h.wg.Add(1)
		go h.run()
	})
}

func (h *watchHub) run() {
	defer h.wg.Done()
	delay := watchReconnectMinDelay
	failureReported := false
	for {
		connection, err := pgx.ConnectConfig(h.ctx, h.connConfig)
		if err != nil {
			reportWatchListenerFailure(h.ctx, err, &failureReported)
			if !h.waitBeforeReconnect(delay) {
				return
			}
			delay = nextWatchReconnectDelay(delay)
			continue
		}

		if _, err := connection.Exec(h.ctx, `LISTEN axern_service_changes`); err != nil {
			reportWatchListenerFailure(h.ctx, err, &failureReported)
			closeWatchConnection(connection)
			if !h.waitBeforeReconnect(delay) {
				return
			}
			delay = nextWatchReconnectDelay(delay)
			continue
		}

		h.markReady()
		if failureReported {
			logrus.Info("service watch listener recovered")
			failureReported = false
		}
		delay = watchReconnectMinDelay
		for {
			notification, err := connection.WaitForNotification(h.ctx)
			if err != nil {
				reportWatchListenerFailure(h.ctx, err, &failureReported)
				h.markUnavailable(err)
				break
			}
			if notification.Channel == serviceChangeChannel {
				h.publish(notification.Payload)
			}
		}
		closeWatchConnection(connection)
		if !h.waitBeforeReconnect(delay) {
			return
		}
		delay = nextWatchReconnectDelay(delay)
	}
}

func reportWatchListenerFailure(ctx context.Context, err error, reported *bool) {
	if ctx.Err() != nil || *reported {
		return
	}
	logrus.WithError(err).Warn("service watch listener unavailable")
	*reported = true
}

func (h *watchHub) markReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ready {
		return
	}
	h.ready = true
	close(h.readyCh)
}

func (h *watchHub) markUnavailable(cause error) {
	h.mu.Lock()
	if h.ready {
		h.ready = false
		h.readyCh = make(chan struct{})
	}
	subscriptions := make([]*watchSubscription, 0, len(h.subscribers))
	for id, subscription := range h.subscribers {
		subscriptions = append(subscriptions, subscription)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()

	for _, subscription := range subscriptions {
		subscription.finish(errors.Join(errWatchListenerUnavailable, cause))
	}
}

func (h *watchHub) publish(serviceID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subscription := range h.subscribers {
		if subscription.serviceID == serviceID {
			subscription.signal()
		}
	}
}

func (h *watchHub) unsubscribe(subscription *watchSubscription) {
	if subscription == nil {
		return
	}
	h.mu.Lock()
	delete(h.subscribers, subscription.id)
	h.mu.Unlock()
	subscription.finish(nil)
}

func (h *watchHub) waitBeforeReconnect(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-h.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *watchHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.ready = false
	subscriptions := make([]*watchSubscription, 0, len(h.subscribers))
	for id, subscription := range h.subscribers {
		subscriptions = append(subscriptions, subscription)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()

	h.cancel()
	for _, subscription := range subscriptions {
		subscription.finish(errWatchListenerUnavailable)
	}
	h.wg.Wait()
}

func (h *watchHub) stats() WatchStats {
	if h == nil {
		return WatchStats{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return WatchStats{
		Active:        len(h.subscribers),
		ListenerReady: h.ready,
	}
}

func nextWatchReconnectDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > watchReconnectMaxDelay {
		return watchReconnectMaxDelay
	}
	return delay
}

func closeWatchConnection(connection *pgx.Conn) {
	if connection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), watchCleanupTimeout)
	defer cancel()
	_ = connection.Close(ctx)
}

type watchSubscription struct {
	id        uint64
	serviceID string
	hub       *watchHub
	changes   chan struct{}
	done      chan struct{}

	finishOnce sync.Once
	mu         sync.RWMutex
	err        error
}

func (s *watchSubscription) signal() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *watchSubscription) wait(ctx context.Context) error {
	select {
	case <-s.done:
		return s.result()
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.result()
	case <-s.changes:
		return nil
	}
}

func (s *watchSubscription) close() {
	if s != nil && s.hub != nil {
		s.hub.unsubscribe(s)
	}
}

func (s *watchSubscription) finish(err error) {
	s.finishOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *watchSubscription) result() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return s.err
	}
	return io.EOF
}
