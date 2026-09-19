package pgrun

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestWatchRunTerminalSnapshotWaitsForReadyOrFailed(t *testing.T) {
	for _, finalStatus := range []runv1.RootfsSnapshotStatus{
		runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY,
		runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED,
	} {
		t.Run(finalStatus.String(), func(t *testing.T) {
			db := newEnvironmentTestDB(t)
			store := NewStore(db)
			defer store.Close()
			now := time.Now().UTC()
			insertTerminalSnapshotWatchRun(t, db, now, "run-terminal-snapshot", "alloc-terminal-snapshot", runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			type watchResult struct {
				run *runv1.Run
				err error
			}
			resultCh := make(chan watchResult, 1)
			go func() {
				run, err := store.WatchRun(ctx, "run-terminal-snapshot", 1)
				resultCh <- watchResult{run: run, err: err}
			}()
			waitForRunWatchSubscriber(t, store, 1)
			select {
			case result := <-resultCh:
				t.Fatalf("terminal Run with pending snapshot returned early: run=%#v err=%v", result.run, result.err)
			default:
			}

			finalJSON, err := marshalProtoJSON(&runv1.RootfsSnapshotResult{Status: finalStatus})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Pool().Exec(ctx, `UPDATE runs SET rootfs_snapshot_result=$2::jsonb, version=2, updated_at=$3 WHERE run_id=$1`, "run-terminal-snapshot", finalJSON, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			result := <-resultCh
			if result.err != nil {
				t.Fatal(result.err)
			}
			if result.run.GetVersion() != 2 || result.run.GetRootfsSnapshot().GetStatus() != finalStatus {
				t.Fatalf("WatchRun() = version %d snapshot %s", result.run.GetVersion(), result.run.GetRootfsSnapshot().GetStatus())
			}
			waitForRunWatchSubscriber(t, store, 0)
		})
	}
}

func TestWatchRunTerminalWithoutSnapshotCompletesImmediately(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	defer store.Close()
	now := time.Now().UTC()
	insertTerminalSnapshotWatchRun(t, db, now, "run-terminal-no-snapshot", "alloc-terminal-no-snapshot", runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_UNSPECIFIED)
	run, err := store.WatchRun(context.Background(), "run-terminal-no-snapshot", 1)
	if err != nil {
		t.Fatal(err)
	}
	if run.GetVersion() != 1 || run.GetRootfsSnapshot() != nil {
		t.Fatalf("WatchRun() = %#v", run)
	}
}

func TestWatchRunPendingSnapshotCancellationReleasesSubscription(t *testing.T) {
	db := newEnvironmentTestDB(t)
	store := NewStore(db)
	defer store.Close()
	now := time.Now().UTC()
	insertTerminalSnapshotWatchRun(t, db, now, "run-terminal-cancel-watch", "alloc-terminal-cancel-watch", runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_PENDING)
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan error, 1)
	go func() {
		_, err := store.WatchRun(ctx, "run-terminal-cancel-watch", 1)
		resultCh <- err
	}()
	waitForRunWatchSubscriber(t, store, 1)
	cancel()
	if err := <-resultCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("WatchRun() error = %v, want context canceled", err)
	}
	waitForRunWatchSubscriber(t, store, 0)
}

func insertTerminalSnapshotWatchRun(t *testing.T, db *postgres.DB, now time.Time, runID, allocationID string, snapshotStatus runv1.RootfsSnapshotStatus) {
	t.Helper()
	config := &commonv1.ExecutionConfig{}
	snapshot := &runv1.RootfsSnapshotResult{}
	if snapshotStatus != runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_UNSPECIFIED {
		config.RootfsSnapshot = &commonv1.RootfsSnapshot{}
		snapshot.Status = snapshotStatus
	}
	configJSON, err := marshalProtoJSON(config)
	if err != nil {
		t.Fatal(err)
	}
	snapshotJSON, err := marshalProtoJSON(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO nodes (node_id, node_target, enrollment_token_hash, admitted_at, last_heartbeat_at, lifecycle_status)
		VALUES ('node-terminal-watch', 'node-terminal-watch:24010', repeat('0', 64), $1, $1, 'active')
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (run_id, namespace, environment_id, status, config, environment_spec, resolved_environment_spec, labels, rootfs_snapshot_result, version, created_at, updated_at)
		VALUES ($1, 'default', 'env-watch', 'RUN_STATUS_SUCCEEDED', $2::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $3::jsonb, 1, $4, $4)
	`, runID, configJSON, snapshotJSON, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO allocations (allocation_id, run_id, node_id, lifecycle_state, cpu_request_milli, created_at, updated_at)
		VALUES ($1, $2, 'node-terminal-watch', 'ALLOCATION_LIFECYCLE_STATE_RELEASED', 1, $3, $3)
	`, allocationID, runID, now); err != nil {
		t.Fatal(err)
	}
}

func waitForRunWatchSubscriber(t *testing.T, store *Store, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		store.runWatches.mu.Lock()
		got := len(store.runWatches.subscribers)
		store.runWatches.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("run watch subscriber count = %d, want %d", got, want)
		}
		runtime.Gosched()
	}
}
