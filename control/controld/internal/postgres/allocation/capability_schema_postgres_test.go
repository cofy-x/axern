package pgallocation

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCapabilitySchemaKeepsRequirementsUnderAllocationOwnership(t *testing.T) {
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
	wantColumns := []string{
		"allocation_id",
		"run_id",
		"node_id",
		"lifecycle_state",
		"cpu_request_milli",
		"sandbox_memory_request_bytes",
		"ephemeral_storage_request_bytes",
		"created_at",
		"updated_at",
		"node_active_at", "output_expires_at",
	}
	if !slices.Equal(allocationColumns, wantColumns) {
		t.Fatalf("allocation columns = %v, want %v", allocationColumns, wantColumns)
	}
	var chargeIndex string
	if err := db.Pool().QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND indexname = 'idx_allocations_resource_charge_node'
	`).Scan(&chargeIndex); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"node_id", "allocation_id", "cpu_request_milli", "sandbox_memory_request_bytes", "ephemeral_storage_request_bytes", "lifecycle_state", "ALLOCATION_LIFECYCLE_STATE_RELEASED"} {
		if !strings.Contains(chargeIndex, required) {
			t.Fatalf("resource charge index %q does not contain %q", chargeIndex, required)
		}
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := uuid.NewString()
	allocationID := "allocation-capability-schema-" + suffix
	nodeID := "node-capability-schema-" + suffix
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO namespaces (namespace, created_at)
		VALUES ('default', $1)
		ON CONFLICT (namespace) DO NOTHING
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
		VALUES ($1, '127.0.0.1:1', repeat('0', 64), $2, $2, 'active')
	`, nodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, allocationID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ($1, $1, $2, $3, 1, $4, $4)
	`, allocationID, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1, $5, $5)
	`, allocationID+"-replacement", allocationID, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND.String(), now); err == nil {
		t.Fatal("one Run accepted a replacement Allocation")
	}
	stoppedRunID := allocationID + "-stopped"
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_FAILED', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, stoppedRunID, now); err != nil {
		t.Fatal(err)
	}
	defer db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id = $1`, stoppedRunID)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1, $5, $5)
	`, allocationID+"-stopped", stoppedRunID, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED.String(), now); err == nil {
		t.Fatal("node-only STOPPED observation was accepted as durable Allocation state")
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id = $1`, allocationID)
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM nodes WHERE node_id = $1`, nodeID)
	})

	insertRequirement := func(key string) error {
		_, execErr := db.Pool().Exec(ctx, `
			INSERT INTO allocation_capability_requirements (
				allocation_id, capability_key_id, capability_key, loss_policy, created_at
			) VALUES ($1, $2, '{}'::jsonb, 'CAPABILITY_LOSS_POLICY_DEGRADE', $3)
		`, allocationID, key, now)
		return execErr
	}
	if err := insertRequirement("platform/1"); err != nil {
		t.Fatal(err)
	}
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	conditionAt := now.Add(time.Second)
	set := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(conditionAt), Conditions: []*capabilityv1.CapabilityCondition{{
		Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}}}
	replaceConditions := func(set *capabilityv1.CapabilityConditionSet, at time.Time) error {
		tx, beginErr := db.Pool().BeginTx(ctx, pgx.TxOptions{})
		if beginErr != nil {
			return beginErr
		}
		defer tx.Rollback(ctx)
		if replaceErr := ReplaceCapabilityConditions(ctx, tx, allocationID, set, at); replaceErr != nil {
			return replaceErr
		}
		return tx.Commit(ctx)
	}
	if err := replaceConditions(set, conditionAt); err != nil {
		t.Fatal(err)
	}
	older := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(now), Conditions: set.GetConditions()}
	if err := replaceConditions(older, conditionAt); err != nil {
		t.Fatalf("older projection was not ignored: %v", err)
	}
	conflict := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(conditionAt), Conditions: []*capabilityv1.CapabilityCondition{{
		Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED,
	}}}
	if err := replaceConditions(conflict, conditionAt); err == nil {
		t.Fatal("equal-time conflicting condition projection was accepted")
	}
	if _, err := db.Pool().Exec(ctx, `
		UPDATE runs
		SET status = 'RUN_STATUS_SUCCEEDED',
			config = '{"declaredOutputs":[{"path":"/tmp/output.patch","format":"DECLARED_OUTPUT_FORMAT_FILE","mediaType":"text/x-diff"}],"rootfsSnapshot":{}}'::jsonb,
			environment_spec = '{"namespace":"default","image":{"ref":"docker.io/library/python:3.12-slim"}}'::jsonb,
			resolved_environment_spec = '{"imageDescriptor":{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","annotations":{"org.opencontainers.image.ref.name":"image:local-import@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}'::jsonb,
			rootfs_snapshot_result = '{"status":"ROOTFS_SNAPSHOT_STATUS_PENDING"}'::jsonb
		WHERE run_id = $1
	`, allocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE allocations SET lifecycle_state = $2 WHERE allocation_id = $1`, allocationID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String()); err != nil {
		t.Fatal(err)
	}
	if err := ScheduleReconcile(ctx, db.Pool(), allocationkernel.ScheduleDeleteRequest(allocationID, now), now); err != nil {
		t.Fatal(err)
	}
	items, err := ClaimDueReconcileItems(ctx, db.Pool(), "output-sealing-test-"+suffix, 100, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var outputItem *allocationkernel.ReconcileItem
	for index := range items {
		if items[index].AllocationID == allocationID {
			outputItem = &items[index]
			break
		}
	}
	if outputItem == nil || len(outputItem.DeclaredOutputs) != 1 || outputItem.DeclaredOutputs[0].GetPath() != "/tmp/output.patch" {
		t.Fatalf("claimed output-sealing contract = %#v", items)
	}
	if !outputItem.RootfsSnapshotRequested {
		t.Fatal("claimed item did not preserve the pending rootfs snapshot intent")
	}
	if got, want := outputItem.BaseImageRef, "docker.io/library/python@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"; got != want {
		t.Fatalf("rootfs snapshot base image = %q, want frozen Run source %q", got, want)
	}
	late := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(conditionAt.Add(time.Second)), Conditions: conflict.GetConditions()}
	if err := replaceConditions(late, conditionAt.Add(time.Second)); err != nil {
		t.Fatalf("late terminal projection was not safely ignored: %v", err)
	}
	var storedAt time.Time
	if err := db.Pool().QueryRow(ctx, `SELECT observed_at FROM allocation_capability_conditions WHERE allocation_id = $1`, allocationID).Scan(&storedAt); err != nil || !storedAt.Equal(conditionAt) {
		t.Fatalf("terminal projection changed stored ordering: observed_at=%s err=%v", storedAt, err)
	}
}
