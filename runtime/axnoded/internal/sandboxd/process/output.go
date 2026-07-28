package process

import (
	"context"
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
	subscribers map[chan StreamEvent]<-chan struct{}
	done        chan struct{}
	closed      bool
}

func newOutputHub(limit int) *outputHub {
	return &outputHub{
		limit:       limit,
		subscribers: map[chan StreamEvent]<-chan struct{}{},
		done:        make(chan struct{}),
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
	for ch, done := range h.subscribers {
		select {
		case ch <- cloneStreamEvent(event):
		case <-done:
			delete(h.subscribers, ch)
			close(ch)
		}
	}
}

func (h *outputHub) subscribe(ctx context.Context) <-chan StreamEvent {
	ch := make(chan StreamEvent, h.limit)
	h.mu.Lock()
	for _, event := range h.backlog {
		ch <- cloneStreamEvent(event)
	}
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch
	}
	h.subscribers[ch] = ctx.Done()
	h.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
		case <-h.done:
			return
		}
		h.mu.Lock()
		if _, ok := h.subscribers[ch]; ok {
			delete(h.subscribers, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()
	return ch
}

func (h *outputHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	close(h.done)
	for ch := range h.subscribers {
		close(ch)
		delete(h.subscribers, ch)
	}
}

func cloneStreamEvent(event StreamEvent) StreamEvent {
	return StreamEvent{
		Stdout: append([]byte(nil), event.Stdout...),
		Stderr: append([]byte(nil), event.Stderr...),
	}
}
