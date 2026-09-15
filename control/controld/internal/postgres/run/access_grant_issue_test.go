package pgrun

import (
	"context"
	"testing"
	"time"

	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAccessGrantsFollowAllocationStateAndOutputDeadline(t *testing.T) {
	db := newEnvironmentTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := db.Pool().Exec(ctx, `
 INSERT INTO runs(run_id,namespace,environment_id,status,config,environment_spec,resolved_environment_spec,labels,created_at,updated_at)
 VALUES ('run-output','default','env-output','RUN_STATUS_RUNNING','{}','{}','{}','{}',$1,$1)
 `, now)
	if err != nil {
		t.Fatal(err)
	}
	insertRunQueryAllocationFixtures(t, db, now, map[string]string{"alloc-output": "run-output"})
	s := NewStore(db)
	defer s.Close()
	interactive := gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_INTERACTIVE
	output := gatewayv1.AllocationAccessPurpose_ALLOCATION_ACCESS_PURPOSE_RUN_OUTPUT
	if _, err := s.IssueAllocationAccessGrant(ctx, "alloc-output", interactive, time.Minute, now); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("bound allocation accepted: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE allocations SET lifecycle_state='ALLOCATION_LIFECYCLE_STATE_ACTIVE' WHERE allocation_id='alloc-output'"); err != nil {
		t.Fatal(err)
	}
	first, err := s.IssueAllocationAccessGrant(ctx, "alloc-output", interactive, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.IssueAllocationAccessGrant(ctx, "alloc-output", interactive, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "UPDATE allocations SET lifecycle_state='ALLOCATION_LIFECYCLE_STATE_RELEASING' WHERE allocation_id='alloc-output'"); err != nil {
		t.Fatal(err)
	}
	if err := pgallocation.RevokeAccessGrants(ctx, tx, "alloc-output"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	grants, _, err := s.loadAllocationAccessGrants(ctx, first.NodeID, second.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 2 || !grants[0].Revoked || !grants[1].Revoked || grants[0].Revision != grants[1].Revision {
		t.Fatalf("revoke not atomic: %+v", grants)
	}
	if _, err := s.IssueAllocationAccessGrant(ctx, "alloc-output", interactive, time.Minute, now); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("terminal execution access: %v", err)
	}
	g, err := s.IssueAllocationAccessGrant(ctx, "alloc-output", output, time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	if !g.ExpiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiry=%v", g.ExpiresAt)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE allocations SET lifecycle_state='ALLOCATION_LIFECYCLE_STATE_RELEASED',updated_at=$1,output_expires_at=$2 WHERE allocation_id='alloc-output'", now.Add(time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	g, err = s.IssueAllocationAccessGrant(ctx, "alloc-output", output, time.Hour, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !g.ExpiresAt.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("cleanup extended output deadline: %v", g.ExpiresAt)
	}
	if _, err := s.IssueAllocationAccessGrant(ctx, "alloc-output", output, time.Minute, now.Add(15*time.Minute)); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expired output accepted: %v", err)
	}
}

func TestAccessGrantCursorRollbackAndNodeIsolation(t *testing.T) {
	db := newEnvironmentTestDB(t)
	ctx := context.Background()
	for _, id := range []string{"node-one", "node-two"} {
		if _, err := db.Pool().Exec(ctx, "INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,last_heartbeat_at,lifecycle_status) VALUES($1,$1,repeat('0',64),now(),now(),'active')", id); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := pgallocation.NextAccessGrantRevision(ctx, tx, "node-one"); err != nil {
		t.Fatal(err)
	}
	other, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback(ctx)
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if revision, err := pgallocation.NextAccessGrantRevision(bounded, other, "node-two"); err != nil || revision != 1 {
		t.Fatalf("unrelated node blocked: %d %v", revision, err)
	}
	if err := other.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	retry, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Rollback(ctx)
	if revision, err := pgallocation.NextAccessGrantRevision(ctx, retry, "node-one"); err != nil || revision != 1 {
		t.Fatalf("rollback advanced cursor: %d %v", revision, err)
	}
}
