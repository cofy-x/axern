package pgrun

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const capabilityTestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRecordAllocationCapabilityAdmissionIsAtomicAndLifecycleNeutral(t *testing.T) {
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
	nodeID := "node-capability-" + suffix
	allocationID := "allocation-capability-" + suffix
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes (
			node_id, registered_at, updated_at, last_heartbeat_at, lifecycle_status
		) VALUES ($1, $2, $2, $2, 'active')
	`, nodeID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, labels, created_at, updated_at)
		VALUES ($1, 'default', 'env-test', 'RUN_STATUS_RUNNING', '{}'::jsonb, '{}'::jsonb, $2, $2)
	`, "run-"+suffix, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (
			allocation_id, run_id, node_id, lifecycle_state, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $5)
	`, allocationID, "run-"+suffix, nodeID, commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(), now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM runs WHERE run_id = $1`, "run-"+suffix)
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM nodes WHERE node_id = $1`, nodeID)
	})

	key := capabilitycontract.ExtensionKey("example.com/accelerator", "model-a")
	observation := &capabilityv1.CapabilityObservation{
		Key:        key,
		State:      capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
		ObservedAt: timestamppb.New(now),
		Evidence:   capabilitycontract.ConfigEvidence(capabilityTestDigest),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	snapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-" + suffix,
		Sequence:       1,
		SnapshotID:     "snapshot-" + suffix,
		CollectedAt:    timestamppb.New(now),
		Observations:   []*capabilityv1.CapabilityObservation{observation},
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := pgallocation.InsertCapabilityDependencies(ctx, db.Pool(), allocationID, nodeID, dependencies, now); err != nil {
		t.Fatal(err)
	}

	store := NewStore(db)
	defer store.Close()
	condition := &capabilityv1.CapabilityCondition{
		Key:        capabilitycontract.CloneKey(key),
		State:      capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		Message:    "verified",
		ObservedAt: timestamppb.New(now),
		Proof:      proto.Clone(dependencies[0].GetSelectedObservation()).(*capabilityv1.CapabilityObservationProof),
	}
	malformed := proto.Clone(condition).(*capabilityv1.CapabilityCondition)
	malformed.Message = strings.Repeat("x", capabilitycontract.MaxReasonBytes+1)
	if err := store.RecordAllocationCapabilityAdmission(ctx, allocationID, &allocationkernel.CapabilityAdmission{
		Dependencies: dependencies,
		ConditionSet: &capabilityv1.CapabilityConditionSet{
			Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{malformed},
		},
	}, now); err == nil {
		t.Fatal("malformed condition report succeeded")
	}
	var admitted bool
	if err := db.Pool().QueryRow(ctx, `
		SELECT admitted_dependency IS NOT NULL
		FROM allocation_capability_dependencies
		WHERE allocation_id = $1
	`, allocationID).Scan(&admitted); err != nil {
		t.Fatal(err)
	}
	if admitted {
		t.Fatal("admitted evidence escaped a rolled-back condition transaction")
	}
	degraded := proto.Clone(condition).(*capabilityv1.CapabilityCondition)
	degraded.State = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED
	degraded.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	degraded.Message = "observation unavailable after node create"
	if err := pgallocation.ReplaceCapabilityConditions(ctx, db.Pool(), allocationID, &capabilityv1.CapabilityConditionSet{
		Revision: 3, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{degraded},
	}, now); err != nil {
		t.Fatalf("persist condition report racing ahead of create response: %v", err)
	}

	admission := &allocationkernel.CapabilityAdmission{
		Dependencies: dependencies,
		ConditionSet: &capabilityv1.CapabilityConditionSet{
			Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{condition},
		},
	}
	if err := store.RecordAllocationCapabilityAdmission(ctx, allocationID, admission, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAllocationCapabilityAdmission(ctx, allocationID, admission, now.Add(time.Second)); err != nil {
		t.Fatalf("exact duplicate condition revision was not idempotent: %v", err)
	}
	degradedReplay := proto.Clone(admission.ConditionSet).(*capabilityv1.CapabilityConditionSet)
	degradedReplay.Conditions[0].State = capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED
	degradedReplay.Conditions[0].ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST
	if err := store.RecordAllocationCapabilityAdmission(ctx, allocationID, &allocationkernel.CapabilityAdmission{
		Dependencies: dependencies, ConditionSet: degradedReplay,
	}, now.Add(2*time.Second)); err == nil {
		t.Fatal("create admission replay accepted a mutable degraded condition as historical create proof")
	}
	conflicting := proto.Clone(admission.ConditionSet).(*capabilityv1.CapabilityConditionSet)
	conflicting.Conditions[0].Message = "conflicting same-revision payload"
	if err := store.RecordAllocationCapabilityAdmission(ctx, allocationID, &allocationkernel.CapabilityAdmission{
		Dependencies: dependencies, ConditionSet: conflicting,
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("immutable admission replay should not rewrite the condition projection: %v", err)
	}
	changedProofs := make([]*capabilityv1.CapabilityDependency, len(dependencies))
	for index, dependency := range dependencies {
		changedProofs[index] = proto.Clone(dependency).(*capabilityv1.CapabilityDependency)
	}
	changedProofs[0].SelectedSnapshot.SnapshotID = "replacement-snapshot-" + suffix
	if err := store.RecordAllocationCapabilityAdmission(ctx, allocationID, &allocationkernel.CapabilityAdmission{
		Dependencies: changedProofs, ConditionSet: conflicting,
	}, now.Add(4*time.Second)); err == nil {
		t.Fatal("create retry replaced the immutable admitted proof")
	}
	var lifecycleState string
	var allocationUpdatedAt time.Time
	if err := db.Pool().QueryRow(ctx, `
		SELECT lifecycle_state, updated_at
		FROM allocations WHERE allocation_id = $1
	`, allocationID).Scan(&lifecycleState, &allocationUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if lifecycleState != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String() || !allocationUpdatedAt.Equal(now) {
		t.Fatalf("capability report mutated allocation lifecycle: state=%s updated_at=%s", lifecycleState, allocationUpdatedAt)
	}
	var revision, conditionCount int64
	if err := db.Pool().QueryRow(ctx, `
		SELECT s.revision, count(c.capability_key_id)
		FROM allocation_capability_condition_sets s
		LEFT JOIN allocation_capability_conditions c ON c.allocation_id = s.allocation_id
		WHERE s.allocation_id = $1
		GROUP BY s.revision
	`, allocationID).Scan(&revision, &conditionCount); err != nil {
		t.Fatal(err)
	}
	if revision != 3 || conditionCount != 1 {
		t.Fatalf("condition projection = revision %d count %d", revision, conditionCount)
	}
	var admissionCount int
	if err := db.Pool().QueryRow(ctx, `
		SELECT count(*) FROM allocation_capability_admissions WHERE allocation_id = $1
	`, allocationID).Scan(&admissionCount); err != nil {
		t.Fatal(err)
	}
	if admissionCount != 1 {
		t.Fatalf("immutable capability admission rows = %d, want 1", admissionCount)
	}
}
