package pgfunction

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

const functionInvocationChannel = "axern_function_invocations"

type invocationNotificationHub struct {
	connConfig *pgx.ConnConfig
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	wake       chan struct{}
	ready      bool
	wg         sync.WaitGroup
}

func newInvocationNotificationHub(pool *pgxpool.Pool, wakeCapacity int) *invocationNotificationHub {
	if wakeCapacity <= 0 {
		wakeCapacity = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &invocationNotificationHub{
		connConfig: pool.Config().ConnConfig.Copy(),
		ctx:        ctx,
		cancel:     cancel,
		wake:       make(chan struct{}, wakeCapacity),
	}
	h.wg.Add(1)
	go h.run()
	return h
}

func (h *invocationNotificationHub) run() {
	defer h.wg.Done()
	delay := 100 * time.Millisecond
	failureReported := false
	for h.ctx.Err() == nil {
		conn, err := pgx.ConnectConfig(h.ctx, h.connConfig)
		if err == nil {
			_, err = conn.Exec(h.ctx, `LISTEN `+functionInvocationChannel)
		}
		if err == nil {
			h.setReady(true)
			if failureReported {
				logrus.Info("function invocation listener recovered")
			}
			failureReported = false
			delay = 100 * time.Millisecond
			h.signal()
			for h.ctx.Err() == nil {
				if _, err = conn.WaitForNotification(h.ctx); err != nil {
					break
				}
				h.signal()
			}
		}
		h.setReady(false)
		if conn != nil {
			_ = conn.Close(context.Background())
		}
		if h.ctx.Err() != nil {
			return
		}
		if !failureReported {
			logrus.WithError(err).Warn("function invocation listener unavailable")
			failureReported = true
		}
		if !waitInvocationListener(h.ctx, delay) {
			return
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

func (h *invocationNotificationHub) setReady(ready bool) {
	h.mu.Lock()
	h.ready = ready
	h.mu.Unlock()
}

func (h *invocationNotificationHub) isReady() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ready
}

func (h *invocationNotificationHub) signal() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *invocationNotificationHub) wait(ctx context.Context, safetyTimeout time.Duration) error {
	if safetyTimeout <= 0 {
		safetyTimeout = time.Second
	}
	timer := time.NewTimer(safetyTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.ctx.Done():
		return context.Canceled
	case <-h.wake:
		return nil
	case <-timer.C:
		return nil
	}
}

func (h *invocationNotificationHub) close() {
	if h == nil {
		return
	}
	h.cancel()
	h.wg.Wait()
	h.setReady(false)
}

func waitInvocationListener(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
