package pgrollout

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestNotificationHubRoutesAndCoalescesEventChanges(t *testing.T) {
	hub := newTestNotificationHub()
	eventA := testNotificationSubscription(hub, 1, notificationKindEvent, "rol-a", workWaitSelector{})
	eventB := testNotificationSubscription(hub, 2, notificationKindEvent, "rol-b", workWaitSelector{})

	hub.publish(rolloutEventChannel, "rol-a")
	hub.publish(rolloutEventChannel, "rol-a")
	if err := eventA.wait(context.Background()); err != nil {
		t.Fatalf("eventA.wait() error = %v", err)
	}
	assertNotificationWaitBlocks(t, eventA)
	assertNotificationWaitBlocks(t, eventB)
}

func TestNotificationHubWakesOneCompatibleWaiterPerCandidateWork(t *testing.T) {
	hub := newTestNotificationHub()
	planner := testNotificationSubscription(hub, 1, notificationKindWork, "", newWorkWaitSelector(true, nil, []string{"AGENT_WIRE_API_ANTHROPIC_MESSAGES"}))
	commandA := testNotificationSubscription(hub, 2, notificationKindWork, "", newWorkWaitSelector(false, []string{"command"}, []string{"AGENT_WIRE_API_ANTHROPIC_MESSAGES"}))
	commandB := testNotificationSubscription(hub, 3, notificationKindWork, "", newWorkWaitSelector(false, []string{"command"}, []string{"AGENT_WIRE_API_ANTHROPIC_MESSAGES"}))
	other := testNotificationSubscription(hub, 4, notificationKindWork, "", newWorkWaitSelector(false, []string{"codex"}, []string{"AGENT_WIRE_API_OPENAI_RESPONSES"}))

	notification := workNotification{Action: "candidate", WorkID: "wrk-1", Kind: "WORK_KIND_EPISODE", RequiredAgent: "command", RequiredWireAPI: "AGENT_WIRE_API_ANTHROPIC_MESSAGES"}
	hub.publish(rolloutWorkChannel, mustWorkNotificationPayload(t, notification))
	if err := commandA.wait(context.Background()); err != nil {
		t.Fatalf("first compatible waiter error = %v", err)
	}
	assertNotificationWaitBlocks(t, commandB)
	assertNotificationWaitBlocks(t, planner)
	assertNotificationWaitBlocks(t, other)

	notification.WorkID = "wrk-2"
	hub.publish(rolloutWorkChannel, mustWorkNotificationPayload(t, notification))
	if err := commandB.wait(context.Background()); err != nil {
		t.Fatalf("second compatible waiter error = %v", err)
	}
}

func TestNotificationHubRotatesCompatibleCapabilityGroups(t *testing.T) {
	hub := newTestNotificationHub()
	plannerOnlyA := testNotificationSubscription(hub, 1, notificationKindWork, "", newWorkWaitSelector(true, nil, nil))
	plannerOnlyB := testNotificationSubscription(hub, 2, notificationKindWork, "", newWorkWaitSelector(true, nil, nil))
	plannerAgentA := testNotificationSubscription(hub, 3, notificationKindWork, "", newWorkWaitSelector(true, []string{"command"}, nil))
	plannerAgentB := testNotificationSubscription(hub, 4, notificationKindWork, "", newWorkWaitSelector(true, []string{"command"}, nil))
	payload := mustWorkNotificationPayload(t, workNotification{Action: "candidate", WorkID: "wrk-plan", Kind: "WORK_KIND_PLAN"})

	hub.publish(rolloutWorkChannel, payload)
	hub.publish(rolloutWorkChannel, payload)

	if err := plannerOnlyA.wait(context.Background()); err != nil {
		t.Fatalf("planner-only waiter error = %v", err)
	}
	if err := plannerAgentA.wait(context.Background()); err != nil {
		t.Fatalf("planner-agent waiter error = %v", err)
	}
	assertNotificationWaitBlocks(t, plannerOnlyB)
	assertNotificationWaitBlocks(t, plannerAgentB)
}

