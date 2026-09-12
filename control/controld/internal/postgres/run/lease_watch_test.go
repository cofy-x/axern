package pgrun

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

func TestWatchExecutionLeasesWakesAfterCommittedNotification(t *testing.T) {
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
		DELETE FROM execution_leases;
		DELETE FROM runs WHERE run_id = 'run-watch';
		DELETE FROM nodes WHERE node_id = 'node-a';
		UPDATE control_revisions SET revision = 0 WHERE name = 'execution_leases'
	`); err != nil {
		t.Fatalf("reset leases: %v", err)
	}
	now := time.Now().UTC()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (node_id, node_target, registered_at, updated_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-a', 'node-a:24010', $1, $1, $1, 'active')
	`, now); err != nil {
		t.Fatalf("insert lease node: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, labels, created_at, updated_at)
		VALUES ('run-watch', 'default', 'env-watch', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, $1, $1)
	`, now); err != nil {
		t.Fatalf("insert lease run: %v", err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (allocation_id, run_id, node_id, attempt, status, config, created_at, updated_at)
		VALUES ('alloc-watch', 'run-watch', 'node-a', 1, 'ALLOCATION_STATUS_RUNNING', '{}'::jsonb, $1, $1)
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
		leases, revision, err := store.WatchExecutionLeases(ctx, "node-a", 0, time.Now().UTC())
		resultCh <- result{count: len(leases), revision: revision, err: err}
	}()

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin lease transaction: %v", err)
	}
	defer tx.Rollback(context.Background())
	var revision int64
	if err := tx.QueryRow(ctx, `
		UPDATE control_revisions SET revision = revision + 1
		WHERE name = 'execution_leases' RETURNING revision
	`).Scan(&revision); err != nil {
		t.Fatalf("advance revision: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO execution_leases (
			lease_id, allocation_id, node_id, node_target, attempt, lease_type,
			expires_at, revision, revoked, token_hash, created_at
		) VALUES ('lease-watch', 'alloc-watch', 'node-a', 'node-a:24010', 1,
			'LEASE_TYPE_RUN', $1, $2, false, 'token-hash', $3)
	`, time.Now().Add(time.Minute).UTC(), revision, time.Now().UTC()); err != nil {
		t.Fatalf("insert lease: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit lease: %v", err)
	}

	got := <-resultCh
	if got.err != nil {
		t.Fatalf("WatchExecutionLeases() error = %v", got.err)
	}
	if got.count != 1 || got.revision != revision {
		t.Fatalf("WatchExecutionLeases() = count %d revision %d, want 1/%d", got.count, got.revision, revision)
	}
}
