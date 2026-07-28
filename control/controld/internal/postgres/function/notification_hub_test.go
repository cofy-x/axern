package pgfunction

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInvocationNotificationWakesOneWaiterPerSignal(t *testing.T) {
	hubCtx, cancelHub := context.WithCancel(context.Background())
	defer cancelHub()
	hub := &invocationNotificationHub{
		ctx:  hubCtx,
		wake: make(chan struct{}, 2),
	}
	hub.signal()

	if err := hub.wait(context.Background(), time.Second); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelSecond()
	if err := hub.wait(secondCtx, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second wait error = %v, want deadline exceeded", err)
	}
}
