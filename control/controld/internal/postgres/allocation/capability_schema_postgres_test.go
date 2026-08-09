package pgallocation

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
)

func TestCapabilitySchemaEnforcesAllocationNodeAttemptAndDependencyOwnership(t *testing.T) {
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
		INSERT INTO allocations (allocation_id, owner_type, owner_id, node_id, attempt, status, config, created_at, updated_at)
		VALUES ($1, 'run', $1, $2, 2, $3, '{}'::jsonb, $4, $4)
	`, allocationID, nodeID, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM allocations WHERE allocation_id = $1`, allocationID)
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
			allocation_id, allocation_attempt, revision, payload_digest, observed_at, updated_at
		) VALUES ($1, 1, 1, $2, $3, $3)
	`, allocationID, digest, now); err == nil {
		t.Fatal("capability condition set accepted an attempt different from its allocation")
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_condition_sets (
			allocation_id, allocation_attempt, revision, payload_digest, observed_at, updated_at
		) VALUES ($1, 2, 1, $2, $3, $3)
	`, allocationID, digest, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_conditions (
			allocation_id, capability_key_id, allocation_attempt, condition_revision, observed_at, condition, updated_at
		) VALUES ($1, 'platform/2', 2, 1, $2, '{}'::jsonb, $2)
	`, allocationID, now); err == nil {
		t.Fatal("capability condition accepted a key outside the allocation dependency set")
	}
}
