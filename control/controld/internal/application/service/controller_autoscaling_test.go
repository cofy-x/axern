package appservice

import (
	"context"
	"strings"
	"testing"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeAutoscalingStatusStore struct {
	updated *servicev1.ServiceAutoscalingStatus
}

func (f *fakeAutoscalingStatusStore) UpdateStatus(context.Context, string, servicev1.ServiceStatus, string, time.Time) (*servicev1.Service, error) {
	panic("unexpected UpdateStatus call")
}

func (f *fakeAutoscalingStatusStore) UpdateAutoscalingStatus(_ context.Context, serviceID string, autoscaling *servicev1.ServiceAutoscalingStatus, now time.Time) (*servicev1.Service, error) {
	f.updated = servicekernel.CloneAutoscalingStatus(autoscaling)
	return &servicev1.Service{
		ID:                serviceID,
		AutoscalingStatus: servicekernel.CloneAutoscalingStatus(autoscaling),
		UpdatedAt:         timestamppb.New(now),
	}, nil
}

func (f *fakeAutoscalingStatusStore) SyncObservedStatus(context.Context, string, time.Time) (*servicev1.Service, error) {
	panic("unexpected SyncObservedStatus call")
}

func (f *fakeAutoscalingStatusStore) UpdateDeletionStatus(context.Context, string, *servicev1.ServiceDeletionStatus, time.Time) (*servicev1.Service, error) {
	return nil, nil
}

type fakeAutoscalingEvents struct {
	events []*servicev1.ServiceEvent
}

func (f *fakeAutoscalingEvents) RecordEvent(_ context.Context, event *servicev1.ServiceEvent) error {
	f.events = append(f.events, event)
	return nil
}

func TestControllerEvaluateAutoscalingEmitsTargetChangeEvent(t *testing.T) {
	now := time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)
	statuses := &fakeAutoscalingStatusStore{}
	events := &fakeAutoscalingEvents{}
	c := &controller{statuses: statuses, events: events}
	current := &servicev1.Service{
		ID:       "svc-1",
		Replicas: 1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
		AutoscalingStatus: &servicev1.ServiceAutoscalingStatus{
			CurrentDesiredReplicas: 1,
		},
	}
	desired, next, err := c.evaluateAutoscaling(context.Background(), current, now)
	if err != nil {
		t.Fatalf("servicekernel.EvaluateAutoscaling returned error: %v", err)
	}
	if desired != 3 {
		t.Fatalf("desired = %d, want 3", desired)
	}
	if next.GetAutoscalingStatus().GetCurrentDesiredReplicas() != 3 {
		t.Fatalf("persisted desired = %d, want 3", next.GetAutoscalingStatus().GetCurrentDesiredReplicas())
	}
	if len(events.events) != 1 || events.events[0].GetType() != servicev1.ServiceEventType_SERVICE_EVENT_TYPE_AUTOSCALE_TARGET_CHANGED {
		t.Fatalf("events = %#v, want one autoscale target changed event", events.events)
	}
}

func TestControllerEvaluateAutoscalingNoopDoesNotEmitEvent(t *testing.T) {
	now := time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)
	statuses := &fakeAutoscalingStatusStore{}
	events := &fakeAutoscalingEvents{}
	c := &controller{statuses: statuses, events: events}
	current := &servicev1.Service{
		ID:       "svc-1",
		Replicas: 1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
		AutoscalingStatus: &servicev1.ServiceAutoscalingStatus{
			CurrentDesiredReplicas: 3,
			EffectiveMinReplicas:   1,
			EffectiveMaxReplicas:   5,
			ActiveScheduleName:     "business",
			ActiveScheduleReplicas: 3,
			LastEvaluatedAt:        timestamppb.New(now),
			LastAction:             servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_NO_CHANGE,
			Message:                "active schedule \"business\" targets 3 replicas",
		},
	}
	desired, _, err := c.evaluateAutoscaling(context.Background(), current, now)
	if err != nil {
		t.Fatalf("servicekernel.EvaluateAutoscaling returned error: %v", err)
	}
	if desired != 3 {
		t.Fatalf("desired = %d, want 3", desired)
	}
	if len(events.events) != 0 {
		t.Fatalf("events = %#v, want none", events.events)
	}
}

