package pgrollout

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	agentprofilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/agentprofile"
	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

type profileControl struct{ profile *agentprofilev1.AgentProfile }

func (p profileControl) Create(context.Context, agentprofilekernel.CreateParams, time.Time) (*agentprofilev1.AgentProfile, error) {
	panic("unexpected")
}
func (p profileControl) Get(_ context.Context, namespace, name string) (*agentprofilev1.AgentProfile, bool, error) {
	return p.profile, p.profile != nil && p.profile.GetNamespace() == namespace && p.profile.GetName() == name, nil
}
func (p profileControl) List(context.Context, *agentprofilev1.AgentProfileListFilter) ([]*agentprofilev1.AgentProfile, string, error) {
	panic("unexpected")
}
func (p profileControl) Update(context.Context, agentprofilekernel.UpdateParams, time.Time) (*agentprofilev1.AgentProfile, error) {
	panic("unexpected")
}
func (p profileControl) Rotate(context.Context, agentprofilekernel.RotateParams, time.Time) (*agentprofilev1.AgentProfile, error) {
	panic("unexpected")
}
func (p profileControl) Delete(context.Context, string, string, int64) (*agentprofilev1.AgentProfile, bool, error) {
	panic("unexpected")
}
func (p profileControl) ResolveSnapshot(_ context.Context, namespace, name string) (*agentprofilekernel.Snapshot, bool, error) {
	if p.profile == nil || p.profile.GetNamespace() != namespace || p.profile.GetName() != name {
		return nil, false, nil
	}
	return &agentprofilekernel.Snapshot{Profile: p.profile, CredentialSecretID: "sec-test", CredentialVersion: p.profile.GetCredentialVersion()}, true, nil
}
func (p profileControl) Doctor(context.Context, agentprofilekernel.DoctorParams, time.Time) (*agentprofilev1.DoctorAgentProfileResponse, error) {
	panic("unexpected")
}

func TestManualRolloutReadyStartAndDeleteLifecycle(t *testing.T) {
	db := newRolloutTestDB(t)
	store := NewStore(db, nil, nil)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-ready", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"command"}, MaxConcurrency: 1}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sessionHash := rolloutkernel.HashToken(session.GetSessionToken())
	createReady := func(key string) *rolloutv1.Rollout {
		t.Helper()
		created, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", IdempotencyKey: key, StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_MANUAL, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}, now)
		if err != nil {
			t.Fatal(err)
		}
		work, token, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: work.GetID(), ResultDigest: "plan-" + key, SourceDigest: "source", DescriptorDigest: "descriptor", Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "task", TaskDigest: "digest", TaskJson: []byte(`{}`)}}, Preflight: &rolloutv1.PreflightReport{}}, rolloutkernel.HashToken(token), now)
		if err != nil || ready.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_READY {
			t.Fatalf("CompletePlan()=%v,%v", ready, err)
		}
		return created
	}

	startedRollout := createReady("ready-start")
	if work, _, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute); err != nil || work != nil {
		t.Fatalf("HELD episode was claimable: work=%v err=%v", work, err)
	}
	started, ok, err := store.Start(ctx, startedRollout.GetID(), "start-key", now)
	if err != nil || !ok || started.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_QUEUED {
		t.Fatalf("Start()=%v,%t,%v", started, ok, err)
	}
	if _, _, err := store.Start(ctx, startedRollout.GetID(), "start-key", now); err != nil {
		t.Fatalf("idempotent Start()=%v", err)
	}
	if repeated, _, err := store.Start(ctx, startedRollout.GetID(), "different-key", now); err != nil || repeated.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_QUEUED {
		t.Fatalf("repeated Start()=%v,%v", repeated, err)
	}
	if cancelled, ok, err := store.Cancel(ctx, startedRollout.GetID(), now); err != nil || !ok || cancelled.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED {
		t.Fatalf("Cancel(started)=%v,%t,%v", cancelled, ok, err)
	}

	readyForDelete := createReady("ready-delete")
	deleting, ok, err := store.Delete(ctx, readyForDelete.GetID(), now)
	if err != nil || !ok || deleting.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_DELETING {
		t.Fatalf("Delete(READY)=%v,%t,%v", deleting, ok, err)
	}
	var held int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM rollout_work_items WHERE rollout_id=$1 AND status='HELD'`, readyForDelete.GetID()).Scan(&held); err != nil || held != 0 {
		t.Fatalf("held work after delete=%d err=%v", held, err)
	}
}

func TestDurableRolloutCancelConvergesThroughLeasedWork(t *testing.T) {
	db := newRolloutTestDB(t)
	store := NewStore(db, nil, nil)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	params := rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, IdempotencyKey: "cancel-flow", Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}
	created, err := store.Create(ctx, params, now)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.Create(ctx, params, now)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.GetID() != created.GetID() {
		t.Fatalf("idempotent create returned %q, want %q", duplicate.GetID(), created.GetID())
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-1", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"command"}, MaxConcurrency: 1}}, "bootstrap-hash", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	planWork, planToken, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if planWork.GetKind() != workerrolloutv1.WorkKind_WORK_KIND_PLAN {
		t.Fatalf("claimed %s", planWork.GetKind())
	}
	if _, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: planWork.GetID(), ResultDigest: "sha256:plan", SourceDigest: "sha256:source", DescriptorDigest: "sha256:descriptor", PlanJson: []byte(`{}`), Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "task-1", TaskDigest: "sha256:task", TaskJson: []byte(`{"instance":{"id":"task-1"}}`)}}, Preflight: &rolloutv1.PreflightReport{}}, rolloutkernel.HashToken(planToken), now); err != nil {
		t.Fatal(err)
	}
	episodeWork, episodeToken, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: episodeWork.GetID(), ReservationID: "cancelled-worker-usage", MaxTokens: 10, MaxCostMicrousd: 20}, rolloutkernel.HashToken(episodeToken), now); err != nil {
		t.Fatal(err)
	}
	cancelled, ok, err := store.Cancel(ctx, created.GetID(), now.Add(time.Second))
	if err != nil || !ok {
		t.Fatalf("Cancel() = %v,%t,%v", cancelled, ok, err)
	}
	if cancelled.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLING {
		t.Fatalf("status=%s", cancelled.GetStatus())
	}
	terminal, err := store.FailWork(ctx, &workerrolloutv1.FailWorkRequest{WorkID: episodeWork.GetID(), Message: "context canceled"}, rolloutkernel.HashToken(episodeToken), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if terminal.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED {
		t.Fatalf("terminal status=%s", terminal.GetStatus())
	}
	_, episodes, ok, err := store.Get(ctx, created.GetID())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if len(episodes) != 1 || episodes[0].GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_CANCELLED {
		t.Fatalf("episodes=%v", episodes)
	}
	var reservationStatus string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM rollout_usage_reservations WHERE reservation_id='cancelled-worker-usage'`).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "RELEASED" {
		t.Fatalf("reservation status=%s, want RELEASED", reservationStatus)
	}
}

