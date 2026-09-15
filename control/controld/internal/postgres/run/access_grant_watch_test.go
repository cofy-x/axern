package pgrun

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestWatchAllocationAccessGrantsWakesAfterCommittedNotification(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer db.Close()
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		DELETE FROM allocation_access_grants;
		DELETE FROM runs WHERE run_id = 'run-watch';
		DELETE FROM nodes WHERE node_id = 'node-a';
		DELETE FROM node_access_grant_cursors
	`); err != nil {
		t.Fatalf("reset access grants: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO namespaces (namespace, created_at)
		VALUES ('default', $1)
		ON CONFLICT (namespace) DO NOTHING
	`, now); err != nil {
		t.Fatalf("insert lease namespace: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-a', 'node-a:24010', repeat('0', 64), $1, $1, 'active')
	`, now); err != nil {
		t.Fatalf("insert lease node: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ('run-watch', 'default', 'env-watch', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert lease run: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ('alloc-watch', 'run-watch', 'node-a', 'ALLOCATION_LIFECYCLE_STATE_ACTIVE', 1, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert lease allocation: %v", err)
	}

	store := NewStore(db)
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	type result struct {
		count    int
		revision int64
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		grants, revision, err := store.WatchAllocationAccessGrants(ctx, "node-a", 0, time.Now().UTC())
		resultCh <- result{count: len(grants), revision: revision, err: err}
	}()

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin lease transaction: %v", err)
	}
	defer tx.Rollback(context.Background())
	var revision int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO node_access_grant_cursors(node_id, revision) VALUES ('node-a', 1)
		ON CONFLICT (node_id) DO UPDATE SET revision = node_access_grant_cursors.revision + 1 RETURNING revision
	`).Scan(&revision); err != nil {
		t.Fatalf("advance revision: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO allocation_access_grants (
			purpose, grant_id, allocation_id, node_id, expires_at, revision, revoked, token_hash, created_at
		) VALUES ('ALLOCATION_ACCESS_PURPOSE_INTERACTIVE', 'grant-watch', 'alloc-watch', 'node-a', $1, $2, false, 'token-hash', $3)
	`, time.Now().Add(time.Minute).UTC(), revision, time.Now().UTC()); err != nil {
		t.Fatalf("insert access grant: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit access grant: %v", err)
	}

	got := <-resultCh
	if got.err != nil {
		t.Fatalf("WatchAllocationAccessGrants() error = %v", got.err)
	}
	if got.count != 1 || got.revision != revision {
		t.Fatalf("WatchAllocationAccessGrants() = count %d revision %d, want 1/%d", got.count, got.revision, revision)
	}
}

func TestWatchRunWakesAfterCommittedVersionChange(t *testing.T) {
	db := newEnvironmentTestDB(t)
	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id='run-change-watch'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, version, created_at, updated_at)
		VALUES ('run-change-watch', 'default', 'env-watch', 'RUN_STATUS_PLACED', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, 1, $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	insertRunQueryAllocationFixtures(t, db, now, map[string]string{"alloc-change-watch": "run-change-watch"})
	store := NewStore(db)
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		run, err := store.WatchRun(ctx, "run-change-watch", 1)
		if err == nil && run.GetVersion() != 2 {
			err = fmt.Errorf("version = %d, want 2", run.GetVersion())
		}
		result <- err
	}()
	if _, err := db.Pool().Exec(ctx, `UPDATE runs SET version=2, updated_at=$1 WHERE run_id='run-change-watch'`, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestListRunsFiltersAndPaginatesInDatabase(t *testing.T) {
	db := newEnvironmentTestDB(t)
	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id IN ('run-page-a','run-page-b','run-page-c')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, version, created_at, updated_at) VALUES
		('run-page-a', 'team-a', 'env-a', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{"suite":"page"}'::jsonb, 1, $1, $1),
		('run-page-b', 'team-a', 'env-b', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{"suite":"page"}'::jsonb, 1, $1, $1),
		('run-page-c', 'team-b', 'env-c', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{"suite":"page"}'::jsonb, 1, $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	insertRunQueryAllocationFixtures(t, db, now, map[string]string{
		"alloc-page-a": "run-page-a",
		"alloc-page-b": "run-page-b",
		"alloc-page-c": "run-page-c",
	})
	store := NewStore(db)
	defer store.Close()
	filter := &runv1.RunListFilter{Namespace: "team-a", Statuses: []runv1.RunStatus{runv1.RunStatus_RUN_STATUS_RUNNING}, Labels: map[string]string{"suite": "page"}, PageSize: 1}
	first, cursor, err := store.ListRuns(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || cursor == "" {
		t.Fatalf("first page = %#v cursor=%q", first, cursor)
	}
	filter.Cursor = cursor
	second, final, err := store.ListRuns(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || final != "" || second[0].GetID() == first[0].GetID() {
		t.Fatalf("second page = %#v cursor=%q", second, final)
	}
}

func insertRunQueryAllocationFixtures(t *testing.T, db *postgres.DB, now time.Time, allocations map[string]string) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-query-test', 'node-query-test:24010', repeat('0', 64), $1, $1, 'active')
		ON CONFLICT (node_id) DO NOTHING
	`, now); err != nil {
		t.Fatalf("insert query node: %v", err)
	}
	for allocationID, runID := range allocations {
		if _, err := db.Pool().Exec(context.Background(), `
			INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
			VALUES ($1, $2, 'node-query-test', 'ALLOCATION_LIFECYCLE_STATE_BOUND', 1, $3, $3)
		`, allocationID, runID, now); err != nil {
			t.Fatalf("insert query allocation %q: %v", allocationID, err)
		}
	}
}
