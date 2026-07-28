package managedrollout

import (
	"context"
	"io"
	"reflect"
	"testing"
	"time"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeControl struct {
	rolloutv1.RolloutControlClient
	get      func() *rolloutv1.GetRolloutResponse
	getError func() error
	watch    func(*rolloutv1.WatchRolloutEventsRequest) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error)
}

func (f fakeControl) GetRollout(context.Context, *rolloutv1.GetRolloutRequest, ...grpc.CallOption) (*rolloutv1.GetRolloutResponse, error) {
	if f.getError != nil {
		if err := f.getError(); err != nil {
			return nil, err
		}
	}
	return f.get(), nil
}

func (f fakeControl) WatchRolloutEvents(_ context.Context, request *rolloutv1.WatchRolloutEventsRequest, _ ...grpc.CallOption) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error) {
	return f.watch(request)
}

type fakeEventStream struct {
	grpc.ClientStream
	items []*rolloutv1.WatchRolloutEventsResponse
	err   error
}

func (s *fakeEventStream) Recv() (*rolloutv1.WatchRolloutEventsResponse, error) {
	if len(s.items) == 0 {
		if s.err != nil {
			err := s.err
			s.err = nil
			return nil, err
		}
		return nil, io.EOF
	}
	item := s.items[0]
	s.items = s.items[1:]
	return item, nil
}

func TestWaitReturnsDurableTerminalStateWithoutOpeningStream(t *testing.T) {
	watched := false
	response, err := (Waiter{Client: fakeControl{
		get: func() *rolloutv1.GetRolloutResponse {
			return &rolloutv1.GetRolloutResponse{
				Rollout: &rolloutv1.Rollout{
					Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED,
				},
				Episodes: []*rolloutv1.Episode{
					{FailureClass: rolloutv1.FailureClass_FAILURE_CLASS_BUDGET},
				},
			}
		},
		watch: func(*rolloutv1.WatchRolloutEventsRequest) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error) {
			watched = true
			return nil, nil
		},
	}}).Wait(context.Background(), "rol-test", UntilTerminal)
	if err != nil || watched || len(response.GetEpisodes()) != 1 {
		t.Fatalf("Wait() response=%v watched=%t err=%v", response, watched, err)
	}
}

func TestWaitReconnectsFromLastSequenceAndDeduplicates(t *testing.T) {
	getCalls, watchCalls := 0, 0
	var after []int64
	var observed []int64
	client := fakeControl{
		get: func() *rolloutv1.GetRolloutResponse {
			getCalls++
			statusValue := rolloutv1.RolloutStatus_ROLLOUT_STATUS_RUNNING
			if getCalls >= 5 {
				statusValue = rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED
			}
			return &rolloutv1.GetRolloutResponse{Rollout: &rolloutv1.Rollout{Status: statusValue}}
		},
		watch: func(request *rolloutv1.WatchRolloutEventsRequest) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error) {
			after = append(after, request.GetAfterSequence())
			watchCalls++
			if watchCalls == 1 {
				return &fakeEventStream{items: []*rolloutv1.WatchRolloutEventsResponse{{Events: []*rolloutv1.RolloutEvent{{Sequence: 1}, {Sequence: 2}}}}}, nil
			}
			return &fakeEventStream{
				items: []*rolloutv1.WatchRolloutEventsResponse{
					{
						Events:   []*rolloutv1.RolloutEvent{{Sequence: 2}, {Sequence: 3}},
						Terminal: true,
					},
				},
			}, nil
		},
	}
	response, err := (Waiter{Client: client, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, OnEvent: func(event *rolloutv1.RolloutEvent) error {
		observed = append(observed, event.GetSequence())
		return nil
	}}).Wait(context.Background(), "rol-test", UntilTerminal)
	if err != nil || response.GetRollout().GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED {
		t.Fatalf("Wait() response=%v err=%v", response, err)
	}
	if !reflect.DeepEqual(after, []int64{0, 2}) || !reflect.DeepEqual(observed, []int64{1, 2, 3}) {
		t.Fatalf("after=%v observed=%v", after, observed)
	}
}

func TestWaitDoesNotRetryPermanentError(t *testing.T) {
	watches := 0
	_, err := (Waiter{Client: fakeControl{
		get: func() *rolloutv1.GetRolloutResponse {
			return &rolloutv1.GetRolloutResponse{Rollout: &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_RUNNING}}
		},
		watch: func(*rolloutv1.WatchRolloutEventsRequest) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error) {
			watches++
			return nil, status.Error(codes.PermissionDenied, "denied")
		},
	}}).Wait(context.Background(), "rol-test", UntilTerminal)
	if status.Code(err) != codes.PermissionDenied || watches != 1 {
		t.Fatalf("Wait() watches=%d err=%v", watches, err)
	}
}

func TestWaitRetriesTransientDurableStateRead(t *testing.T) {
	gets := 0
	response, err := (Waiter{Client: fakeControl{
		getError: func() error {
			gets++
			if gets == 1 {
				return status.Error(codes.Unavailable, "temporary")
			}
			return nil
		},
		get: func() *rolloutv1.GetRolloutResponse {
			return &rolloutv1.GetRolloutResponse{Rollout: &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED}}
		},
		watch: func(*rolloutv1.WatchRolloutEventsRequest) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error) {
			t.Fatal("watch should not open after terminal durable read")
			return nil, nil
		},
	}, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}).Wait(context.Background(), "rol-test", UntilTerminal)
	if err != nil || response.GetRollout().GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED || gets != 2 {
		t.Fatalf("Wait() response=%v gets=%d err=%v", response, gets, err)
	}
}

func TestWaitRetriesTransientDurableReadAfterStreamDisconnect(t *testing.T) {
	getCalls, successfulGets := 0, 0
	response, err := (Waiter{Client: fakeControl{
		getError: func() error {
			getCalls++
			if getCalls == 3 {
				return status.Error(codes.Unavailable, "temporary post-stream read failure")
			}
			return nil
		},
		get: func() *rolloutv1.GetRolloutResponse {
			successfulGets++
			statusValue := rolloutv1.RolloutStatus_ROLLOUT_STATUS_RUNNING
			if successfulGets >= 3 {
				statusValue = rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED
			}
			return &rolloutv1.GetRolloutResponse{Rollout: &rolloutv1.Rollout{Status: statusValue}}
		},
		watch: func(*rolloutv1.WatchRolloutEventsRequest) (rolloutv1.RolloutControl_WatchRolloutEventsClient, error) {
			return &fakeEventStream{items: []*rolloutv1.WatchRolloutEventsResponse{{}}}, nil
		},
	}, MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond}).Wait(context.Background(), "rol-test", UntilTerminal)
	if err != nil || response.GetRollout().GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED || getCalls != 4 {
		t.Fatalf("Wait() response=%v getCalls=%d successfulGets=%d err=%v", response, getCalls, successfulGets, err)
	}
}
