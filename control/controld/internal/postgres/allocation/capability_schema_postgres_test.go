package pgallocation

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
)

func TestCapabilitySchemaEnforcesAllocationNodeAndDependencyOwnership(t *testing.T) {
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	ctx := context.Background()
	db, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Pool().Query(ctx, `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'allocations'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatal(err)
	}
	var allocationColumns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		allocationColumns = append(allocationColumns, column)
	}
	rows.Close()
	wantColumns := []string{"allocation_id", "run_id", "node_id", "lifecycle_state", "created_at", "updated_at", "node_active_at"}
	if !slices.Equal(allocationColumns, wantColumns) {
		t.Fatalf("allocation columns = %v, want %v", allocationColumns, wantColumns)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := uuid.NewString()
	allocationID := "allocation-capability-schema-" + suffix
	nodeID := "node-capability-schema-" + suffix
	otherNodeID := "node-capability-schema-other-" + suffix
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes (node_id, node_target, registered_at, updated_at, last_heartbeat_at, lifecycle_status)
		VALUES ($1, '127.0.0.1:1', $3, $3, $3, 'active'), ($2, '127.0.0.1:2', $3, $3, $3, 'active')
	`, nodeID, otherNodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, allocationID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at)
		VALUES ($1, $1, $2, $3, $4, $4)
	`, allocationID, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, allocationID+"-replacement", allocationID, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(), now); err == nil {
		t.Fatal("one Run accepted a replacement Allocation")
	}
	stoppedRunID := allocationID + "-stopped"
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_FAILED', '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, stoppedRunID, now); err != nil {
		t.Fatal(err)
	}
	defer db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id = $1`, stoppedRunID)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $5)
	`, allocationID+"-stopped", stoppedRunID, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED.String(), now); err == nil {
		t.Fatal("node-only STOPPED observation was accepted as durable Allocation state")
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id = $1`, allocationID)
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM nodes WHERE node_id = ANY($1::text[])`, []string{nodeID, otherNodeID})
	})

	insertDependency := func(node, key string) error {
		_, execErr := db.Pool().Exec(ctx, `
			INSERT INTO allocation_capability_dependencies (
				allocation_id, node_id, capability_key_id, capability_key, loss_policy,
				placement_dependency, created_at, updated_at
			) VALUES ($1, $2, $3, '{}'::jsonb, 'CAPABILITY_LOSS_POLICY_DEGRADE', '{}'::jsonb, $4, $4)
		`, allocationID, node, key, now)
		return execErr
	}
	if err := insertDependency(otherNodeID, "platform/1"); err == nil {
		t.Fatal("capability dependency accepted a node different from its allocation")
	}
	if err := insertDependency(nodeID, "platform/1"); err != nil {
		t.Fatal(err)
	}
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_condition_sets (
			allocation_id, revision, payload_digest, observed_at, updated_at
		) VALUES ($1, 1, $2, $3, $3)
	`, allocationID, digest, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_conditions (
			allocation_id, capability_key_id, condition_revision, observed_at, condition, updated_at
		) VALUES ($1, 'platform/2', 1, $2, '{}'::jsonb, $2)
	`, allocationID, now); err == nil {
		t.Fatal("capability condition accepted a key outside the allocation dependency set")
	}
}
