package pgrun

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestCompleteAllocationReleasePublishesSnapshotAndSchedulesAcknowledgement(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	now := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	ctx := context.Background()

	configJSON, err := marshalProtoJSON(&commonv1.ExecutionConfig{RootfsSnapshot: &commonv1.RootfsSnapshot{}})
	if err != nil {
		t.Fatal(err)
	}
	specJSON, err := marshalProtoJSON(&environmentv1.EnvironmentSpec{
		Namespace: "default",
		Image:     &environmentv1.EnvironmentImageSource{Ref: "registry.example/base@sha256:base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolvedJSON, err := marshalProtoJSON(&environmentv1.ResolvedEnvironmentSpec{
		ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: "sha256:base", MediaType: "application/vnd.oci.image.manifest.v1+json", SizeBytes: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	pendingJSON, err := marshalProtoJSON(&runv1.RootfsSnapshotResult{Status: runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-snapshot-release', 'node-snapshot-release:24010', repeat('0', 64), $1, $1, 'active')
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, rootfs_snapshot_result, created_at, updated_at)
		VALUES ('run-snapshot-release', 'default', 'env-base', 'RUN_STATUS_SUCCEEDED', $2::jsonb, $3::jsonb, $4::jsonb, '{}'::jsonb, $5::jsonb, $1, $1)
	`, now, configJSON, specJSON, resolvedJSON, pendingJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ('alloc-snapshot-release', 'run-snapshot-release', 'node-snapshot-release', 'ALLOCATION_LIFECYCLE_STATE_RELEASING', 1, $1, $1)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO allocation_reconcile_queue (allocation_id, next_run_at, claim_owner, claim_expires_at, created_at, updated_at)
		VALUES ('alloc-snapshot-release', $1, 'worker-a', $2, $1, $1)
	`, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	result := &allocationkernel.RootfsSnapshotResult{
		ImageRef: "127.0.0.1:5001/axern/snapshots@sha256:derived",
		ImageDescriptor: &environmentv1.OciImageDescriptor{
			Digest: "sha256:derived", MediaType: "application/vnd.oci.image.manifest.v1+json", SizeBytes: 2,
		},
		PlatformOS: "linux", PlatformArch: "arm64",
	}
	if err := store.CompleteAllocationRelease(ctx, "alloc-snapshot-release", "worker-a", result, now.Add(time.Second)); err != nil {
		t.Fatalf("CompleteAllocationRelease() error = %v", err)
	}

	var lifecycleState, claimOwner string
	var claimExpiresAt *time.Time
	if err := db.Pool().QueryRow(ctx, `
		SELECT a.lifecycle_state, q.claim_owner, q.claim_expires_at
		FROM allocations a JOIN allocation_reconcile_queue q USING (allocation_id)
		WHERE a.allocation_id = 'alloc-snapshot-release'
	`).Scan(&lifecycleState, &claimOwner, &claimExpiresAt); err != nil {
		t.Fatal(err)
	}
	if lifecycleState != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String() || claimOwner != "" || claimExpiresAt != nil {
		t.Fatalf("release/ack intent = state %q owner %q expiry %v", lifecycleState, claimOwner, claimExpiresAt)
	}
	run, err := store.GetRun(ctx, "run-snapshot-release")
	if err != nil {
		t.Fatal(err)
	}
	if run.GetRootfsSnapshot().GetStatus() != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY || run.GetRootfsSnapshot().GetEnvironmentID() == "" {
		t.Fatalf("rootfs snapshot result = %#v", run.GetRootfsSnapshot())
	}
	if _, err := store.GetEnvironment(ctx, run.GetRootfsSnapshot().GetEnvironmentID()); err != nil {
		t.Fatalf("GetEnvironment(derived) error = %v", err)
	}
}

func TestSnapshotFinalResultCannotBeOverwritten(t *testing.T) {
	result := &allocationkernel.RootfsSnapshotResult{
		ImageRef: "127.0.0.1:5001/axern/snapshots@sha256:derived",
		ImageDescriptor: &environmentv1.OciImageDescriptor{
			Digest: "sha256:derived", MediaType: "application/vnd.oci.image.manifest.v1+json", SizeBytes: 2,
		},
		PlatformOS: "linux", PlatformArch: "arm64",
	}
	for _, finalStatus := range []runv1.RootfsSnapshotStatus{
		runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY,
		runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED,
	} {
		t.Run(finalStatus.String(), func(t *testing.T) {
			db := newEnvironmentTestDB(t)
			store := NewStore(db)
			now := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
			insertFinalSnapshotReleaseFixture(t, db.Pool(), now, finalStatus)

			err := store.CompleteAllocationRelease(context.Background(), "alloc-final-snapshot", "worker-a", result, now.Add(time.Second))
			if grpcstatus.Code(err) != codes.FailedPrecondition {
				t.Fatalf("CompleteAllocationRelease() error = %v, want FailedPrecondition", err)
			}
			err = store.MarkRootfsSnapshotFailed(context.Background(), "alloc-final-snapshot", "worker-a", "replacement", now.Add(time.Second))
			if grpcstatus.Code(err) != codes.FailedPrecondition {
				t.Fatalf("MarkRootfsSnapshotFailed() error = %v, want FailedPrecondition", err)
			}
			var status string
			var version int64
			if err := db.Pool().QueryRow(context.Background(), `SELECT rootfs_snapshot_result->>'status', version FROM runs WHERE run_id='run-final-snapshot'`).Scan(&status, &version); err != nil {
				t.Fatal(err)
			}
			if status != finalStatus.String() || version != 1 {
				t.Fatalf("final snapshot changed to status=%q version=%d", status, version)
			}
		})
	}
}

func TestSnapshotEnvironmentIdentityConflictRollsBackRelease(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	now := time.Date(2026, 9, 19, 9, 0, 0, 0, time.UTC)
	insertFinalSnapshotReleaseFixture(t, db.Pool(), now, runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING)
	environmentID := "env-" + uuid.NewSHA1(uuid.NameSpaceOID, []byte("alloc-final-snapshot")).String()
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO environments (environment_id, namespace, spec, resolved_spec, labels, created_at) VALUES ($1,'default','{}'::jsonb,'{}'::jsonb,'{}'::jsonb,$2)`, environmentID, now); err != nil {
		t.Fatal(err)
	}
	result := &allocationkernel.RootfsSnapshotResult{
		ImageRef: "127.0.0.1:5001/axern/snapshots@sha256:derived",
		ImageDescriptor: &environmentv1.OciImageDescriptor{
			Digest: "sha256:derived", MediaType: "application/vnd.oci.image.manifest.v1+json", SizeBytes: 2,
		},
		PlatformOS: "linux", PlatformArch: "arm64",
	}
	if err := store.CompleteAllocationRelease(context.Background(), "alloc-final-snapshot", "worker-a", result, now.Add(time.Second)); err == nil {
		t.Fatal("CompleteAllocationRelease() succeeded with conflicting derived Environment")
	}
	var lifecycle, snapshotStatus string
	if err := db.Pool().QueryRow(context.Background(), `SELECT a.lifecycle_state, r.rootfs_snapshot_result->>'status' FROM allocations a JOIN runs r USING (run_id) WHERE a.allocation_id='alloc-final-snapshot'`).Scan(&lifecycle, &snapshotStatus); err != nil {
		t.Fatal(err)
	}
	if lifecycle != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String() || snapshotStatus != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING.String() {
		t.Fatalf("release transaction partially committed: lifecycle=%q snapshot=%q", lifecycle, snapshotStatus)
	}
}

type snapshotFixtureExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertFinalSnapshotReleaseFixture(t *testing.T, db snapshotFixtureExecutor, now time.Time, status runv1.RootfsSnapshotStatus) {
	t.Helper()
	configJSON, err := marshalProtoJSON(&commonv1.ExecutionConfig{RootfsSnapshot: &commonv1.RootfsSnapshot{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotJSON, err := marshalProtoJSON(&runv1.RootfsSnapshotResult{Status: status})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status) VALUES ('node-final-snapshot', 'node-final-snapshot:24010', repeat('0',64), $1, $1, 'active')`, []any{now}},
		{`INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, rootfs_snapshot_result, version, created_at, updated_at) VALUES ('run-final-snapshot','default','env-base','RUN_STATUS_SUCCEEDED',$2::jsonb,'{}'::jsonb,'{}'::jsonb,'{}'::jsonb,$3::jsonb,1,$1,$1)`, []any{now, configJSON, snapshotJSON}},
		{`INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at) VALUES ('alloc-final-snapshot','run-final-snapshot','node-final-snapshot','ALLOCATION_LIFECYCLE_STATE_RELEASING',1,$1,$1)`, []any{now}},
		{`INSERT INTO allocation_reconcile_queue (allocation_id, next_run_at, claim_owner, claim_expires_at, created_at, updated_at) VALUES ('alloc-final-snapshot',$1,'worker-a',$2,$1,$1)`, []any{now, now.Add(time.Minute)}},
	}
	for _, statement := range statements {
		if _, err := db.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
}
