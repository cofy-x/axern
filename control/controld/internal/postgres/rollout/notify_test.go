package pgrollout

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNotificationWaitsDoNotConsumeQueryPool(t *testing.T) {
	const waitersPerKind = 64
	db := newNotificationTestDB(t)
	store := NewStore(db, nil, nil)
	store.StartNotifications()
	t.Cleanup(store.Close)
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.ListenerReady })
	sessionID, sessionTokenHash := registerNotificationWorker(t, store, waitersPerKind)

	results := make(chan error, waitersPerKind*2)
	var started sync.WaitGroup
	started.Add(waitersPerKind * 2)
	for range waitersPerKind {
		go func() {
			started.Done()
			results <- store.WaitForEvents(context.Background(), "rol-capacity", 0, 10*time.Second)
		}()
		go func() {
			started.Done()
			results <- store.WaitForWork(context.Background(), sessionID, sessionTokenHash, 10*time.Second)
		}()
	}
	started.Wait()
	waitForNotificationStats(t, store, func(stats NotificationStats) bool {
		return stats.EventWaiters == waitersPerKind && stats.WorkWaiters == waitersPerKind
	})
	waitForQueryPoolAcquired(t, db, 0)
	queryCtx, cancelQuery := context.WithTimeout(context.Background(), time.Second)
	defer cancelQuery()
	var one int
	if err := db.Pool().QueryRow(queryCtx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("query while %d notification waiters are idle = %d, %v", waitersPerKind*2, one, err)
	}
	if _, err := db.Pool().Exec(queryCtx, `SELECT pg_notify($1, $2)`, rolloutEventChannel, "rol-capacity"); err != nil {
		t.Fatalf("publish rollout notifications: %v", err)
	}
	if _, err := db.Pool().Exec(queryCtx, `
		SELECT pg_notify($1, json_build_object(
			'action','candidate',
			'work_id','wrk-capacity-' || value,
			'kind','WORK_KIND_PLAN',
			'required_agent','',
			'required_wire_api',''
		)::text)
		FROM generate_series(1,$2) AS value
	`, rolloutWorkChannel, waitersPerKind); err != nil {
		t.Fatalf("publish bounded work notifications: %v", err)
	}
	for range waitersPerKind * 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("notification waiter error = %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("notification waiter did not receive shared listener wakeup")
		}
	}
	waitForNotificationStats(t, store, func(stats NotificationStats) bool {
		return stats.EventWaiters == 0 && stats.WorkWaiters == 0
	})
}

func TestNotificationWaitPreservesParentDeadline(t *testing.T) {
	db := newNotificationTestDB(t)
	store := NewStore(db, nil, nil)
	store.StartNotifications()
	t.Cleanup(store.Close)
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.ListenerReady })
	sessionID, sessionTokenHash := registerNotificationWorker(t, store, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := store.WaitForWork(ctx, sessionID, sessionTokenHash, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForWork() error = %v, want parent deadline exceeded", err)
	}
}