func TestDurableRolloutCancelConvergesAfterWorkerLeaseExpires(t *testing.T) {
	db := newRolloutTestDB(t)
	store := NewStore(db, nil, nil)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 8, 30, 0, 0, time.UTC)
	created, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-crashed", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"command"}, MaxConcurrency: 1}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sessionHash := rolloutkernel.HashToken(session.GetSessionToken())
	plan, planToken, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	planHash := rolloutkernel.HashToken(planToken)
	for index, usage := range []int64{1, 2} {
		reservationID := fmt.Sprintf("usage-preflight-%d", index)
		if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: plan.GetID(), ReservationID: reservationID, MaxTokens: usage, MaxCostMicrousd: usage}, planHash, now); err != nil {
			t.Fatal(err)
		}
		if _, _, err := store.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{WorkID: plan.GetID(), ReservationID: reservationID, InputTokens: usage, CostMicrousd: usage}, planHash, now); err != nil {
			t.Fatal(err)
		}
	}
	planned, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: plan.GetID(), ResultDigest: "plan", SourceDigest: "source", DescriptorDigest: "descriptor", PlanJson: []byte(`{}`), Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "task", TaskDigest: "digest", TaskJson: []byte(`{}`)}}, Preflight: &rolloutv1.PreflightReport{}}, planHash, now)
	if err != nil {
		t.Fatal(err)
	}
	if planned.GetSummary().GetInputTokens() != 3 || planned.GetSummary().GetCostMicrousd() != 3 {
		t.Fatalf("planned usage summary = %+v", planned.GetSummary())
	}
	work, workToken, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
	if err != nil || work == nil {
		t.Fatalf("ClaimWork() = %v,%v", work, err)
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "crashed-worker-usage", MaxTokens: 10, MaxCostMicrousd: 20}, rolloutkernel.HashToken(workToken), now); err != nil {
		t.Fatal(err)
	}
	cancelling, ok, err := store.Cancel(ctx, created.GetID(), now.Add(time.Second))
	if err != nil || !ok || cancelling.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLING {
		t.Fatalf("Cancel() = %v,%t,%v", cancelling, ok, err)
	}
	count, err := store.ReconcileExpiredLeases(ctx, now.Add(2*time.Minute), 10)
	if err != nil || count != 1 {
		t.Fatalf("ReconcileExpiredLeases() = %d,%v", count, err)
	}
	terminal, episodes, ok, err := store.Get(ctx, created.GetID())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if terminal.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED || len(episodes) != 1 || episodes[0].GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_CANCELLED {
		t.Fatalf("terminal=%v episodes=%v", terminal, episodes)
	}
	var reservationStatus string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM rollout_usage_reservations WHERE reservation_id='crashed-worker-usage'`).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "RELEASED" {
		t.Fatalf("reservation status=%s, want RELEASED", reservationStatus)
	}
}

func TestDurableRolloutBudgetExpiryReleasesPendingUsage(t *testing.T) {
	db := newRolloutTestDB(t)
	store := NewStore(db, nil, nil)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 8, 45, 0, 0, time.UTC)
	created, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-budget", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"command"}, MaxConcurrency: 1}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sessionHash := rolloutkernel.HashToken(session.GetSessionToken())
	plan, planToken, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: plan.GetID(), ResultDigest: "plan", SourceDigest: "source", DescriptorDigest: "descriptor", PlanJson: []byte(`{}`), Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "task", TaskDigest: "digest", TaskJson: []byte(`{}`)}}, Preflight: &rolloutv1.PreflightReport{}}, rolloutkernel.HashToken(planToken), now); err != nil {
		t.Fatal(err)
	}
	work, workToken, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
	if err != nil || work == nil {
		t.Fatalf("ClaimWork() = %v,%v", work, err)
	}
	workTokenHash := rolloutkernel.HashToken(workToken)
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "budget-pending-usage", MaxTokens: 10, MaxCostMicrousd: 20}, workTokenHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FailWork(ctx, &workerrolloutv1.FailWorkRequest{WorkID: work.GetID(), Code: "INFRASTRUCTURE", Message: "retry later", Retriable: true}, workTokenHash, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE rollouts SET deadline=$2 WHERE rollout_id=$1`, created.GetID(), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	expired, err := store.ExpireBudgets(ctx, now.Add(3*time.Second))
	if err != nil || expired != 1 {
		t.Fatalf("ExpireBudgets() = %d,%v", expired, err)
	}
	terminal, episodes, ok, err := store.Get(ctx, created.GetID())
	if err != nil || !ok || terminal.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED || terminal.GetFailureClass() != rolloutv1.FailureClass_FAILURE_CLASS_BUDGET || len(episodes) != 1 || episodes[0].GetFailureClass() != rolloutv1.FailureClass_FAILURE_CLASS_BUDGET {
		t.Fatalf("terminal=%v episodes=%v ok=%t err=%v", terminal, episodes, ok, err)
	}
	var reservationStatus string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM rollout_usage_reservations WHERE reservation_id='budget-pending-usage'`).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "RELEASED" {
		t.Fatalf("reservation status=%s, want RELEASED", reservationStatus)
	}
}

func TestPlanningFailurePersistsRolloutFailureClass(t *testing.T) {
	db := newRolloutTestDB(t)
	store := NewStore(db, nil, nil)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 8, 50, 0, 0, time.UTC)
	created, err := store.Create(ctx, rolloutkernel.CreateParams{
		Namespace:   "default",
		StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO,
		Spec: &rolloutv1.RolloutSpec{
			TaskSetRef: "registry.example/tasks/demo@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Agent:      &rolloutv1.RolloutAgent{Name: "command", Command: "true"},
			Execution:  &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1},
		},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{
		WorkerID:     "worker-planning-budget",
		Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, MaxConcurrency: 1},
	}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	work, token, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
	if err != nil || work == nil {
		t.Fatalf("ClaimWork() = %v,%v", work, err)
	}
	if _, err := store.FailWork(ctx, &workerrolloutv1.FailWorkRequest{
		WorkID: work.GetID(), Code: "BUDGET_EXHAUSTED", Message: "rollout usage budget is exhausted",
	}, rolloutkernel.HashToken(token), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	terminal, episodes, ok, err := store.Get(ctx, created.GetID())
	if err != nil || !ok || len(episodes) != 0 || terminal.GetFailureClass() != rolloutv1.FailureClass_FAILURE_CLASS_BUDGET {
		t.Fatalf("terminal=%v episodes=%v ok=%t err=%v", terminal, episodes, ok, err)
	}
}

func TestFailureClassForWorkCode(t *testing.T) {
	tests := map[string]rolloutv1.FailureClass{
		"BUDGET_EXHAUSTED":   rolloutv1.FailureClass_FAILURE_CLASS_BUDGET,
		"METERING_FAILED":    rolloutv1.FailureClass_FAILURE_CLASS_METERING,
		"PREFLIGHT_REJECTED": rolloutv1.FailureClass_FAILURE_CLASS_UNSPECIFIED,
		"WORKER_CRASHED":     rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE,
	}
	for code, expected := range tests {
		if actual := failureClassForWorkCode(code); actual != expected {
			t.Errorf("failureClassForWorkCode(%q) = %s, want %s", code, actual, expected)
		}
	}
}

func TestDurableRolloutMeteringAndCompletionAreIdempotent(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	profile := &agentprofilev1.AgentProfile{ID: "apf-test", Namespace: "default", Name: "test", Version: 1, CredentialVersion: 1, Spec: &agentprofilev1.AgentProfileSpec{Agent: "codex", Provider: agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI, WireApi: agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES, BaseUrl: "https://api.example.test", MaxConcurrency: 2}}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at) VALUES('sec-test','default','SECRET_TYPE_OPAQUE','["token"]',decode('00','hex'),'{}',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	specJSON, _ := json.Marshal(map[string]any{"agent": "codex", "provider": "AGENT_PROVIDER_OPENAI", "wireApi": "AGENT_WIRE_API_OPENAI_RESPONSES", "baseUrl": "https://api.example.test", "maxConcurrency": 2})
	if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profiles(profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,created_at,updated_at) VALUES('apf-test','default','test',$1::jsonb,'sec-test',1,$2,$2)`, specJSON, now); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, profileControl{profile: profile}, nil)
	created, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Agent: &rolloutv1.RolloutAgent{Name: "codex", Profile: "test", ApprovalPolicy: "never"}, Model: "openai/gpt-5", Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}, Budget: &rolloutv1.RolloutBudget{MaxTokens: 100, MaxCostMicrousd: 1000}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-metered", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"codex"}, WireApis: []agentprofilev1.AgentWireApi{agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES}, MaxConcurrency: 1}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	plan, planToken, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	planHash := rolloutkernel.HashToken(planToken)
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: plan.GetID(), ReservationID: "usage-plan", MaxTokens: 1, MaxCostMicrousd: 1}, planHash, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{WorkID: plan.GetID(), ReservationID: "usage-plan"}, planHash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: plan.GetID(), ResultDigest: "plan", SourceDigest: "source", DescriptorDigest: "descriptor", PlanJson: []byte(`{}`), Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "task", TaskDigest: "digest", TaskJson: []byte(`{}`)}}, Preflight: &rolloutv1.PreflightReport{}, UsageReservationID: "usage-plan"}, planHash, now); err != nil {
		t.Fatal(err)
	}
	work, token, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	hash := rolloutkernel.HashToken(token)
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "usage-1", MaxTokens: 50, MaxCostMicrousd: 500}, hash, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{WorkID: work.GetID(), ReservationID: "usage-1", InputTokens: 10, OutputTokens: 5, CostMicrousd: 100}, hash, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "usage-released", MaxTokens: 1, MaxCostMicrousd: 1}, hash, now); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseUsage(ctx, &workerrolloutv1.ReleaseUsageRequest{WorkID: work.GetID(), ReservationID: "usage-released"}, hash, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "usage-released", MaxTokens: 1, MaxCostMicrousd: 1}, hash, now); err == nil {
		t.Fatal("released usage reservation was accepted as an active reservation")
	}
	if _, _, err := store.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{WorkID: work.GetID(), ReservationID: "usage-released"}, hash, now); err == nil {
		t.Fatal("released usage reservation was committed")
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "usage-zero"}, hash, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CommitUsage(ctx, &workerrolloutv1.CommitUsageRequest{WorkID: work.GetID(), ReservationID: "usage-zero", InputTokens: 1}, hash, now); err == nil || !strings.Contains(err.Error(), "exceeds reserved") {
		t.Fatalf("zero allowance commit error = %v", err)
	}
	request := &workerrolloutv1.CompleteEpisodeRequest{WorkID: work.GetID(), ResultDigest: "episode-result", Episode: &rolloutv1.Episode{ID: work.GetEpisodeID(), Status: rolloutv1.EpisodeStatus_EPISODE_STATUS_COMPLETED, Passed: true, Reward: 1, InputTokens: 10, OutputTokens: 5, CostMicrousd: 100}, UsageReservationID: "usage-1"}
	mismatched := &workerrolloutv1.CompleteEpisodeRequest{WorkID: work.GetID(), ResultDigest: "episode-result", Episode: &rolloutv1.Episode{ID: work.GetEpisodeID(), Status: rolloutv1.EpisodeStatus_EPISODE_STATUS_COMPLETED, InputTokens: 11, OutputTokens: 5, CostMicrousd: 100}, UsageReservationID: "usage-1"}
	if _, err := store.CompleteEpisode(ctx, mismatched, hash, now); err == nil || !strings.Contains(err.Error(), "does not match committed") {
		t.Fatalf("mismatched durable usage was accepted: %v", err)
	}
	completed, err := store.CompleteEpisode(ctx, request, hash, now)
	if err != nil {
		t.Fatal(err)
	}
	if completed.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED {
		t.Fatalf("status=%s", completed.GetStatus())
	}
	if _, err := store.CompleteEpisode(ctx, request, hash, now); err != nil {
		t.Fatalf("idempotent completion: %v", err)
	}
	if _, err := store.CompleteEpisode(ctx, request, rolloutkernel.HashToken("wrong"), now); err == nil {
		t.Fatal("completed work accepted wrong lease token")
	}
	if completed.GetID() != created.GetID() {
		t.Fatal("rollout identity changed")
	}
}

