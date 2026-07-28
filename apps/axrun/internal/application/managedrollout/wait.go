package managedrollout

import (
	"context"
	"errors"
	"io"
	"time"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Until int

const (
	UntilReady Until = iota + 1
	UntilTerminal
)

type Waiter struct {
	Client     rolloutv1.RolloutControlClient
	OnEvent    func(*rolloutv1.RolloutEvent) error
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

func (w Waiter) Wait(ctx context.Context, rolloutID string, until Until) (*rolloutv1.GetRolloutResponse, error) {
	minBackoff, maxBackoff := w.MinBackoff, w.MaxBackoff
	if minBackoff <= 0 {
		minBackoff = 250 * time.Millisecond
	}
	if maxBackoff < minBackoff {
		maxBackoff = 5 * time.Second
	}
	backoff := minBackoff
	var sequence int64
	for {
		current, err := w.get(ctx, rolloutID)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if !retryableWatch(err) {
				return nil, err
			}
			if err := waitBackoff(ctx, backoff); err != nil {
				return nil, err
			}
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		if reached(current.GetRollout(), until) {
			return current, nil
		}
		stream, err := w.Client.WatchRolloutEvents(ctx, &rolloutv1.WatchRolloutEventsRequest{
			RolloutID:     rolloutID,
			AfterSequence: sequence,
		})
		if err == nil {
			for {
				batch, recvErr := stream.Recv()
				if recvErr != nil {
					err = recvErr
					break
				}
				backoff = minBackoff
				for _, event := range batch.GetEvents() {
					if event.GetSequence() <= sequence {
						continue
					}
					sequence = event.GetSequence()
					if w.OnEvent != nil {
						if err := w.OnEvent(event); err != nil {
							return nil, err
						}
					}
				}
				current, getErr := w.get(ctx, rolloutID)
				if getErr != nil {
					err = getErr
					break
				}
				if batch.GetTerminal() || reached(current.GetRollout(), until) {
					return current, nil
				}
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !retryableWatch(err) {
			return nil, err
		}
		// A stream may close after the terminal transition but before the final
		// event reaches the client. Re-read durable state before reconnecting.
		current, getErr := w.get(ctx, rolloutID)
		if getErr == nil && reached(current.GetRollout(), until) {
			return current, nil
		}
		if getErr != nil && !retryableWatch(getErr) {
			return nil, getErr
		}
		if err := waitBackoff(ctx, backoff); err != nil {
			return nil, err
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
}

func waitBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextBackoff(current, maximum time.Duration) time.Duration {
	if current >= maximum || current > maximum/2 {
		return maximum
	}
	return current * 2
}

func (w Waiter) get(ctx context.Context, rolloutID string) (*rolloutv1.GetRolloutResponse, error) {
	return w.Client.GetRollout(ctx, &rolloutv1.GetRolloutRequest{RolloutID: rolloutID})
}

func retryableWatch(err error) bool {
	if errors.Is(err, io.EOF) {
		return true
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return true
	default:
		return false
	}
}

func reached(rollout *rolloutv1.Rollout, until Until) bool {
	if rollout == nil {
		return false
	}
	if until == UntilReady && rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_READY {
		return true
	}
	return IsTerminal(rollout.GetStatus())
}

func IsTerminal(value rolloutv1.RolloutStatus) bool {
	switch value {
	case rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED,
		rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED,
		rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED,
		rolloutv1.RolloutStatus_ROLLOUT_STATUS_DELETING:
		return true
	default:
		return false
	}
}
