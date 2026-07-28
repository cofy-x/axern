package pgfunction

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	functionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/function"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	functionv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/function/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestAsyncInvocationLeaseFencesStaleCompletion(t *testing.T) {
	store, db := newFunctionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 2, 0, 0, 0, time.UTC)
	deployed := deployFunctionFixture(t, store, now)
	started := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now, time.Minute)

	first, ok, err := store.ClaimAsyncInvocation(ctx, "owner-a", time.Second)
	if err != nil || !ok || first.Invocation.GetID() != started.Invocation.GetID() || first.Attempt != 1 || first.ExecutionGeneration != 1 {
		t.Fatalf("first claim = %+v,%t,%v", first, ok, err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE function_invocations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE invocation_id=$1`, started.Invocation.GetID()); err != nil {
		t.Fatal(err)
	}
	second, ok, err := store.ClaimAsyncInvocation(ctx, "owner-b", time.Minute)
	if err != nil || !ok || second.Invocation.GetID() != started.Invocation.GetID() || second.Attempt != 2 || second.ExecutionGeneration != 2 {
		t.Fatalf("reclaimed invocation = %+v,%t,%v", second, ok, err)
	}
	if _, committed, err := store.FinishAsyncInvocation(ctx, first, functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED, &functionv1.FunctionResult{Data: []byte("stale")}, nil, "stale"); err != nil || committed {
		t.Fatalf("stale completion committed=%t err=%v", committed, err)
	}
	finished, committed, err := store.FinishAsyncInvocation(ctx, second, functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED, &functionv1.FunctionResult{Data: []byte("ok")}, nil, "done")
	if err != nil || !committed || finished.GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED {
		t.Fatalf("current completion = %+v,%t,%v", finished, committed, err)
	}
	var active int32
	if err := db.Pool().QueryRow(ctx, `SELECT active_invocations FROM function_deployments WHERE function_id=$1`, deployed.Function.GetID()).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active invocations = %d, err=%v", active, err)
	}
}

func TestAsyncInvocationDeadlineUsesDatabaseClock(t *testing.T) {
	store, _ := newFunctionTestStore(t)
	ctx := context.Background()
	clientNow := time.Now().UTC().Add(24 * time.Hour)
	deployed := deployFunctionFixture(t, store, clientNow)
	started := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), clientNow, time.Minute)
	if started.Invocation.GetCreatedAt().AsTime().After(time.Now().UTC().Add(time.Minute)) {
		t.Fatalf("invocation created_at used skewed client clock: %s", started.Invocation.GetCreatedAt().AsTime())
	}
	claim, ok, err := store.ClaimAsyncInvocation(ctx, "owner-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimAsyncInvocation() = %+v,%t,%v", claim, ok, err)
	}
	if claim.DeadlineRemaining <= 0 || claim.DeadlineRemaining > time.Minute {
		t.Fatalf("deadline remaining = %s, want within (0,1m]", claim.DeadlineRemaining)
	}
}

func TestAsyncInvocationDeadlineConvergesAndBlocksLifecycleMutation(t *testing.T) {
	store, db := newFunctionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 3, 0, 0, 0, time.UTC)
	deployed := deployFunctionFixture(t, store, now)
	started := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now, time.Second)

	_, err := store.DeployFunction(ctx, functionkernel.DeployParams{
		Namespace: "default",
		Name:      "durable-async",
		Spec:      &functionv1.FunctionSpec{Runtime: "python3.11", Handler: "handler.changed"},
	}, now.Add(time.Millisecond))
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("deploy with active invocation error = %v, want FailedPrecondition", err)
	}
	if _, _, err := store.DeleteFunction(ctx, deployed.Function.GetID(), "", "", now.Add(time.Millisecond)); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("delete with active invocation error = %v, want FailedPrecondition", err)
	}

	if _, err := db.Pool().Exec(ctx, `UPDATE function_invocations SET created_at=clock_timestamp()-interval '2 seconds',deadline_at=clock_timestamp()-interval '1 second' WHERE invocation_id=$1`, started.Invocation.GetID()); err != nil {
		t.Fatal(err)
	}
	expired, err := store.ExpireAsyncInvocations(ctx, 10)
	if err != nil || expired != 1 {
		t.Fatalf("ExpireAsyncInvocations() = %d,%v", expired, err)
	}
	invocation, ok, err := store.GetInvocation(ctx, started.Invocation.GetID())
	if err != nil || !ok || invocation.GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_TIMED_OUT {
		t.Fatalf("expired invocation = %+v,%t,%v", invocation, ok, err)
	}
	var active int32
	if err := db.Pool().QueryRow(ctx, `SELECT active_invocations FROM function_deployments WHERE function_id=$1`, deployed.Function.GetID()).Scan(&active); err != nil || active != 0 {
		t.Fatalf("active invocations after expiry = %d, err=%v", active, err)
	}
}

func TestAsyncInvocationCompletionIsFencedByDatabaseDeadline(t *testing.T) {
	store, db := newFunctionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 3, 30, 0, 0, time.UTC)
	deployed := deployFunctionFixture(t, store, now)
	started := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now, time.Minute)
	claim, ok, err := store.ClaimAsyncInvocation(ctx, "owner-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimAsyncInvocation() = %+v,%t,%v", claim, ok, err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE function_invocations SET created_at=clock_timestamp()-interval '2 seconds',deadline_at=clock_timestamp()-interval '1 second' WHERE invocation_id=$1`, started.Invocation.GetID()); err != nil {
		t.Fatal(err)
	}
	if _, committed, err := store.FinishAsyncInvocation(ctx, claim, functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED, &functionv1.FunctionResult{Data: []byte("late")}, nil, "late"); err != nil || committed {
		t.Fatalf("late completion committed=%t err=%v", committed, err)
	}
	if expired, err := store.ExpireAsyncInvocations(ctx, 10); err != nil || expired != 1 {
		t.Fatalf("ExpireAsyncInvocations() = %d,%v", expired, err)
	}
}

