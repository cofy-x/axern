package pgrollout

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
)

const (
	rolloutEventChannel             = "axern_rollout_events"
	rolloutWorkChannel              = "axern_rollout_work_changes"
	notificationApplicationName     = "axern-controld-rollout-notifications"
	notificationReconnectMinDelay   = 100 * time.Millisecond
	notificationReconnectMaxDelay   = 2 * time.Second
	notificationConnectionCloseTime = 2 * time.Second
)

var errNotificationListenerUnavailable = errors.New("rollout notification listener unavailable")

type notificationKind uint8

const (
	notificationKindEvent notificationKind = iota + 1
	notificationKindWork
)

type NotificationStats struct {
	EventWaiters  int
	WorkWaiters   int
	ListenerReady bool
}

type workWaitSelector struct {
	planner  bool
	agents   []string
	wireAPIs []string
}

type workNotification struct {
	Action          string `json:"action"`
	WorkID          string `json:"work_id"`
	Kind            string `json:"kind"`
	RequiredAgent   string `json:"required_agent"`
	RequiredWireAPI string `json:"required_wire_api"`
}

type workWaiterGroup struct {
	key          string
	selector     workWaitSelector
	waiters      *list.List
	orderElement *list.Element
}

type notificationHub struct {
	connConfig *pgx.ConnConfig
	ctx        context.Context
	cancel     context.CancelFunc

	startOnce sync.Once
	wg        sync.WaitGroup

	mu           sync.Mutex
	ready        bool
	readyCh      chan struct{}
	closed       bool
	nextID       uint64
	subscribers  map[uint64]*notificationSubscription
	eventWaiters map[string]map[uint64]*notificationSubscription
	workWaiters  map[uint64]*notificationSubscription
	workGroups   map[string]*workWaiterGroup
	workOrder    *list.List
}

func newNotificationHub(pool *pgxpool.Pool) *notificationHub {
	ctx, cancel := context.WithCancel(context.Background())
	connConfig := pool.Config().ConnConfig.Copy()
	if connConfig.RuntimeParams == nil {
		connConfig.RuntimeParams = make(map[string]string)
	}
	connConfig.RuntimeParams["application_name"] = notificationApplicationName
	return &notificationHub{
		connConfig:   connConfig,
		ctx:          ctx,
		cancel:       cancel,
		readyCh:      make(chan struct{}),
		subscribers:  make(map[uint64]*notificationSubscription),
		eventWaiters: make(map[string]map[uint64]*notificationSubscription),
		workWaiters:  make(map[uint64]*notificationSubscription),
		workGroups:   make(map[string]*workWaiterGroup),
		workOrder:    list.New(),
	}
}

func (h *notificationHub) start() {
	h.startOnce.Do(func() {
		h.wg.Add(1)
		go h.run()
	})
}

func (h *notificationHub) subscribeWork(ctx context.Context, selector workWaitSelector) (*notificationSubscription, error) {
	return h.subscribe(ctx, notificationKindWork, "", selector)
}

func (h *notificationHub) subscribeEvent(ctx context.Context, rolloutID string) (*notificationSubscription, error) {
	return h.subscribe(ctx, notificationKindEvent, rolloutID, workWaitSelector{})
}

func (h *notificationHub) subscribe(ctx context.Context, kind notificationKind, rolloutID string, selector workWaitSelector) (*notificationSubscription, error) {
	h.start()
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errNotificationListenerUnavailable
		}
		if h.ready {
			h.nextID++
			subscription := &notificationSubscription{
				id:        h.nextID,
				kind:      kind,
				rolloutID: rolloutID,
				selector:  selector.normalized(),
				hub:       h,
				changes:   make(chan struct{}, 1),
				done:      make(chan struct{}),
			}
			h.subscribers[subscription.id] = subscription
			h.indexSubscription(subscription)
			h.mu.Unlock()
			return subscription, nil
		}
		readyCh := h.readyCh
		h.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.ctx.Done():
			return nil, errNotificationListenerUnavailable
		case <-readyCh:
		}
	}
}

func (h *notificationHub) run() {
	defer h.wg.Done()
	delay := notificationReconnectMinDelay
	failureReported := false
	for {
		connection, err := pgx.ConnectConfig(h.ctx, h.connConfig)
		if err == nil {
			_, err = connection.Exec(h.ctx, `LISTEN `+rolloutEventChannel)
		}
		if err == nil {
			_, err = connection.Exec(h.ctx, `LISTEN `+rolloutWorkChannel)
		}
		if err != nil {
			reportNotificationListenerFailure(h.ctx, err, &failureReported)
			closeNotificationConnection(connection)
			if !h.waitBeforeReconnect(delay) {
				return
			}
			delay = nextNotificationReconnectDelay(delay)
			continue
		}

		h.markReady()
		if failureReported {
			logrus.Info("rollout notification listener recovered")
			failureReported = false
		}
		delay = notificationReconnectMinDelay
		for {
			notification, err := connection.WaitForNotification(h.ctx)
			if err != nil {
				reportNotificationListenerFailure(h.ctx, err, &failureReported)
				h.markUnavailable(err)
				break
			}
			h.publish(notification.Channel, notification.Payload)
		}
		closeNotificationConnection(connection)
		if !h.waitBeforeReconnect(delay) {
			return
		}
		delay = nextNotificationReconnectDelay(delay)
	}
}

