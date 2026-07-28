package pgagentprofile

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	agentprofilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/agentprofile"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDoctorWorkRequiresFrozenProfileWireCapability(t *testing.T) {
	db := newAgentProfileTestDB(t)
	now := time.Date(2026, 7, 20, 7, 30, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO namespaces(namespace,created_at,updated_at) VALUES('default',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at,visibility,owner_type,owner_id)
		VALUES('sec-doctor','default','SECRET_TYPE_OPAQUE','["token"]','ciphertext','{}',1,$1,$1,'INTERNAL','AGENT_PROFILE','apf-doctor')`, now); err != nil {
		t.Fatal(err)
	}
	profileJSON := `{"agent":"claude-code","provider":"AGENT_PROVIDER_ANTHROPIC","wireApi":"AGENT_WIRE_API_ANTHROPIC_MESSAGES","baseUrl":"https://api.example.test","maxConcurrency":1}`
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO agent_profiles(profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,labels,version,created_at,updated_at)
		VALUES('apf-doctor','default','doctor',$1::jsonb,'sec-doctor',1,'{}',1,$2,$2)`, profileJSON, now); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewStore(db, nil).Doctor(ctx, agentprofilekernel.DoctorParams{Namespace: "default", Name: "doctor", Model: "test"}, now)
		done <- err
	}()
	var requiredWireAPI, requiredProfileID string
	var requiredProfileVersion int64
	var requiredProfileConcurrency int32
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		err := db.Pool().QueryRow(context.Background(), `SELECT required_wire_api,required_profile_id,required_profile_version,required_profile_concurrency FROM rollout_work_items WHERE kind='WORK_KIND_PROFILE_DOCTOR'`).Scan(&requiredWireAPI, &requiredProfileID, &requiredProfileVersion, &requiredProfileConcurrency)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if requiredWireAPI != agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES.String() {
		cancel()
		<-done
		t.Fatalf("doctor required_wire_api = %q", requiredWireAPI)
	}
	if requiredProfileID != "apf-doctor" || requiredProfileVersion != 1 || requiredProfileConcurrency != 1 {
		cancel()
		<-done
		t.Fatalf("doctor profile concurrency contract = (%q,%d,%d)", requiredProfileID, requiredProfileVersion, requiredProfileConcurrency)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Doctor() error = %v, want context cancellation", err)
	}
}

func TestOperationResultRejectsIdempotencyKeyUsedForAnotherProfile(t *testing.T) {
	db := newAgentProfileTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO namespaces(namespace,created_at,updated_at) VALUES('default',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at,visibility,owner_type,owner_id)
		VALUES('sec-first','default','SECRET_TYPE_OPAQUE','["token"]','ciphertext','{}',1,$1,$1,'INTERNAL','AGENT_PROFILE','apf-first')`, now); err != nil {
		t.Fatal(err)
	}
	profileJSON := `{"agent":"codex","provider":"AGENT_PROVIDER_OPENAI","wireApi":"AGENT_WIRE_API_OPENAI_RESPONSES","baseUrl":"https://api.openai.com/v1","maxConcurrency":1}`
	if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profiles(profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,labels,version,created_at,updated_at)
		VALUES('apf-first','default','first',$1::jsonb,'sec-first',1,'{}',1,$2,$2)`, profileJSON, now); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	profile := &agentprofilev1.AgentProfile{ID: "apf-first", Namespace: "default", Name: "first", Version: 1}
	if err := recordOperation(ctx, tx, "default", "first", "update", "shared-key", "same-hash", profile, now); err != nil {
		t.Fatal(err)
	}
	if _, matched, err := operationResult(ctx, tx, "default", "second", "update", "shared-key", "same-hash"); status.Code(err) != codes.AlreadyExists || matched {
		t.Fatalf("operationResult() matched=%t err=%v, want AlreadyExists", matched, err)
	}
}

func TestDeleteProfilePreservesCredentialRetainedByFrozenRollout(t *testing.T) {
	db := newAgentProfileTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO namespaces(namespace,created_at,updated_at) VALUES('default',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO secrets(secret_id,namespace,type,data_keys,encrypted_payload,labels,version,created_at,updated_at,visibility,owner_type,owner_id)
		VALUES('sec-retained','default','SECRET_TYPE_OPAQUE','["token"]','ciphertext','{}',1,$1,$1,'INTERNAL','AGENT_PROFILE','apf-retained')`, now); err != nil {
		t.Fatal(err)
	}
	profileJSON := `{"agent":"codex","provider":"AGENT_PROVIDER_OPENAI","wireApi":"AGENT_WIRE_API_OPENAI_RESPONSES","baseUrl":"https://api.openai.com/v1","maxConcurrency":1}`
	if _, err := db.Pool().Exec(ctx, `INSERT INTO agent_profiles(profile_id,namespace,name,spec,credential_secret_id,credential_secret_version,labels,version,created_at,updated_at)
		VALUES('apf-retained','default','production',$1::jsonb,'sec-retained',1,'{}',1,$2,$2)`, profileJSON, now); err != nil {
		t.Fatal(err)
	}
	rolloutSpec := `{"taskSetRef":"registry.example/tasks/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","agent":{"name":"codex","profile":"production"},"model":"openai/test","execution":{"concurrency":1,"attempts":1}}`
	if _, err := db.Pool().Exec(ctx, `INSERT INTO rollouts(rollout_id,namespace,status,spec,spec_hash,idempotency_key,profile_id,frozen_profile,frozen_credential_secret_id,frozen_credential_version,created_at)
		VALUES('rol-retained','default','ROLLOUT_STATUS_READY',$1::jsonb,'hash','key','apf-retained',$2::jsonb,'sec-retained',1,$3)`, rolloutSpec, `{"id":"apf-retained","namespace":"default","name":"production","spec":`+profileJSON+`,"version":1,"credentialVersion":1}`, now); err != nil {
		t.Fatal(err)
	}

	deleted, ok, err := NewStore(db, nil).Delete(ctx, "default", "production", 1)
	if err != nil || !ok || deleted.GetID() != "apf-retained" {
		t.Fatalf("Delete() profile=%v ok=%t err=%v", deleted, ok, err)
	}
	var profiles, credentials, rollouts int
	if err := db.Pool().QueryRow(ctx, `SELECT
		(SELECT count(*) FROM agent_profiles WHERE profile_id='apf-retained'),
		(SELECT count(*) FROM secrets WHERE secret_id='sec-retained'),
		(SELECT count(*) FROM rollouts WHERE rollout_id='rol-retained' AND frozen_credential_secret_id='sec-retained')`).Scan(&profiles, &credentials, &rollouts); err != nil {
		t.Fatal(err)
	}
	if profiles != 0 || credentials != 1 || rollouts != 1 {
		t.Fatalf("profiles=%d credentials=%d rollouts=%d, want 0/1/1", profiles, credentials, rollouts)
	}
}

func newAgentProfileTestDB(t *testing.T) *postgres.DB {
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
	return db
}
