package pgrun

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

const (
	leaseChangeChannel          = "axern_execution_lease_changes"
	leaseWatchReconnectMinDelay = 100 * time.Millisecond
	leaseWatchReconnectMaxDelay = 2 * time.Second
	leaseWatchCleanupTimeout    = 2 * time.Second
)

var errLeaseWatchUnavailable = errors.New("execution lease watch listener unavailable")

type leaseWatchHub struct {
	connConfig *pgx.ConnConfig
	ctx        context.Context
	cancel     context.CancelFunc

	mu          sync.Mutex
	ready       bool
	readyCh     chan struct{}
	closed      bool
	nextID      uint64
	subscribers map[uint64]*leaseWatchSubscription
	wg          sync.WaitGroup
}

func newLeaseWatchHub(pool *pgxpool.Pool) *leaseWatchHub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &leaseWatchHub{
		connConfig:  pool.Config().ConnConfig.Copy(),
		ctx:         ctx,
		cancel:      cancel,
		readyCh:     make(chan struct{}),
		subscribers: make(map[uint64]*leaseWatchSubscription),
	}
	h.wg.Add(1)
	go h.run()
	return h
}

func (h *leaseWatchHub) subscribe(ctx context.Context, nodeID string) (*leaseWatchSubscription, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errLeaseWatchUnavailable
		}
		if h.ready {
			h.nextID++
			s := &leaseWatchSubscription{id: h.nextID, nodeID: nodeID, hub: h, changes: make(chan struct{}, 1), done: make(chan struct{})}
			h.subscribers[s.id] = s
			h.mu.Unlock()
			return s, nil
		}
		readyCh := h.readyCh
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.ctx.Done():
			return nil, errLeaseWatchUnavailable
		case <-readyCh:
		}
	}
}

func (h *leaseWatchHub) run() {
	defer h.wg.Done()
	delay := leaseWatchReconnectMinDelay
	reported := false
	for {
		conn, err := pgx.ConnectConfig(h.ctx, h.connConfig)
		if err == nil {
			_, err = conn.Exec(h.ctx, `LISTEN `+leaseChangeChannel)
		}
		if err != nil {
			if !reported && h.ctx.Err() == nil {
				logrus.WithError(err).Warn("execution lease watch listener unavailable")
				reported = true
			}
			closeLeaseWatchConnection(conn)
			if !h.waitReconnect(delay) {
				return
			}
			delay = nextLeaseWatchReconnectDelay(delay)
			continue
		}

		h.markReady()
		if reported {
			logrus.Info("execution lease watch listener recovered")
			reported = false
		}
		delay = leaseWatchReconnectMinDelay
		for {
			notification, err := conn.WaitForNotification(h.ctx)
			if err != nil {
				h.markUnavailable(err)
				break
			}
			if notification.Channel == leaseChangeChannel {
				h.publish(notification.Payload)
			}
		}
		closeLeaseWatchConnection(conn)
		if !h.waitReconnect(delay) {
			return
		}
		delay = nextLeaseWatchReconnectDelay(delay)
	}
}

func (h *leaseWatchHub) markReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ready {
		return
	}
	h.ready = true
	close(h.readyCh)
}

func (h *leaseWatchHub) markUnavailable(cause error) {
	h.mu.Lock()
	if h.ready {
		h.ready = false
		h.readyCh = make(chan struct{})
	}
	subs := make([]*leaseWatchSubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.finish(errors.Join(errLeaseWatchUnavailable, cause))
	}
}

func (h *leaseWatchHub) publish(nodeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if sub.nodeID == nodeID {
			sub.signal()
		}
	}
}

func (h *leaseWatchHub) unsubscribe(sub *leaseWatchSubscription) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	delete(h.subscribers, sub.id)
	h.mu.Unlock()
	sub.finish(nil)
}

func (h *leaseWatchHub) waitReconnect(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-h.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *leaseWatchHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subs := make([]*leaseWatchSubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	h.cancel()
	for _, sub := range subs {
		sub.finish(errLeaseWatchUnavailable)
	}
	h.wg.Wait()
}

func nextLeaseWatchReconnectDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > leaseWatchReconnectMaxDelay {
		return leaseWatchReconnectMaxDelay
	}
	return delay
}

func closeLeaseWatchConnection(conn *pgx.Conn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), leaseWatchCleanupTimeout)
	defer cancel()
	_ = conn.Close(ctx)
}

type leaseWatchSubscription struct {
	id      uint64
	nodeID  string
	hub     *leaseWatchHub
	changes chan struct{}
	done    chan struct{}
	once    sync.Once
	errMu   sync.RWMutex
	err     error
}

func (s *leaseWatchSubscription) signal() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *leaseWatchSubscription) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		s.errMu.RLock()
		defer s.errMu.RUnlock()
		return s.err
	case <-s.changes:
		return nil
	}
}

func (s *leaseWatchSubscription) close() { s.hub.unsubscribe(s) }

func (s *leaseWatchSubscription) finish(err error) {
	s.once.Do(func() {
		s.errMu.Lock()
		s.err = err
		s.errMu.Unlock()
		close(s.done)
	})
}
