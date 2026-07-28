package pgservice

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatchHubRoutesAndCoalescesServiceChanges(t *testing.T) {
	hub := &watchHub{subscribers: make(map[uint64]*watchSubscription)}
	subscription := &watchSubscription{
		id:        1,
		serviceID: "svc-a",
		hub:       hub,
		changes:   make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	hub.subscribers[subscription.id] = subscription

	hub.publish("svc-b")
	hub.publish("svc-a")
	hub.publish("svc-a")
	if err := subscription.wait(context.Background()); err != nil {
		t.Fatalf("wait(first) error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := subscription.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait(second) error = %v, want deadline exceeded after coalescing", err)
	}
}

func TestWatchHubFailsActiveSubscriptionsWhenListenerDisconnects(t *testing.T) {
	hub := &watchHub{
		ready:       true,
		readyCh:     closedSignal(),
		subscribers: make(map[uint64]*watchSubscription),
	}
	subscription := &watchSubscription{
		id:        1,
		serviceID: "svc-a",
		hub:       hub,
		changes:   make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	hub.subscribers[subscription.id] = subscription

	hub.markUnavailable(errors.New("connection lost"))

	if err := subscription.wait(context.Background()); !errors.Is(err, errWatchListenerUnavailable) {
		t.Fatalf("wait() error = %v, want listener unavailable", err)
	}
	if hub.ready {
		t.Fatal("hub remained ready after listener failure")
	}
	if len(hub.subscribers) != 0 {
		t.Fatalf("subscribers = %d, want 0", len(hub.subscribers))
	}
}

func closedSignal() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
