package pgtunnel

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
	tunnelChangeChannel           = "axern_tunnel_session_changes"
	watchReconnectMinDelay        = 100 * time.Millisecond
	watchReconnectMaxDelay        = 2 * time.Second
	watchConnectionCleanupTimeout = 2 * time.Second
)

var errWatchUnavailable = errors.New("tunnel session watch listener unavailable")

type watchHub struct {
	connConfig *pgx.ConnConfig
	ctx        context.Context
	cancel     context.CancelFunc

	mu          sync.Mutex
	ready       bool
	readyCh     chan struct{}
	closed      bool
	nextID      uint64
	subscribers map[uint64]*watchSubscription
	wg          sync.WaitGroup
}

func newWatchHub(pool *pgxpool.Pool) *watchHub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &watchHub{
		connConfig:  pool.Config().ConnConfig.Copy(),
		ctx:         ctx,
		cancel:      cancel,
		readyCh:     make(chan struct{}),
		subscribers: make(map[uint64]*watchSubscription),
	}
	h.wg.Add(1)
	go h.run()
	return h
}

func (h *watchHub) subscribe(ctx context.Context, nodeID string) (*watchSubscription, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errWatchUnavailable
		}
		if h.ready {
			h.nextID++
			s := &watchSubscription{id: h.nextID, nodeID: nodeID, hub: h, changes: make(chan struct{}, 1), done: make(chan struct{})}
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
			return nil, errWatchUnavailable
		case <-readyCh:
		}
	}
}

func (h *watchHub) run() {
	defer h.wg.Done()
	delay := watchReconnectMinDelay
	reported := false
	for {
		conn, err := pgx.ConnectConfig(h.ctx, h.connConfig)
		if err == nil {
			_, err = conn.Exec(h.ctx, `LISTEN `+tunnelChangeChannel)
		}
		if err != nil {
			if !reported && h.ctx.Err() == nil {
				logrus.WithError(err).Warn("tunnel session watch listener unavailable")
				reported = true
			}
			closeWatchConnection(conn)
			if !h.waitReconnect(delay) {
				return
			}
			delay = nextWatchReconnectDelay(delay)
			continue
		}

		h.markReady()
		if reported {
			logrus.Info("tunnel session watch listener recovered")
			reported = false
		}
		delay = watchReconnectMinDelay
		for {
			notification, err := conn.WaitForNotification(h.ctx)
			if err != nil {
				h.markUnavailable(err)
				break
			}
			if notification.Channel == tunnelChangeChannel {
				h.publish(notification.Payload)
			}
		}
		closeWatchConnection(conn)
		if !h.waitReconnect(delay) {
			return
		}
		delay = nextWatchReconnectDelay(delay)
	}
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
	subs := make([]*watchSubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.finish(errors.Join(errWatchUnavailable, cause))
	}
}

func (h *watchHub) publish(nodeID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if sub.nodeID == nodeID {
			sub.signal()
		}
	}
}

func (h *watchHub) unsubscribe(sub *watchSubscription) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	delete(h.subscribers, sub.id)
	h.mu.Unlock()
	sub.finish(nil)
}

func (h *watchHub) waitReconnect(delay time.Duration) bool {
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
	subs := make([]*watchSubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	h.cancel()
	for _, sub := range subs {
		sub.finish(errWatchUnavailable)
	}
	h.wg.Wait()
}

func nextWatchReconnectDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > watchReconnectMaxDelay {
		return watchReconnectMaxDelay
	}
	return delay
}

func closeWatchConnection(conn *pgx.Conn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), watchConnectionCleanupTimeout)
	defer cancel()
	_ = conn.Close(ctx)
}

type watchSubscription struct {
	id      uint64
	nodeID  string
	hub     *watchHub
	changes chan struct{}
	done    chan struct{}
	once    sync.Once
	errMu   sync.RWMutex
	err     error
}

func (s *watchSubscription) signal() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *watchSubscription) wait(ctx context.Context) error {
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

func (s *watchSubscription) waitFor(ctx context.Context, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		return true, nil
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-s.done:
		s.errMu.RLock()
		defer s.errMu.RUnlock()
		return false, s.err
	case <-s.changes:
		return false, nil
	case <-timer.C:
		return true, nil
	}
}

func (s *watchSubscription) close() { s.hub.unsubscribe(s) }

func (s *watchSubscription) finish(err error) {
	s.once.Do(func() {
		s.errMu.Lock()
		s.err = err
		s.errMu.Unlock()
		close(s.done)
	})
}
