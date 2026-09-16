package process

import (
	"context"
	"fmt"
	"sync"
)

type streamWriter struct {
	emit func([]byte)
}

func (w streamWriter) Write(data []byte) (int, error) {
	if len(data) > 0 {
		copied := append([]byte(nil), data...)
		w.emit(copied)
	}
	return len(data), nil
}

type outputHub struct {
	limit       int
	mu          sync.Mutex
	backlog     []StreamEvent
	subscribers map[chan StreamEvent]outputSubscription
	closed      bool
}

type outputSubscription struct {
	done <-chan struct{}
	stop func() bool
}

func newOutputHub(limit int) *outputHub {
	return &outputHub{
		limit:       limit,
		subscribers: map[chan StreamEvent]outputSubscription{},
	}
}

func (h *outputHub) publish(event StreamEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.backlog = append(h.backlog, cloneStreamEvent(event))
	for len(h.backlog) > h.limit {
		h.backlog = h.backlog[1:]
	}
	for ch, subscription := range h.subscribers {
		select {
		case ch <- cloneStreamEvent(event):
		case <-subscription.done:
			subscription.stop()
			delete(h.subscribers, ch)
			close(ch)
		default:
			subscription.stop()
			// Disconnect a lagging subscriber with an explicit loss diagnostic.
			// Never hold process cleanup hostage to an unconsumed stream.
			select {
			case <-ch:
			default:
			}
			ch <- StreamEvent{Error: "process output consumer exceeded bounded backlog"}
			delete(h.subscribers, ch)
			close(ch)
		}
	}
}

func (h *outputHub) subscribe(ctx context.Context) (<-chan StreamEvent, error) {
	ch := make(chan StreamEvent, h.limit)
	h.mu.Lock()
	if len(h.subscribers) >= 64 {
		h.mu.Unlock()
		return nil, fmt.Errorf("process output subscriber limit reached: %w", ErrResourceLimit)
	}
	for _, event := range h.backlog {
		ch <- cloneStreamEvent(event)
	}
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, nil
	}
	stop := context.AfterFunc(ctx, func() {
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	})
	h.subscribers[ch] = outputSubscription{done: ctx.Done(), stop: stop}
	h.mu.Unlock()
	return ch, nil
}

func (h *outputHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch, subscription := range h.subscribers {
		subscription.stop()
		close(ch)
		delete(h.subscribers, ch)
	}
}

func cloneStreamEvent(event StreamEvent) StreamEvent {
	return StreamEvent{
		Error:  event.Error,
		Stdout: append([]byte(nil), event.Stdout...),
		Stderr: append([]byte(nil), event.Stderr...),
	}
}