func TestPlanningWorkRequiresFrozenProfileWireCapability(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 9, 30, 0, 0, time.UTC)
	profile := &agentprofilev1.AgentProfile{
		ID: "apf-anthropic", Namespace: "default", Name: "anthropic", Version: 1, CredentialVersion: 1,
		Spec: &agentprofilev1.AgentProfileSpec{
			Agent: "claude-code", Provider: agentprofilev1.AgentProvider_AGENT_PROVIDER_ANTHROPIC,
			WireApi: agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES,
			BaseUrl: "https://api.example.test", MaxConcurrency: 1,
		},
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at,visibility,owner_type,owner_id)
		VALUES('sec-test','default','SECRET_TYPE_OPAQUE','["token"]',decode('00','hex'),'{}',1,$1,$1,'INTERNAL','AGENT_PROFILE',$2)`, now, profile.GetID()); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, profileControl{profile: profile}, nil)
	if _, err := store.Create(ctx, rolloutkernel.CreateParams{
		Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO,
		Spec: &rolloutv1.RolloutSpec{
			TaskSetRef: "registry.example/tasks/demo@sha256:abababababababababababababababababababababababababababababababab",
			Agent:      &rolloutv1.RolloutAgent{Name: "claude-code", Profile: "anthropic", ApprovalPolicy: "never"},
			Model:      "test", Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1},
		},
	}, now); err != nil {
		t.Fatal(err)
	}
	register := func(id string, wire agentprofilev1.AgentWireApi) *workerrolloutv1.RegisterWorkerResponse {
		t.Helper()
		session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{
			WorkerID:     id,
			Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, WireApis: []agentprofilev1.AgentWireApi{wire}, MaxConcurrency: 1},
		}, "bootstrap", now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	responses := register("responses-only", agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES)
	if work, _, err := store.ClaimWork(ctx, responses.GetSessionID(), rolloutkernel.HashToken(responses.GetSessionToken()), now, time.Minute); err != nil || work != nil {
		t.Fatalf("responses-only ClaimWork() = %v,%v; want no Anthropic plan", work, err)
	}
	messages := register("messages", agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES)
	work, _, err := store.ClaimWork(ctx, messages.GetSessionID(), rolloutkernel.HashToken(messages.GetSessionToken()), now, time.Minute)
	if err != nil || work == nil || work.GetKind() != workerrolloutv1.WorkKind_WORK_KIND_PLAN {
		t.Fatalf("messages ClaimWork() = %v,%v; want plan", work, err)
	}
}

func TestDurableRolloutClaimEnforcesRolloutConcurrency(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)
	store := NewStore(db, nil, nil)
	created, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-concurrency", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"command"}, MaxConcurrency: 4}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claim := func() (*workerrolloutv1.WorkItem, string) {
		work, token, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		return work, token
	}
	plan, token := claim()
	if _, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: plan.GetID(), ResultDigest: "plan", SourceDigest: "source", DescriptorDigest: "descriptor", PlanJson: []byte(`{}`), Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "one", TaskDigest: "one", TaskJson: []byte(`{}`)}, {TaskID: "two", TaskDigest: "two", TaskJson: []byte(`{}`)}}, Preflight: &rolloutv1.PreflightReport{}}, rolloutkernel.HashToken(token), now); err != nil {
		t.Fatal(err)
	}
	first, _ := claim()
	if first == nil || first.GetKind() != workerrolloutv1.WorkKind_WORK_KIND_EPISODE {
		t.Fatalf("first episode claim = %v", first)
	}
	second, _ := claim()
	if second != nil {
		t.Fatalf("claimed %s while rollout %s was at concurrency limit", second.GetID(), created.GetID())
	}
}

func TestProfileCapacityUsesFrozenProfileVersion(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 10, 30, 0, 0, time.UTC)
	rolloutSpec := `{"agent":{"name":"codex","profile":"production","approvalPolicy":"never"},"execution":{"concurrency":4,"attempts":1}}`
	insertRollout := func(id string, version, limit int) {
		t.Helper()
		frozen := fmt.Sprintf(`{"id":"apf-shared","version":"%d","spec":{"maxConcurrency":%d}}`, version, limit)
		if _, err := db.Pool().Exec(ctx, `INSERT INTO rollouts(rollout_id,namespace,status,spec,spec_hash,profile_id,frozen_profile,created_at)
			VALUES($1,'default','ROLLOUT_STATUS_QUEUED',$2::jsonb,$1,'apf-shared',$3::jsonb,$4)`, id, rolloutSpec, frozen, now); err != nil {
			t.Fatal(err)
		}
	}
	insertRollout("rol-v1-active", 1, 1)
	insertRollout("rol-v1-waiting", 1, 1)
	insertRollout("rol-v2-waiting", 2, 2)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,rollout_id,execution_generation,status,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,claim_owner,lease_token_hash,lease_expires_at,created_at,updated_at)
		VALUES('wrk-v1-active','WORK_KIND_PLAN','rol-v1-active',1,'LEASED','apf-shared',1,1,'{}',$1,'worker','lease',$2,$1,$1)`, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := lockAndCheckWorkCapacity(ctx, tx, &workRecord{kind: "WORK_KIND_PROFILE_DOCTOR", requiredProfileID: "apf-shared", requiredProfileVersion: 1, requiredProfileConcurrency: 1}, now)
	_ = tx.Rollback(ctx)
	if err != nil || allowed {
		t.Fatalf("same frozen version capacity allowed=%t err=%v, want false", allowed, err)
	}
	tx, err = db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err = lockAndCheckWorkCapacity(ctx, tx, &workRecord{kind: "WORK_KIND_PLAN", rolloutID: "rol-v2-waiting", requiredProfileID: "apf-shared", requiredProfileVersion: 2, requiredProfileConcurrency: 2}, now)
	_ = tx.Rollback(ctx)
	if err != nil || !allowed {
		t.Fatalf("new frozen version capacity allowed=%t err=%v, want true", allowed, err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,rollout_id,execution_generation,status,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,created_at,updated_at) VALUES
		('wrk-v1-waiting','WORK_KIND_PLAN','rol-v1-waiting',1,'PENDING','apf-shared',1,1,'{}',$1,$1,$1),
		('wrk-v2-waiting','WORK_KIND_PLAN','rol-v2-waiting',1,'PENDING','apf-shared',2,2,'{}',$1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, nil, nil)
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-profile-capacity", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, MaxConcurrency: 2}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claimed, _, err := store.ClaimWork(ctx, session.GetSessionID(), rolloutkernel.HashToken(session.GetSessionToken()), now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claimed == nil || claimed.GetRolloutID() != "rol-v2-waiting" {
		t.Fatalf("ClaimWork() = %v, want work for frozen Profile v2", claimed)
	}
}