func reportNotificationListenerFailure(ctx context.Context, err error, reported *bool) {
	if ctx.Err() != nil || *reported {
		return
	}
	logrus.WithError(err).Warn("rollout notification listener unavailable")
	*reported = true
}

func (h *notificationHub) markReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ready {
		return
	}
	h.ready = true
	close(h.readyCh)
}

func (h *notificationHub) markUnavailable(cause error) {
	h.mu.Lock()
	if h.ready {
		h.ready = false
		h.readyCh = make(chan struct{})
	}
	subscriptions := make([]*notificationSubscription, 0, len(h.subscribers))
	for id, subscription := range h.subscribers {
		subscriptions = append(subscriptions, subscription)
		if subscription.kind == notificationKindWork {
			h.unindexWorkSubscription(subscription)
		}
		delete(h.subscribers, id)
	}
	h.eventWaiters = make(map[string]map[uint64]*notificationSubscription)
	h.workWaiters = make(map[uint64]*notificationSubscription)
	h.workGroups = make(map[string]*workWaiterGroup)
	h.workOrder.Init()
	h.mu.Unlock()

	for _, subscription := range subscriptions {
		subscription.finish(errors.Join(errNotificationListenerUnavailable, cause))
	}
}

func (h *notificationHub) publish(channel, payload string) {
	h.mu.Lock()
	switch channel {
	case rolloutEventChannel:
		for _, subscription := range h.eventWaiters[payload] {
			subscription.signal()
		}
		h.mu.Unlock()
	case rolloutWorkChannel:
		action, woken := h.publishWork(payload)
		h.mu.Unlock()
		recordRolloutWorkNotification(action, woken)
	default:
		h.mu.Unlock()
	}
}

func (h *notificationHub) publishWork(payload string) (string, int) {
	notification := workNotification{}
	if err := json.Unmarshal([]byte(payload), &notification); err != nil {
		notification.Action = "rescan"
	}
	woken := 0
	switch notification.Action {
	case "candidate":
		woken = h.wakeCompatibleWorkWaiter(notification)
	case "capacity", "rescan":
		woken = h.wakeOnePerWorkGroup()
	default:
		notification.Action = "invalid"
		woken = h.wakeOnePerWorkGroup()
	}
	return notification.Action, woken
}

func (h *notificationHub) wakeCompatibleWorkWaiter(notification workNotification) int {
	for element := h.workOrder.Front(); element != nil; element = element.Next() {
		group := element.Value.(*workWaiterGroup)
		if group.selector.matches(notification) {
			h.wakeWorkGroup(group)
			return 1
		}
	}
	return 0
}

func (h *notificationHub) wakeOnePerWorkGroup() int {
	groups := make([]*workWaiterGroup, 0, len(h.workGroups))
	for element := h.workOrder.Front(); element != nil; element = element.Next() {
		groups = append(groups, element.Value.(*workWaiterGroup))
	}
	for _, group := range groups {
		h.wakeWorkGroup(group)
	}
	return len(groups)
}

func (h *notificationHub) wakeWorkGroup(group *workWaiterGroup) {
	element := group.waiters.Front()
	if element == nil {
		return
	}
	subscription := element.Value.(*notificationSubscription)
	h.unindexWorkSubscription(subscription)
	if group.orderElement != nil {
		h.workOrder.MoveToBack(group.orderElement)
	}
	subscription.signal()
}

func (h *notificationHub) indexSubscription(subscription *notificationSubscription) {
	switch subscription.kind {
	case notificationKindEvent:
		waiters := h.eventWaiters[subscription.rolloutID]
		if waiters == nil {
			waiters = make(map[uint64]*notificationSubscription)
			h.eventWaiters[subscription.rolloutID] = waiters
		}
		waiters[subscription.id] = subscription
	case notificationKindWork:
		h.workWaiters[subscription.id] = subscription
		key := subscription.selector.key()
		group := h.workGroups[key]
		if group == nil {
			group = &workWaiterGroup{key: key, selector: subscription.selector, waiters: list.New()}
			group.orderElement = h.workOrder.PushBack(group)
			h.workGroups[key] = group
		}
		subscription.workGroup = group
		subscription.workElement = group.waiters.PushBack(subscription)
	}
}

