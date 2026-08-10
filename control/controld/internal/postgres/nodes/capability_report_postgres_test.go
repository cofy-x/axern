package pgnodes

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgallocation "github.com/cofy-x/axern/control/controld/internal/postgres/allocation"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCapabilityReportUsesDependencyIndexAndRollsBackTransitionQueueAtomically(t *testing.T) {
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

	suffix := uuid.NewString()
	nodeID := "node-capability-report-" + suffix
	affectedID := "allocation-affected-" + suffix
	admissionOnlyID := "allocation-admission-only-" + suffix
	unrelatedID := "allocation-unrelated-" + suffix
	terminalID := "allocation-terminal-" + suffix
	releasingID := "allocation-releasing-" + suffix
	authToken := "token-" + suffix
	store := NewPGStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := store.Register(ctx, nodekernel.RegisterParams{NodeID: nodeID, NodeTarget: "127.0.0.1:1", Runtimes: []string{"runsc"}, NodeAuthToken: authToken, Now: now}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool().Exec(context.Background(), `DELETE FROM nodes WHERE node_id = $1`, nodeID) })

	affectedKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	admissionOnlyKey := capabilitycontract.ExtensionKey("example.com/admission-only", "v1")
	unrelatedKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	initial := capabilityReportSnapshot(now, 1, "snapshot-initial-"+suffix,
		availableConfigObservation(affectedKey, now),
		availableConfigObservation(admissionOnlyKey, now),
		availableConfigObservation(unrelatedKey, now),
	)
	if _, err := store.Report(ctx, nodekernel.ReportParams{
		NodeID: nodeID, NodeTarget: "127.0.0.1:1", Runtimes: []string{"runsc"},
		NodeAuthToken: authToken, Now: now, Summary: capabilityReportSummary(initial),
	}); err != nil {
		t.Fatal(err)
	}

	insertAllocation := func(allocationID string, status commonv1.AllocationStatus) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, `
			INSERT INTO allocations (
				allocation_id, owner_type, owner_id, node_id, attempt, status, config, created_at, updated_at
			) VALUES ($1, 'run', $1, $2, 1, $3, '{}'::jsonb, $4, $4)
		`, allocationID, nodeID, status.String(), now); err != nil {
			t.Fatal(err)
		}
	}
	insertAllocation(affectedID, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING)
	insertAllocation(admissionOnlyID, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING)
	insertAllocation(unrelatedID, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING)
	insertAllocation(terminalID, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED)
	insertAllocation(releasingID, commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING)
	for allocationID, key := range map[string]*capabilityv1.CapabilityKey{
		affectedID: affectedKey, admissionOnlyID: admissionOnlyKey, unrelatedID: unrelatedKey,
		terminalID: affectedKey, releasingID: affectedKey,
	} {
		dependencies, resolveErr := capabilitycontract.ResolveDependencies(initial, []*capabilityv1.CapabilityKey{key}, now)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if err := pgallocation.InsertCapabilityDependencies(ctx, db.Pool(), allocationID, nodeID, dependencies, now); err != nil {
			t.Fatal(err)
		}
	}

	nextTime := now.Add(time.Second)
	next := capabilityReportSnapshot(nextTime, 2, "snapshot-loss-"+suffix,
		unavailableConfigObservation(affectedKey, nextTime),
		unavailableConfigObservation(admissionOnlyKey, nextTime),
		availableConfigObservation(unrelatedKey, nextTime),
	)
	record, err := store.Report(ctx, nodekernel.ReportParams{
		NodeID: nodeID, NodeTarget: "127.0.0.1:1", Runtimes: []string{"runsc"},
		NodeAuthToken: authToken, Now: nextTime, Summary: capabilityReportSummary(next),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(record.ReportedCapabilityTransitions) != 2 {
		t.Fatalf("reported transitions = %d, want 2", len(record.ReportedCapabilityTransitions))
	}
	rows, err := db.Pool().Query(ctx, `SELECT allocation_id FROM allocation_capability_reconcile_queue ORDER BY allocation_id`)
	if err != nil {
		t.Fatal(err)
	}
	var queued []string
	for rows.Next() {
		var allocationID string
		if err := rows.Scan(&allocationID); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(allocationID, suffix) {
			queued = append(queued, allocationID)
		}
	}
	rows.Close()
	if len(queued) != 1 || queued[0] != affectedID {
		t.Fatalf("indexed capability queue = %#v, want [%q]", queued, affectedID)
	}

	// A snapshot ID is an idempotency identity, not a display label. Reusing
	// an older ID for a new sequence must abort the report; otherwise the
	// transition ON CONFLICT path would silently discard history.
	reusedTime := nextTime.Add(time.Second)
	reused := capabilityReportSnapshot(reusedTime, 3, initial.GetSnapshotID(),
		availableConfigObservation(affectedKey, reusedTime),
		availableConfigObservation(admissionOnlyKey, reusedTime),
		availableConfigObservation(unrelatedKey, reusedTime),
	)
	if _, err := store.Report(ctx, nodekernel.ReportParams{
		NodeID: nodeID, NodeTarget: "127.0.0.1:1", Runtimes: []string{"runsc"},
		NodeAuthToken: authToken, Now: reusedTime, Summary: capabilityReportSummary(reused),
	}); err == nil {
		t.Fatal("reused capability snapshot identity was accepted")
	}
	var snapshotAfterReuse string
	if err := db.Pool().QueryRow(ctx, `SELECT summary->'capabilitySnapshot'->>'snapshotId' FROM node_summaries WHERE node_id = $1`, nodeID).Scan(&snapshotAfterReuse); err != nil {
		t.Fatal(err)
	}
	if snapshotAfterReuse != next.GetSnapshotID() {
		t.Fatalf("reused snapshot changed node summary to %q, want %q", snapshotAfterReuse, next.GetSnapshotID())
	}

	if _, err := db.Pool().Exec(ctx, `DELETE FROM allocation_capability_reconcile_queue WHERE allocation_id = $1`, affectedID); err != nil {
		t.Fatal(err)
	}
	functionName := "fail_capability_enqueue_" + strings.ReplaceAll(suffix, "-", "_")
	triggerName := functionName + "_trigger"
	if _, err := db.Pool().Exec(ctx, `CREATE FUNCTION `+functionName+`() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RAISE EXCEPTION ''forced queue failure''; END'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `CREATE TRIGGER `+triggerName+` BEFORE INSERT ON allocation_capability_reconcile_pending_keys FOR EACH ROW EXECUTE FUNCTION `+functionName+`()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DROP TRIGGER IF EXISTS `+triggerName+` ON allocation_capability_reconcile_pending_keys`)
		_, _ = db.Pool().Exec(context.Background(), `DROP FUNCTION IF EXISTS `+functionName+`()`)
	})

	failingTime := reusedTime.Add(time.Second)
	failing := capabilityReportSnapshot(failingTime, 3, "snapshot-recovery-"+suffix,
		availableConfigObservation(affectedKey, failingTime),
		availableConfigObservation(admissionOnlyKey, failingTime),
		availableConfigObservation(unrelatedKey, failingTime),
	)
	if _, err := store.Report(ctx, nodekernel.ReportParams{
		NodeID: nodeID, NodeTarget: "127.0.0.1:1", Runtimes: []string{"runsc"},
		NodeAuthToken: authToken, Now: failingTime, Summary: capabilityReportSummary(failing),
	}); err == nil {
		t.Fatal("forced queue failure did not abort node report")
	}
	var storedSnapshotID string
	if err := db.Pool().QueryRow(ctx, `SELECT summary->'capabilitySnapshot'->>'snapshotId' FROM node_summaries WHERE node_id = $1`, nodeID).Scan(&storedSnapshotID); err != nil {
		t.Fatal(err)
	}
	if storedSnapshotID != next.GetSnapshotID() {
		t.Fatalf("failed report changed node summary to %q, want %q", storedSnapshotID, next.GetSnapshotID())
	}
	var failedTransitions, failedQueueRows int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM node_capability_transitions WHERE node_id = $1 AND snapshot_id = $2`, nodeID, failing.GetSnapshotID()).Scan(&failedTransitions); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM allocation_capability_reconcile_queue WHERE allocation_id = $1`, affectedID).Scan(&failedQueueRows); err != nil {
		t.Fatal(err)
	}
	if failedTransitions != 0 || failedQueueRows != 0 {
		t.Fatalf("failed report escaped transaction: transitions=%d queue_rows=%d", failedTransitions, failedQueueRows)
	}
}

func capabilityReportSummary(snapshot *capabilityv1.CapabilitySnapshot) *nodev1.NodeSummary {
	return &nodev1.NodeSummary{CollectedAt: proto.Clone(snapshot.GetCollectedAt()).(*timestamppb.Timestamp), CapabilitySnapshot: snapshot}
}

func capabilityReportSnapshot(observedAt time.Time, sequence int64, snapshotID string, observations ...*capabilityv1.CapabilityObservation) *capabilityv1.CapabilitySnapshot {
	return &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "postgres-capability-report-instance", Sequence: sequence, SnapshotID: snapshotID,
		CollectedAt: timestamppb.New(observedAt), Observations: observations,
	}
}

func availableConfigObservation(key *capabilityv1.CapabilityKey, observedAt time.Time) *capabilityv1.CapabilityObservation {
	provider, _, err := capabilitycontract.ObservationOwner(key)
	if err != nil {
		panic(err)
	}
	observation := &capabilityv1.CapabilityObservation{
		Key: capabilitycontract.CloneKey(key), State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider: provider, ObservedAt: timestamppb.New(observedAt),
		Evidence:   capabilitycontract.ConfigEvidence("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	if key.GetExtension() == nil {
		definition, _ := capabilitycontract.PlatformDefinition(key.GetPlatform())
		if definition.Freshness.MaxValidity > 0 {
			observation.ValidUntil = timestamppb.New(observedAt.Add(definition.Freshness.MaxValidity))
		}
	}
	capabilitycontract.NormalizeObservation(observation)
	return observation
}

func unavailableConfigObservation(key *capabilityv1.CapabilityKey, observedAt time.Time) *capabilityv1.CapabilityObservation {
	observation := availableConfigObservation(key, observedAt)
	observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	observation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	observation.Reason = "forced loss"
	capabilitycontract.NormalizeObservation(observation)
	return observation
}
