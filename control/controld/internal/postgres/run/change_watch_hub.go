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
	changeWatchReconnectMinDelay = 100 * time.Millisecond
	changeWatchReconnectMaxDelay = 2 * time.Second
	changeWatchCleanupTimeout    = 2 * time.Second
)

var errChangeWatchUnavailable = errors.New("postgres change watch listener unavailable")

type changeWatchHub struct {
	connConfig  *pgx.ConnConfig
	channel     string
	description string
	ctx         context.Context
	cancel      context.CancelFunc

	mu          sync.Mutex
	ready       bool
	readyCh     chan struct{}
	closed      bool
	nextID      uint64
	subscribers map[uint64]*changeWatchSubscription
	wg          sync.WaitGroup
}

func newChangeWatchHub(pool *pgxpool.Pool, channel, description string) *changeWatchHub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &changeWatchHub{
		connConfig:  pool.Config().ConnConfig.Copy(),
		channel:     channel,
		description: description,
		ctx:         ctx,
		cancel:      cancel,
		readyCh:     make(chan struct{}),
		subscribers: make(map[uint64]*changeWatchSubscription),
	}
	h.wg.Add(1)
	go h.run()
	return h
}

func (h *changeWatchHub) subscribe(ctx context.Context, key string) (*changeWatchSubscription, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errChangeWatchUnavailable
		}
		if h.ready {
			h.nextID++
			s := &changeWatchSubscription{id: h.nextID, key: key, hub: h, changes: make(chan struct{}, 1), done: make(chan struct{})}
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
			return nil, errChangeWatchUnavailable
		case <-readyCh:
		}
	}
}

func (h *changeWatchHub) run() {
	defer h.wg.Done()
	delay := changeWatchReconnectMinDelay
	reported := false
	for {
		conn, err := pgx.ConnectConfig(h.ctx, h.connConfig)
		if err == nil {
			_, err = conn.Exec(h.ctx, `LISTEN `+h.channel)
		}
		if err != nil {
			if !reported && h.ctx.Err() == nil {
				logrus.WithError(err).Warn(h.description + " watch listener unavailable")
				reported = true
			}
			closeChangeWatchConnection(conn)
			if !h.waitReconnect(delay) {
				return
			}
			delay = nextChangeWatchReconnectDelay(delay)
			continue
		}

		h.markReady()
		if reported {
			logrus.Info(h.description + " watch listener recovered")
			reported = false
		}
		delay = changeWatchReconnectMinDelay
		for {
			notification, err := conn.WaitForNotification(h.ctx)
			if err != nil {
				h.markUnavailable(err)
				break
			}
			if notification.Channel == h.channel {
				h.publish(notification.Payload)
			}
		}
		closeChangeWatchConnection(conn)
		if !h.waitReconnect(delay) {
			return
		}
		delay = nextChangeWatchReconnectDelay(delay)
	}
}

func (h *changeWatchHub) markReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ready {
		return
	}
	h.ready = true
	close(h.readyCh)
}

func (h *changeWatchHub) markUnavailable(cause error) {
	h.mu.Lock()
	if h.ready {
		h.ready = false
		h.readyCh = make(chan struct{})
	}
	subs := make([]*changeWatchSubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.finish(errors.Join(errChangeWatchUnavailable, cause))
	}
}

func (h *changeWatchHub) publish(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subscribers {
		if sub.key == key {
			sub.signal()
		}
	}
}

func (h *changeWatchHub) unsubscribe(sub *changeWatchSubscription) {
	if sub == nil {
		return
	}
	h.mu.Lock()
	delete(h.subscribers, sub.id)
	h.mu.Unlock()
	sub.finish(nil)
}

func (h *changeWatchHub) waitReconnect(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-h.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (h *changeWatchHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subs := make([]*changeWatchSubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	h.cancel()
	for _, sub := range subs {
		sub.finish(errChangeWatchUnavailable)
	}
	h.wg.Wait()
}

func nextChangeWatchReconnectDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > changeWatchReconnectMaxDelay {
		return changeWatchReconnectMaxDelay
	}
	return delay
}

func closeChangeWatchConnection(conn *pgx.Conn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), changeWatchCleanupTimeout)
	defer cancel()
	_ = conn.Close(ctx)
}

type changeWatchSubscription struct {
	id      uint64
	key     string
	hub     *changeWatchHub
	changes chan struct{}
	done    chan struct{}
	once    sync.Once
	errMu   sync.RWMutex
	err     error
}

func (s *changeWatchSubscription) signal() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *changeWatchSubscription) wait(ctx context.Context) error {
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

func (s *changeWatchSubscription) close() { s.hub.unsubscribe(s) }

func (s *changeWatchSubscription) finish(err error) {
	s.once.Do(func() {
		s.errMu.Lock()
		s.err = err
		s.errMu.Unlock()
		close(s.done)
	})
}