func TestRetriableDoctorInfrastructureFailureReturnsWorkToQueue(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 10, 45, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at,visibility,owner_type,owner_id)
		VALUES('sec-doctor-retry','default','SECRET_TYPE_OPAQUE','["token"]',decode('00','hex'),'{}',1,$1,$1,'INTERNAL','AGENT_PROFILE','apf-doctor-retry')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profiles(profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,labels,version,created_at,updated_at)
		VALUES('apf-doctor-retry','default','doctor-retry','{}','sec-doctor-retry',1,'{}',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profile_doctor_jobs(job_id,profile_id,frozen_profile,frozen_credential_secret_id,model,status,created_at)
		VALUES('doctor-retry','apf-doctor-retry','{}','sec-doctor-retry','test-model','RUNNING',$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,doctor_job_id,execution_generation,status,required_profile_id,required_profile_version,required_profile_concurrency,payload,next_run_at,claim_owner,lease_token_hash,lease_expires_at,attempts,created_at,updated_at)
		VALUES('wrk-doctor-retry','WORK_KIND_PROFILE_DOCTOR','doctor-retry',1,'LEASED','apf-doctor-retry',1,1,'{}',$1,'worker','lease-hash',$2,1,$1,$1)`, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db, nil, nil)
	if _, err := store.FailWork(ctx, &workerrolloutv1.FailWorkRequest{WorkID: "wrk-doctor-retry", Message: "worker restarting", Retriable: true}, "lease-hash", now); err != nil {
		t.Fatal(err)
	}
	var workStatus, jobStatus, leaseHash string
	var nextRunAt time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT w.status,j.status,w.lease_token_hash,w.next_run_at FROM rollout_work_items w JOIN agent_profile_doctor_jobs j ON j.job_id=w.doctor_job_id WHERE w.work_id='wrk-doctor-retry'`).Scan(&workStatus, &jobStatus, &leaseHash, &nextRunAt); err != nil {
		t.Fatal(err)
	}
	if workStatus != "PENDING" || jobStatus != "PENDING" || leaseHash != "" || !nextRunAt.After(now) {
		t.Fatalf("doctor retry state = work:%s job:%s lease:%q next:%s", workStatus, jobStatus, leaseHash, nextRunAt)
	}
}