func TestNotificationHubCapacityWakeIsBoundedByCapabilityGroup(t *testing.T) {
	hub := newTestNotificationHub()
	plannerA := testNotificationSubscription(hub, 1, notificationKindWork, "", newWorkWaitSelector(true, nil, nil))
	plannerB := testNotificationSubscription(hub, 2, notificationKindWork, "", newWorkWaitSelector(true, nil, nil))
	agent := testNotificationSubscription(hub, 3, notificationKindWork, "", newWorkWaitSelector(false, []string{"command"}, nil))

	hub.publish(rolloutWorkChannel, mustWorkNotificationPayload(t, workNotification{Action: "capacity", WorkID: "wrk-done"}))
	if err := plannerA.wait(context.Background()); err != nil {
		t.Fatalf("planner capacity waiter error = %v", err)
	}
	if err := agent.wait(context.Background()); err != nil {
		t.Fatalf("agent capacity waiter error = %v", err)
	}
	assertNotificationWaitBlocks(t, plannerB)
}

func TestNotificationHubFailsWaitersWhenListenerDisconnects(t *testing.T) {
	hub := newTestNotificationHub()
	event := testNotificationSubscription(hub, 1, notificationKindEvent, "rol-a", workWaitSelector{})
	work := testNotificationSubscription(hub, 2, notificationKindWork, "", newWorkWaitSelector(true, nil, nil))

	hub.markUnavailable(errors.New("connection lost"))

	for name, subscription := range map[string]*notificationSubscription{"event": event, "work": work} {
		if err := subscription.wait(context.Background()); !errors.Is(err, errNotificationListenerUnavailable) {
			t.Fatalf("%s wait error = %v, want listener unavailable", name, err)
		}
	}
	stats := hub.stats()
	if stats.ListenerReady || stats.EventWaiters != 0 || stats.WorkWaiters != 0 {
		t.Fatalf("stats after disconnect = %+v, want unavailable with no waiters", stats)
	}
}

func TestNotificationHubStatsSeparateWaiterKinds(t *testing.T) {
	hub := newTestNotificationHub()
	testNotificationSubscription(hub, 1, notificationKindEvent, "rol-a", workWaitSelector{})
	testNotificationSubscription(hub, 2, notificationKindEvent, "rol-b", workWaitSelector{})
	testNotificationSubscription(hub, 3, notificationKindWork, "", newWorkWaitSelector(true, nil, nil))

	stats := hub.stats()
	if stats.EventWaiters != 2 || stats.WorkWaiters != 1 || !stats.ListenerReady {
		t.Fatalf("stats = %+v, want 2 event waiters, 1 work waiter, ready", stats)
	}
}

func testNotificationSubscription(hub *notificationHub, id uint64, kind notificationKind, rolloutID string, selector workWaitSelector) *notificationSubscription {
	subscription := &notificationSubscription{
		id:        id,
		kind:      kind,
		rolloutID: rolloutID,
		selector:  selector,
		hub:       hub,
		changes:   make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	hub.subscribers[id] = subscription
	hub.indexSubscription(subscription)
	return subscription
}

func newTestNotificationHub() *notificationHub {
	return &notificationHub{
		ready:        true,
		readyCh:      closedNotificationSignal(),
		subscribers:  make(map[uint64]*notificationSubscription),
		eventWaiters: make(map[string]map[uint64]*notificationSubscription),
		workWaiters:  make(map[uint64]*notificationSubscription),
		workGroups:   make(map[string]*workWaiterGroup),
		workOrder:    list.New(),
	}
}

func mustWorkNotificationPayload(t *testing.T, notification workNotification) string {
	t.Helper()
	payload, err := json.Marshal(notification)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func assertNotificationWaitBlocks(t *testing.T, subscription *notificationSubscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := subscription.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait() error = %v, want deadline exceeded", err)
	}
}

func closedNotificationSignal() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