func TestAsyncInvocationRequeueHonorsDurableBackoff(t *testing.T) {
	store, _ := newFunctionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	deployed := deployFunctionFixture(t, store, now)
	started := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now, time.Minute)
	claim, ok, err := store.ClaimAsyncInvocation(ctx, "owner-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimAsyncInvocation() = %+v,%t,%v", claim, ok, err)
	}
	if ok, err := store.RequeueAsyncInvocation(ctx, claim, 2*time.Second, "gateway unavailable"); err != nil || !ok {
		t.Fatalf("RequeueAsyncInvocation() = %t,%v", ok, err)
	}
	if early, ok, err := store.ClaimAsyncInvocation(ctx, "owner-b", time.Minute); err != nil || ok || early != nil {
		t.Fatalf("early claim = %+v,%t,%v", early, ok, err)
	}
	if _, err := store.db.Pool().Exec(ctx, `UPDATE function_invocations SET next_run_at=clock_timestamp()-interval '1 second' WHERE invocation_id=$1`, started.Invocation.GetID()); err != nil {
		t.Fatal(err)
	}
	retried, ok, err := store.ClaimAsyncInvocation(ctx, "owner-b", time.Minute)
	if err != nil || !ok || retried.Invocation.GetID() != started.Invocation.GetID() || retried.Attempt != 2 {
		t.Fatalf("delayed claim = %+v,%t,%v", retried, ok, err)
	}
}

func TestAsyncInvocationClaimRespectsFunctionConcurrency(t *testing.T) {
	store, _ := newFunctionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 5, 0, 0, 0, time.UTC)
	deployed := deployFunctionFixture(t, store, now)
	firstStarted := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now, time.Minute)
	secondStarted := startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now.Add(time.Millisecond), time.Minute)
	first, ok, err := store.ClaimAsyncInvocation(ctx, "owner-a", time.Minute)
	if err != nil || !ok || first.Invocation.GetID() != firstStarted.Invocation.GetID() {
		t.Fatalf("first claim = %+v,%t,%v", first, ok, err)
	}
	if blocked, ok, err := store.ClaimAsyncInvocation(ctx, "owner-b", time.Minute); err != nil || ok || blocked != nil {
		t.Fatalf("claim above function concurrency = %+v,%t,%v", blocked, ok, err)
	}
	if _, committed, err := store.FinishAsyncInvocation(ctx, first, functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_SUCCEEDED, nil, nil, "done"); err != nil || !committed {
		t.Fatalf("finish first invocation = %t,%v", committed, err)
	}
	second, ok, err := store.ClaimAsyncInvocation(ctx, "owner-b", time.Minute)
	if err != nil || !ok || second.Invocation.GetID() != secondStarted.Invocation.GetID() {
		t.Fatalf("second claim = %+v,%t,%v", second, ok, err)
	}
}