func TestSingleWorkNotificationDoesNotWakeAllWaiters(t *testing.T) {
	const waiterCount = 64
	db := newNotificationTestDB(t)
	store := NewStore(db, nil, nil)
	store.StartNotifications()
	t.Cleanup(store.Close)
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.ListenerReady })
	sessionID, sessionTokenHash := registerNotificationWorker(t, store, waiterCount)

	waitCtx, cancelWaiters := context.WithCancel(context.Background())
	results := make(chan error, waiterCount)
	for range waiterCount {
		go func() {
			results <- store.WaitForWork(waitCtx, sessionID, sessionTokenHash, 10*time.Second)
		}()
	}
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.WorkWaiters == waiterCount })
	payload := mustWorkNotificationPayload(t, workNotification{Action: "candidate", WorkID: "wrk-single", Kind: "WORK_KIND_PLAN"})
	if _, err := db.Pool().Exec(context.Background(), `SELECT pg_notify($1,$2)`, rolloutWorkChannel, payload); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-results:
		if err != nil {
			t.Fatalf("selected waiter error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("single actionable notification did not wake a waiter")
	}
	select {
	case err := <-results:
		t.Fatalf("single actionable notification woke more than one waiter: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancelWaiters()
	for range waiterCount - 1 {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled waiter error = %v, want context canceled", err)
		}
	}
}

func TestWorkNotificationAmplificationIsBoundedByReplicaCount(t *testing.T) {
	const waitersPerReplica = 16
	db := newNotificationTestDB(t)
	storeA := NewStore(db, nil, nil)
	storeB := NewStore(db, nil, nil)
	storeA.StartNotifications()
	storeB.StartNotifications()
	t.Cleanup(storeA.Close)
	t.Cleanup(storeB.Close)
	waitForNotificationStats(t, storeA, func(stats NotificationStats) bool { return stats.ListenerReady })
	waitForNotificationStats(t, storeB, func(stats NotificationStats) bool { return stats.ListenerReady })
	sessionID, sessionTokenHash := registerNotificationWorker(t, storeA, waitersPerReplica*2)

	waitCtx, cancelWaiters := context.WithCancel(context.Background())
	results := make(chan error, waitersPerReplica*2)
	startWaiters := func(store *Store) {
		for range waitersPerReplica {
			go func() {
				results <- store.WaitForWork(waitCtx, sessionID, sessionTokenHash, 10*time.Second)
			}()
		}
	}
	startWaiters(storeA)
	startWaiters(storeB)
	waitForNotificationStats(t, storeA, func(stats NotificationStats) bool { return stats.WorkWaiters == waitersPerReplica })
	waitForNotificationStats(t, storeB, func(stats NotificationStats) bool { return stats.WorkWaiters == waitersPerReplica })

	payload := mustWorkNotificationPayload(t, workNotification{Action: "candidate", WorkID: "wrk-multi-replica", Kind: "WORK_KIND_PLAN"})
	if _, err := db.Pool().Exec(context.Background(), `SELECT pg_notify($1,$2)`, rolloutWorkChannel, payload); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("selected replica waiter error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("notification did not wake one waiter per replica")
		}
	}
	select {
	case err := <-results:
		t.Fatalf("single notification exceeded replica amplification bound: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	cancelWaiters()
	for range waitersPerReplica*2 - 2 {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled multi-replica waiter error = %v, want context canceled", err)
		}
	}
}

func TestWaitForWorkUsesDurableWorkerCapabilities(t *testing.T) {
	db := newNotificationTestDB(t)
	store := NewStore(db, nil, nil)
	store.StartNotifications()
	t.Cleanup(store.Close)
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.ListenerReady })

	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollouts(rollout_id,namespace,status,spec,spec_hash,created_at)
		VALUES('rol-selector','default','ROLLOUT_STATUS_QUEUED','{}','sha256:test',$1)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollout_work_items(work_id,kind,rollout_id,status,next_run_at,created_at,updated_at)
		VALUES('wrk-selector','WORK_KIND_PLAN','rol-selector','PENDING',$1,$1,$1)
	`, now); err != nil {
		t.Fatal(err)
	}
	plannerID, plannerHash := registerNotificationWorkerWithCapabilities(t, store, "planner-only", &workerrolloutv1.WorkerCapabilities{Planner: true, MaxConcurrency: 1})
	agentID, agentHash := registerNotificationWorkerWithCapabilities(t, store, "command-agent", &workerrolloutv1.WorkerCapabilities{Agents: []string{"command"}, MaxConcurrency: 1})

	agentCtx, cancelAgent := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelAgent()
	if err := store.WaitForWork(agentCtx, agentID, agentHash, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("agent-only WaitForWork() error = %v, want deadline for incompatible plan", err)
	}
	if err := store.WaitForWork(context.Background(), plannerID, plannerHash, time.Second); err != nil {
		t.Fatalf("compatible planner WaitForWork() error = %v", err)
	}
}

func TestNotificationListenerReconnectsAfterConnectionLoss(t *testing.T) {
	db := newNotificationTestDB(t)
	store := NewStore(db, nil, nil)
	store.StartNotifications()
	t.Cleanup(store.Close)
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.ListenerReady })
	sessionID, sessionTokenHash := registerNotificationWorker(t, store, 1)

	result := make(chan error, 1)
	go func() { result <- store.WaitForWork(context.Background(), sessionID, sessionTokenHash, 10*time.Second) }()
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.WorkWaiters == 1 })
	var terminated bool
	if err := db.Pool().QueryRow(context.Background(), `
		SELECT COALESCE(bool_or(pg_terminate_backend(pid)), false)
		FROM pg_stat_activity
		WHERE datname=current_database() AND application_name=$1 AND pid<>pg_backend_pid()
	`, notificationApplicationName).Scan(&terminated); err != nil {
		t.Fatalf("terminate notification listener: %v", err)
	}
	if !terminated {
		t.Fatal("notification listener backend was not found")
	}
	select {
	case err := <-result:
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("wait after listener loss code = %s, want Unavailable; err=%v", status.Code(err), err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiter did not fail after notification listener connection loss")
	}
	waitForNotificationStats(t, store, func(stats NotificationStats) bool {
		return stats.ListenerReady && stats.WorkWaiters == 0
	})

	recovered := make(chan error, 1)
	go func() {
		recovered <- store.WaitForWork(context.Background(), sessionID, sessionTokenHash, 10*time.Second)
	}()
	waitForNotificationStats(t, store, func(stats NotificationStats) bool { return stats.WorkWaiters == 1 })
	if _, err := db.Pool().Exec(context.Background(), `SELECT pg_notify($1, '')`, rolloutWorkChannel); err != nil {
		t.Fatalf("notify recovered listener: %v", err)
	}
	select {
	case err := <-recovered:
		if err != nil {
			t.Fatalf("wait after listener recovery error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("recovered notification listener did not wake waiter")
	}
}

func TestNotificationListenerUnavailableIsRetryable(t *testing.T) {
	err := notificationWaitError(errors.Join(errNotificationListenerUnavailable, errors.New("connection lost")))
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("notificationWaitError() code = %s, want Unavailable", status.Code(err))
	}
}

func newNotificationTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn, postgres.WithMaxConnections(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE rollout_worker_sessions,rollout_events,rollout_usage_reservations,rollout_artifacts,rollout_work_items,rollout_episodes,rollout_tasks,rollout_plans,rollouts,agent_profile_doctor_jobs,agent_profile_operations,agent_profiles,secrets,namespaces CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pgnamespace.Ensure(context.Background(), db.Pool(), "default"); err != nil {
		t.Fatal(err)
	}
	return db
}

func registerNotificationWorker(t *testing.T, store *Store, concurrency int) (string, string) {
	t.Helper()
	return registerNotificationWorkerWithCapabilities(t, store, "notification-worker", &workerrolloutv1.WorkerCapabilities{
		Planner:        true,
		Agents:         []string{"command"},
		MaxConcurrency: int32(concurrency),
	})
}

func registerNotificationWorkerWithCapabilities(t *testing.T, store *Store, workerID string, capabilities *workerrolloutv1.WorkerCapabilities) (string, string) {
	t.Helper()
	registered, err := store.RegisterWorker(context.Background(), &workerrolloutv1.RegisterWorkerRequest{
		WorkerID:     workerID,
		Capabilities: capabilities,
	}, "bootstrap", time.Now(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return registered.GetSessionID(), rolloutkernel.HashToken(registered.GetSessionToken())
}

func waitForNotificationStats(t *testing.T, store *Store, ready func(NotificationStats) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		stats := store.NotificationStats()
		if ready(stats) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("notification stats did not reach expected state; last=%+v", store.NotificationStats())
}

func waitForQueryPoolAcquired(t *testing.T, db *postgres.DB, want int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if db.Pool().Stat().AcquiredConns() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("query pool acquired connections = %d, want %d", db.Pool().Stat().AcquiredConns(), want)
}
