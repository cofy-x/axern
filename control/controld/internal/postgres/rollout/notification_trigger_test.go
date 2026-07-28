package pgrollout

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestRolloutWorkTriggerEmitsOnlyActionableChanges(t *testing.T) {
	db := newNotificationTestDB(t)
	listener, err := pgx.ConnectConfig(context.Background(), db.Pool().Config().ConnConfig.Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close(context.Background()) })
	if _, err := listener.Exec(context.Background(), `LISTEN `+rolloutWorkChannel); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollouts(rollout_id,namespace,status,spec,spec_hash,created_at)
		VALUES('rol-trigger','default','ROLLOUT_STATUS_QUEUED','{}','sha256:test',$1)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO rollout_work_items(work_id,kind,rollout_id,status,required_wire_api,next_run_at,created_at,updated_at)
		VALUES('wrk-trigger','WORK_KIND_PLAN','rol-trigger','PENDING','AGENT_WIRE_API_ANTHROPIC_MESSAGES',$1,$1,$1)
	`, now); err != nil {
		t.Fatal(err)
	}
	assertWorkNotification(t, listener, "candidate")

	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE rollout_work_items
		SET status='LEASED',claim_owner='worker',lease_token_hash='lease',lease_expires_at=$2,updated_at=$1
		WHERE work_id='wrk-trigger'
	`, now.Add(time.Second), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertNoWorkNotification(t, listener)

	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE rollout_work_items SET lease_expires_at=$2,updated_at=$1 WHERE work_id='wrk-trigger'
	`, now.Add(2*time.Second), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertNoWorkNotification(t, listener)

	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE rollout_work_items
		SET status='PENDING',claim_owner='',lease_token_hash='',lease_expires_at=NULL,next_run_at=$2,updated_at=$1
		WHERE work_id='wrk-trigger'
	`, now.Add(3*time.Second), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertWorkNotification(t, listener, "capacity")

	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE rollout_work_items SET next_run_at=$2,updated_at=$1 WHERE work_id='wrk-trigger'
	`, now.Add(4*time.Second), now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertWorkNotification(t, listener, "candidate")

	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE rollout_work_items
		SET status='LEASED',claim_owner='worker',lease_token_hash='lease-2',lease_expires_at=$2,updated_at=$1
		WHERE work_id='wrk-trigger'
	`, now.Add(5*time.Second), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	assertNoWorkNotification(t, listener)

	if _, err := db.Pool().Exec(context.Background(), `
		UPDATE rollout_work_items SET status='SUCCEEDED',updated_at=$1 WHERE work_id='wrk-trigger'
	`, now.Add(6*time.Second)); err != nil {
		t.Fatal(err)
	}
	assertWorkNotification(t, listener, "capacity")
}

func assertWorkNotification(t *testing.T, listener *pgx.Conn, wantAction string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	notification, err := listener.WaitForNotification(ctx)
	if err != nil {
		t.Fatalf("wait for %s work notification: %v", wantAction, err)
	}
	payload := workNotification{}
	if err := json.Unmarshal([]byte(notification.Payload), &payload); err != nil {
		t.Fatalf("decode work notification %q: %v", notification.Payload, err)
	}
	if payload.Action != wantAction || payload.WorkID != "wrk-trigger" || payload.Kind != "WORK_KIND_PLAN" || payload.RequiredAgent != "" || payload.RequiredWireAPI != "AGENT_WIRE_API_ANTHROPIC_MESSAGES" {
		t.Fatalf("work notification = %+v, want action=%s with durable selector", payload, wantAction)
	}
}

func assertNoWorkNotification(t *testing.T, listener *pgx.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := listener.WaitForNotification(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("non-actionable work update notification error = %v, want deadline exceeded", err)
	}
}