func TestConcurrentAsyncClaimsSerializePerFunction(t *testing.T) {
	store, db := newFunctionTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 26, 6, 0, 0, 0, time.UTC)
	deployed := deployFunctionFixture(t, store, now)
	startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now, time.Minute)
	startAsyncInvocationFixture(t, store, deployed.Function.GetID(), now.Add(time.Millisecond), time.Minute)

	if _, err := db.Pool().Exec(ctx, `
		CREATE OR REPLACE FUNCTION test_pause_async_claim() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_sleep(0.2);
			RETURN NEW;
		END;
		$$;
		CREATE TRIGGER test_pause_async_claim
		BEFORE UPDATE ON function_invocations
		FOR EACH ROW
		WHEN (NEW.status = 'FUNCTION_INVOCATION_STATUS_RUNNING' AND OLD.status <> 'FUNCTION_INVOCATION_STATUS_RUNNING')
		EXECUTE FUNCTION test_pause_async_claim();
	`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DROP TRIGGER IF EXISTS test_pause_async_claim ON function_invocations`)
		_, _ = db.Pool().Exec(context.Background(), `DROP FUNCTION IF EXISTS test_pause_async_claim()`)
	})

	type claimResult struct {
		claim *functionkernel.AsyncInvocationClaim
		ok    bool
		err   error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, owner := range []string{"owner-a", "owner-b"} {
		go func() {
			ready.Done()
			<-start
			claim, ok, err := store.ClaimAsyncInvocation(ctx, owner, time.Minute)
			results <- claimResult{claim: claim, ok: ok, err: err}
		}()
	}
	ready.Wait()
	close(start)

	claimed := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.ok {
			claimed++
			if result.claim == nil {
				t.Fatal("successful claim is nil")
			}
		}
	}
	if claimed != 1 {
		t.Fatalf("concurrent claims = %d, want exactly one", claimed)
	}
}

func newFunctionTestStore(t *testing.T) (*Store, *postgres.DB) {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE function_events,function_invocations,function_deployments,function_revisions,function_idempotency_records,function_bundles,functions,namespaces CASCADE`); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, 1)
	t.Cleanup(store.Close)
	return store, db
}

func deployFunctionFixture(t *testing.T, store *Store, now time.Time) *functionkernel.DeployResult {
	t.Helper()
	deployed, err := store.DeployFunction(context.Background(), functionkernel.DeployParams{
		Namespace: "default",
		Name:      "durable-async",
		Spec: &functionv1.FunctionSpec{
			Runtime: "python3.11",
			Handler: "handler.run",
			Timeout: durationpb.New(time.Minute),
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return deployed
}

func startAsyncInvocationFixture(t *testing.T, store *Store, functionID string, now time.Time, timeout time.Duration) *functionkernel.InvocationStartResult {
	t.Helper()
	started, err := store.StartInvocation(context.Background(), functionkernel.InvokeParams{
		FunctionID: functionID,
		Mode:       functionv1.FunctionInvocationMode_FUNCTION_INVOCATION_MODE_ASYNC,
		Timeout:    durationpb.New(timeout),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if started.Invocation.GetStatus() != functionv1.FunctionInvocationStatus_FUNCTION_INVOCATION_STATUS_QUEUED || started.Invocation.GetStartedAt() != nil {
		t.Fatalf("queued invocation = %+v", started.Invocation)
	}
	return started
}
