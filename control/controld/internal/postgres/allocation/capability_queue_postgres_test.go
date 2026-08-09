package pgallocation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCapabilityQueueCompletionPreservesNewerGeneration(t *testing.T) {
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
	nodeID := "node-capability-queue-" + suffix
	allocationID := "allocation-capability-queue-" + suffix
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes (node_id, node_target, registered_at, updated_at, last_heartbeat_at, lifecycle_status)
		VALUES ($1, '127.0.0.1:1', $2, $2, $2, 'active')
	`, nodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, owner_type, owner_id, node_id, attempt, status, config, created_at, updated_at)
		VALUES ($1, 'run', $1, $2, 1, $3, '{}'::jsonb, $4, $4)
	`, allocationID, nodeID, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool().Exec(context.Background(), `DELETE FROM nodes WHERE node_id = $1`, nodeID) })

	key := capabilitycontract.ExtensionKey("example.com/queue", "v1")
	observation := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
		ObservedAt: timestamppb.New(now), Evidence: capabilitycontract.ConfigEvidence("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	snapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-" + suffix, Sequence: 1, SnapshotID: "snapshot-" + suffix,
		CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{observation},
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := InsertCapabilityDependencies(ctx, db.Pool(), allocationID, nodeID, dependencies, now); err != nil {
		t.Fatal(err)
	}
	keyID, _ := capabilitycontract.KeyID(key)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_reconcile_queue (allocation_id, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $2, $2)
	`, allocationID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_reconcile_pending_keys
			(allocation_id, capability_key_id, snapshot_sequence, created_at, updated_at)
		VALUES ($1, $3, 1, $2, $2)
	`, allocationID, now, keyID); err != nil {
		t.Fatal(err)
	}

	queue := NewCapabilityQueue(db)
	claimed, err := queue.Claim(ctx, "worker-1", 1, now, 30*time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].PendingGenerations[keyID] != 1 {
		t.Fatalf("first claim = (%#v, %v), want generation 1", claimed, err)
	}
	conditionSet := &capabilityv1.CapabilityConditionSet{
		Revision: 1, ObservedAt: timestamppb.New(now),
		Conditions: []*capabilityv1.CapabilityCondition{{
			Key: capabilitycontract.CloneKey(key), State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, ObservedAt: timestamppb.New(now),
			Proof: proto.Clone(dependencies[0].GetSelectedObservation()).(*capabilityv1.CapabilityObservationProof),
		}},
	}
	if err := queue.RecordConditions(ctx, claimed[0], "worker-1", &allocationkernel.CapabilityReconciliation{
		Attempt: 1, Dependencies: dependencies, ConditionSet: conditionSet,
	}, now); err != nil {
		t.Fatal(err)
	}
	var admittedProofWasRewritten bool
	if err := db.Pool().QueryRow(ctx, `
		SELECT admitted_dependency IS NOT NULL
		FROM allocation_capability_dependencies
		WHERE allocation_id = $1 AND capability_key_id = $2
	`, allocationID, keyID).Scan(&admittedProofWasRewritten); err != nil {
		t.Fatal(err)
	}
	if admittedProofWasRewritten {
		t.Fatal("runtime capability reconciliation rewrote the immutable create admission proof")
	}
	transitionAt := now.Add(time.Second)
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_capability_reconcile_queue
			(allocation_id, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $2, $2)
		ON CONFLICT (allocation_id) DO UPDATE SET
			next_run_at = LEAST(allocation_capability_reconcile_queue.next_run_at, EXCLUDED.next_run_at),
			updated_at = EXCLUDED.updated_at
	`, allocationID, transitionAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		UPDATE allocation_capability_reconcile_pending_keys
		SET snapshot_sequence = 2, updated_at = $2
		WHERE allocation_id = $1 AND capability_key_id = $3
	`, allocationID, transitionAt, keyID); err != nil {
		t.Fatal(err)
	}
	var activeLeaseOwner string
	if err := db.Pool().QueryRow(ctx, `
		SELECT lease_owner
		FROM allocation_capability_reconcile_queue
		WHERE allocation_id = $1
	`, allocationID).Scan(&activeLeaseOwner); err != nil {
		t.Fatal(err)
	}
	if activeLeaseOwner != "worker-1" {
		t.Fatalf("new transition changed active lease owner to %q, want worker-1", activeLeaseOwner)
	}
	if err := queue.Complete(ctx, claimed[0], "worker-1", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}

	var generation int64
	var leaseOwner string
	if err := db.Pool().QueryRow(ctx, `
		SELECT p.snapshot_sequence, q.lease_owner
		FROM allocation_capability_reconcile_pending_keys p
		JOIN allocation_capability_reconcile_queue q USING (allocation_id)
		WHERE p.allocation_id = $1 AND p.capability_key_id = $2
	`, allocationID, keyID).Scan(&generation, &leaseOwner); err != nil {
		t.Fatal(err)
	}
	if generation != 2 || leaseOwner != "" {
		t.Fatalf("newer work after completion = generation %d lease %q, want generation 2 and released lease", generation, leaseOwner)
	}

	claimed, err = queue.Claim(ctx, "worker-2", 1, now.Add(3*time.Second), 30*time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].PendingGenerations[keyID] != 2 {
		t.Fatalf("second claim = (%#v, %v), want generation 2", claimed, err)
	}
	if err := queue.Retry(ctx, claimed[0], "worker-2", now.Add(34*time.Second), errors.New("late worker")); err == nil {
		t.Fatal("expired worker lease was allowed to reschedule capability work")
	}
	claimed, err = queue.Claim(ctx, "worker-3", 1, now.Add(34*time.Second), 30*time.Second)
	if err != nil || len(claimed) != 1 || claimed[0].PendingGenerations[keyID] != 2 {
		t.Fatalf("reclaim after expiry = (%#v, %v), want generation 2", claimed, err)
	}
	if err := queue.Complete(ctx, claimed[0], "worker-3", now.Add(35*time.Second)); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM allocation_capability_reconcile_queue WHERE allocation_id = $1`, allocationID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("completed queue rows = %d, want 0", remaining)
	}
}