func TestDurableRolloutRetryRestoresFrozenEpisodeWork(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 18, 11, 0, 0, 0, time.UTC)
	store := NewStore(db, nil, nil)
	created, err := store.Create(ctx, rolloutkernel.CreateParams{Namespace: "default", StartPolicy: rolloutv1.RolloutStartPolicy_ROLLOUT_START_POLICY_AUTO, Spec: &rolloutv1.RolloutSpec{TaskSetRef: "registry.example/tasks/demo@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Agent: &rolloutv1.RolloutAgent{Name: "command", Command: "true"}, Execution: &rolloutv1.RolloutExecution{Concurrency: 1, Attempts: 1}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.RegisterWorker(ctx, &workerrolloutv1.RegisterWorkerRequest{WorkerID: "worker-retry", Capabilities: &workerrolloutv1.WorkerCapabilities{Planner: true, Agents: []string{"command"}, MaxConcurrency: 1}}, "bootstrap", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sessionHash := rolloutkernel.HashToken(session.GetSessionToken())
	plan, planToken, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	taskJSON := []byte(`{"instance":{"id":"retry-task"}}`)
	if _, err := store.CompletePlan(ctx, &workerrolloutv1.CompletePlanRequest{WorkID: plan.GetID(), ResultDigest: "plan", SourceDigest: "source", DescriptorDigest: "descriptor", PlanJson: []byte(`{}`), Tasks: []*workerrolloutv1.PlannedTask{{TaskID: "retry-task", TaskDigest: "task-digest", TaskJson: taskJSON}}, Preflight: &rolloutv1.PreflightReport{}}, rolloutkernel.HashToken(planToken), now); err != nil {
		t.Fatal(err)
	}
	work, token, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: work.GetID(), ReservationID: "failed-worker-usage", MaxTokens: 10, MaxCostMicrousd: 20}, rolloutkernel.HashToken(token), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE rollout_episodes SET passed=TRUE,reward=1,input_tokens=10,cached_input_tokens=2,output_tokens=3,cost_microusd=50,duration_ms=100,execution_facts='{"nodeId":"stale"}'::jsonb WHERE episode_id=$1`, work.GetEpisodeID()); err != nil {
		t.Fatal(err)
	}
	failed, err := store.FailWork(ctx, &workerrolloutv1.FailWorkRequest{WorkID: work.GetID(), Code: "INFRASTRUCTURE", Message: "runtime unavailable"}, rolloutkernel.HashToken(token), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if failed.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED {
		t.Fatalf("failed rollout status=%s", failed.GetStatus())
	}
	var reservationStatus string
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM rollout_usage_reservations WHERE reservation_id='failed-worker-usage'`).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "RELEASED" {
		t.Fatalf("reservation status=%s, want RELEASED", reservationStatus)
	}
	retried, ok, err := store.Retry(ctx, created.GetID(), now.Add(2*time.Second))
	if err != nil || !ok {
		t.Fatalf("Retry() = %v,%t,%v", retried, ok, err)
	}
	if retried.GetStatus() != rolloutv1.RolloutStatus_ROLLOUT_STATUS_QUEUED {
		t.Fatalf("retried rollout status=%s", retried.GetStatus())
	}
	_, episodes, ok, err := store.Get(ctx, created.GetID())
	if err != nil || !ok || len(episodes) != 1 {
		t.Fatalf("Get() episodes=%v ok=%t err=%v", episodes, ok, err)
	}
	episode := episodes[0]
	if episode.GetStatus() != rolloutv1.EpisodeStatus_EPISODE_STATUS_PENDING || episode.GetFailureClass() != rolloutv1.FailureClass_FAILURE_CLASS_UNSPECIFIED || episode.GetPassed() || episode.GetReward() != 0 || episode.GetInputTokens() != 0 || episode.GetCachedInputTokens() != 0 || episode.GetOutputTokens() != 0 || episode.GetCostMicrousd() != 0 || episode.GetDurationMs() != 0 || episode.GetArtifactManifestID() != "" || episode.GetMessage() != "" {
		t.Fatalf("retry retained stale episode projection: %+v", episode)
	}
	retryWork, retryToken, err := store.ClaimWork(ctx, session.GetSessionID(), sessionHash, now.Add(2*time.Second), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if retryWork == nil || retryWork.GetKind() != workerrolloutv1.WorkKind_WORK_KIND_EPISODE || retryWork.GetEpisodeID() != work.GetEpisodeID() {
		t.Fatalf("retry work=%v", retryWork)
	}
	var gotPayload, wantPayload any
	if err := json.Unmarshal(retryWork.GetPayloadJson(), &gotPayload); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(taskJSON, &wantPayload); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPayload, wantPayload) {
		t.Fatalf("retry payload=%s, want %s", retryWork.GetPayloadJson(), taskJSON)
	}
	if _, _, err := store.ReserveUsage(ctx, &workerrolloutv1.ReserveUsageRequest{WorkID: retryWork.GetID(), ReservationID: "pending-worker-usage", MaxTokens: 10, MaxCostMicrousd: 20}, rolloutkernel.HashToken(retryToken), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FailWork(ctx, &workerrolloutv1.FailWorkRequest{WorkID: retryWork.GetID(), Code: "INFRASTRUCTURE", Message: "retry later", Retriable: true}, rolloutkernel.HashToken(retryToken), now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Cancel(ctx, created.GetID(), now.Add(4*time.Second)); err != nil || !ok {
		t.Fatalf("Cancel(pending retry) ok=%t err=%v", ok, err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT status FROM rollout_usage_reservations WHERE reservation_id='pending-worker-usage'`).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != "RELEASED" {
		t.Fatalf("pending reservation status=%s, want RELEASED", reservationStatus)
	}
}

func TestReconcileDoctorJobsRemovesExpiredJobsAndUnreferencedCredentials(t *testing.T) {
	db := newRolloutTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"sec-current", "sec-terminal", "sec-recent", "sec-orphan"} {
		if _, err := db.Pool().Exec(ctx, `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at,visibility,owner_type,owner_id)
			VALUES($1,'default','SECRET_TYPE_OPAQUE','["token"]',decode('00','hex'),'{}',1,$2,$2,'INTERNAL','AGENT_PROFILE','apf-doctor')`, id, now.Add(-48*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profiles(profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,created_at,updated_at)
		VALUES('apf-doctor','default','doctor','{}','sec-current',4,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	type doctorJob struct {
		id        string
		secretID  string
		status    string
		createdAt time.Time
		completed *time.Time
	}
	terminalAt := now.Add(-doctorTerminalRetention - time.Second)
	recentAt := now.Add(-doctorTerminalRetention + time.Second)
	jobs := []doctorJob{
		{id: "doctor-terminal", secretID: "sec-terminal", status: "COMPLETED", createdAt: now.Add(-time.Hour), completed: &terminalAt},
		{id: "doctor-recent", secretID: "sec-recent", status: "COMPLETED", createdAt: now.Add(-time.Hour), completed: &recentAt},
		{id: "doctor-orphan", secretID: "sec-orphan", status: "PENDING", createdAt: now.Add(-doctorOrphanRetention - time.Second)},
	}
	for _, job := range jobs {
		if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profile_doctor_jobs(job_id,profile_id,frozen_profile,frozen_credential_secret_id,model,status,created_at,completed_at)
			VALUES($1,'apf-doctor','{}',$2,'test-model',$3,$4,$5)`, job.id, job.secretID, job.status, job.createdAt, job.completed); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO rollout_work_items(work_id,kind,doctor_job_id,execution_generation,status,payload,next_run_at,created_at,updated_at)
			VALUES('wrk-'||$1,'WORK_KIND_PROFILE_DOCTOR',$1,1,'PENDING','{}',$2,$2,$2)`, job.id, job.createdAt); err != nil {
			t.Fatal(err)
		}
	}

	store := NewStore(db, nil, nil)
	deleted, err := store.ReconcileDoctorJobs(ctx, now, 100)
	if err != nil || deleted != 2 {
		t.Fatalf("ReconcileDoctorJobs() = %d,%v; want 2,nil", deleted, err)
	}
	for _, id := range []string{"doctor-terminal", "doctor-orphan"} {
		var count int
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM agent_profile_doctor_jobs WHERE job_id=$1`, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("expired doctor job %s count=%d err=%v", id, count, err)
		}
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM rollout_work_items WHERE doctor_job_id=$1`, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("expired doctor work %s count=%d err=%v", id, count, err)
		}
	}
	for _, id := range []string{"sec-terminal", "sec-orphan"} {
		var count int
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM secrets WHERE secret_id=$1`, id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("expired credential %s count=%d err=%v", id, count, err)
		}
	}
	for _, id := range []string{"doctor-recent", "sec-recent", "sec-current"} {
		var count int
		query := `SELECT count(*) FROM secrets WHERE secret_id=$1`
		if id == "doctor-recent" {
			query = `SELECT count(*) FROM agent_profile_doctor_jobs WHERE job_id=$1`
		}
		if err := db.Pool().QueryRow(ctx, query, id).Scan(&count); err != nil || count != 1 {
			t.Fatalf("retained row %s count=%d err=%v", id, count, err)
		}
	}
}

func newRolloutTestDB(t *testing.T) *postgres.DB {
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
	if _, err := db.Pool().Exec(context.Background(), `TRUNCATE TABLE rollout_worker_sessions,rollout_events,rollout_usage_reservations,rollout_artifacts,rollout_work_items,rollout_episodes,rollout_tasks,rollout_plans,rollouts,agent_profile_doctor_jobs,agent_profile_operations,agent_profiles,secrets,namespaces CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pgnamespace.Ensure(context.Background(), db.Pool(), "default"); err != nil {
		t.Fatal(err)
	}
	return db
}