func TestControllerEvaluateAutoscalingFirstActiveScheduleEmitsTargetChangeEvent(t *testing.T) {
	now := time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)
	statuses := &fakeAutoscalingStatusStore{}
	events := &fakeAutoscalingEvents{}
	c := &controller{statuses: statuses, events: events}
	current := &servicev1.Service{
		ID:       "svc-1",
		Replicas: 1,
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
	}
	desired, next, err := c.evaluateAutoscaling(context.Background(), current, now)
	if err != nil {
		t.Fatalf("servicekernel.EvaluateAutoscaling returned error: %v", err)
	}
	if desired != 3 {
		t.Fatalf("desired = %d, want 3", desired)
	}
	if next.GetAutoscalingStatus().GetLastAction() != servicev1.ServiceAutoscalingAction_SERVICE_AUTOSCALING_ACTION_SCALED_UP {
		t.Fatalf("last action = %v, want scaled up", next.GetAutoscalingStatus().GetLastAction())
	}
	if len(events.events) != 1 {
		t.Fatalf("events = %#v, want one autoscale target changed event", events.events)
	}
	if got := events.events[0].GetMessage(); !strings.Contains(got, "previous=1 current=3") {
		t.Fatalf("event message = %q, want manual baseline reflected", got)
	}
}

type fakeReconcileStore struct {
	listCalls      int
	listAutoscaled int
}

func (f *fakeReconcileStore) Create(context.Context, servicekernel.CreateParams, time.Time) (*servicev1.Service, error) {
	panic("unexpected Create call")
}

func (f *fakeReconcileStore) Update(context.Context, *servicev1.UpdateServiceRequest, time.Time) (*servicev1.Service, error) {
	panic("unexpected Update call")
}

func (f *fakeReconcileStore) Delete(context.Context, servicekernel.DeleteParams, time.Time) (*servicev1.Service, bool, error) {
	panic("unexpected Delete call")
}

func (f *fakeReconcileStore) Purge(context.Context, string, time.Time) (string, bool, error) {
	panic("unexpected Purge call")
}

func (f *fakeReconcileStore) AcquireLease(context.Context, string, string, time.Duration, time.Time) (*servicev1.Service, *commonv1.ExecutionLease, error) {
	panic("unexpected AcquireLease call")
}

func (f *fakeReconcileStore) Get(context.Context, string) (*servicev1.Service, bool, error) {
	panic("unexpected Get call")
}

func (f *fakeReconcileStore) List(_ context.Context, _ *servicev1.ServiceListFilter) ([]*servicev1.Service, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeReconcileStore) ListAutoscaled(context.Context) ([]*servicev1.Service, error) {
	f.listAutoscaled++
	return nil, nil
}

func (f *fakeReconcileStore) GetReplica(context.Context, string, string) (*servicev1.ServiceReplica, bool, error) {
	panic("unexpected GetReplica call")
}

func (f *fakeReconcileStore) ListReplicas(context.Context, string, *servicev1.ServiceReplicaListFilter) ([]*servicev1.ServiceReplica, error) {
	panic("unexpected ListReplicas call")
}

func (f *fakeReconcileStore) ListEvents(context.Context, string, int32) ([]*servicev1.ServiceEvent, error) {
	panic("unexpected ListEvents call")
}

func TestReconcilePendingUsesDedicatedAutoscalingSweepReader(t *testing.T) {
	store := &fakeReconcileStore{}
	c := &controller{store: store, autoscaling: store}
	if err := c.ReconcilePending(context.Background(), time.Date(2026, 4, 27, 9, 15, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ReconcilePending returned error: %v", err)
	}
	if store.listCalls != 1 {
		t.Fatalf("List calls = %d, want 1", store.listCalls)
	}
	if store.listAutoscaled != 1 {
		t.Fatalf("ListAutoscaled calls = %d, want 1", store.listAutoscaled)
	}
}