func (h *notificationHub) removeSubscription(subscription *notificationSubscription) {
	delete(h.subscribers, subscription.id)
	switch subscription.kind {
	case notificationKindEvent:
		waiters := h.eventWaiters[subscription.rolloutID]
		delete(waiters, subscription.id)
		if len(waiters) == 0 {
			delete(h.eventWaiters, subscription.rolloutID)
		}
	case notificationKindWork:
		delete(h.workWaiters, subscription.id)
		h.unindexWorkSubscription(subscription)
	}
}

func (h *notificationHub) unindexWorkSubscription(subscription *notificationSubscription) {
	group := subscription.workGroup
	if group == nil || subscription.workElement == nil {
		return
	}
	group.waiters.Remove(subscription.workElement)
	subscription.workElement = nil
	subscription.workGroup = nil
	if group.waiters.Len() == 0 {
		delete(h.workGroups, group.key)
		if group.orderElement != nil {
			h.workOrder.Remove(group.orderElement)
			group.orderElement = nil
		}
	}
}

func (h *notificationHub) unsubscribe(subscription *notificationSubscription) {
	if subscription == nil {
		return
	}
	h.mu.Lock()
	h.removeSubscription(subscription)
	h.mu.Unlock()
	subscription.finish(nil)
}

func (h *notificationHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.ready = false
	subscriptions := make([]*notificationSubscription, 0, len(h.subscribers))
	for id, subscription := range h.subscribers {
		subscriptions = append(subscriptions, subscription)
		if subscription.kind == notificationKindWork {
			h.unindexWorkSubscription(subscription)
		}
		delete(h.subscribers, id)
	}
	h.eventWaiters = make(map[string]map[uint64]*notificationSubscription)
	h.workWaiters = make(map[uint64]*notificationSubscription)
	h.workGroups = make(map[string]*workWaiterGroup)
	h.workOrder.Init()
	h.mu.Unlock()

	h.cancel()
	for _, subscription := range subscriptions {
		subscription.finish(errNotificationListenerUnavailable)
	}
	h.wg.Wait()
}

func (h *notificationHub) stats() NotificationStats {
	if h == nil {
		return NotificationStats{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	stats := NotificationStats{ListenerReady: h.ready}
	stats.WorkWaiters = len(h.workWaiters)
	stats.EventWaiters = len(h.subscribers) - stats.WorkWaiters
	return stats
}

func (h *notificationHub) waitBeforeReconnect(delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-h.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextNotificationReconnectDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > notificationReconnectMaxDelay {
		return notificationReconnectMaxDelay
	}
	return delay
}

func closeNotificationConnection(connection *pgx.Conn) {
	if connection == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), notificationConnectionCloseTime)
	defer cancel()
	_ = connection.Close(ctx)
}

type notificationSubscription struct {
	id          uint64
	kind        notificationKind
	rolloutID   string
	selector    workWaitSelector
	hub         *notificationHub
	changes     chan struct{}
	done        chan struct{}
	workGroup   *workWaiterGroup
	workElement *list.Element

	finishOnce sync.Once
	mu         sync.RWMutex
	err        error
}

func newWorkWaitSelector(planner bool, agents, wireAPIs []string) workWaitSelector {
	return workWaitSelector{planner: planner, agents: agents, wireAPIs: wireAPIs}.normalized()
}

func (s workWaitSelector) normalized() workWaitSelector {
	s.agents = normalizedStrings(s.agents)
	s.wireAPIs = normalizedStrings(s.wireAPIs)
	return s
}

func (s workWaitSelector) key() string {
	return fmt.Sprintf("%t|%q|%q", s.planner, s.agents, s.wireAPIs)
}

func (s workWaitSelector) matches(notification workNotification) bool {
	if notification.RequiredWireAPI != "" && !sortedStringsContain(s.wireAPIs, notification.RequiredWireAPI) {
		return false
	}
	switch notification.Kind {
	case "WORK_KIND_PLAN", "WORK_KIND_PROFILE_DOCTOR":
		return s.planner
	case "WORK_KIND_EPISODE":
		return sortedStringsContain(s.agents, notification.RequiredAgent)
	default:
		return false
	}
}

func normalizedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedStringsContain(values []string, value string) bool {
	_, found := sort.Find(len(values), func(index int) int { return strings.Compare(value, values[index]) })
	return found
}

func (s *notificationSubscription) signal() {
	select {
	case s.changes <- struct{}{}:
	default:
	}
}

func (s *notificationSubscription) wait(ctx context.Context) error {
	select {
	case <-s.done:
		return s.result()
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.done:
		return s.result()
	case <-s.changes:
		return nil
	}
}

func (s *notificationSubscription) close() {
	if s != nil && s.hub != nil {
		s.hub.unsubscribe(s)
	}
}

func (s *notificationSubscription) finish(err error) {
	s.finishOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *notificationSubscription) result() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.err != nil {
		return s.err
	}
	return io.EOF
}
