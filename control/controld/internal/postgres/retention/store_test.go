package pgretention

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
)

func TestCleanupSkipsWhileAnotherControllerHoldsAdvisoryLock(t *testing.T) {
	db := newRetentionTestDB(t)
	ctx := context.Background()
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	store := NewPGStore(db)
	if locked, err := store.tryAdvisoryLock(ctx, tx); err != nil || !locked {
		t.Fatalf("hold retention lock = %v, %v", locked, err)
	}
	result, err := store.Cleanup(ctx, retentionkernel.DefaultConfig(), time.Now())
	if err != nil || !result.Skipped || result.TotalDeleted() != 0 {
		t.Fatalf("concurrent cleanup = %+v, %v", result, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	result, err = store.Cleanup(ctx, retentionkernel.DefaultConfig(), time.Now())
	if err != nil || result.Skipped {
		t.Fatalf("cleanup after lock release = %+v, %v", result, err)
	}
}

func TestCleanupPreservesLiveWorkAndAccessBeforeDeletingTerminalHistory(t *testing.T) {
	db := newRetentionTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	if _, err := db.Pool().Exec(ctx, `INSERT INTO namespaces(namespace,created_at) VALUES('retention',$1)`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,lifecycle_status) VALUES('retention-node','',$1,$2,'active')`, strings.Repeat("0", 64), old); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO principals(principal_id,name,display_name,kind,status,created_at,updated_at) VALUES('retention-owner','retention-owner','Retention owner','human','active',$1,$1) ON CONFLICT (principal_id) DO NOTHING`, old); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name           string
		runStatus      string
		lifecycle      string
		recentRun      bool
		retainedOutput bool
		queued         bool
		grant          string
		tunnel         string
		wantDeleted    bool
	}{
		{name: "eligible", wantDeleted: true},
		{name: "active run", runStatus: "RUN_STATUS_RUNNING", lifecycle: "ALLOCATION_LIFECYCLE_STATE_ACTIVE"},
		{name: "unreleased allocation", lifecycle: "ALLOCATION_LIFECYCLE_STATE_RELEASING"},
		{name: "recent run", recentRun: true},
		{name: "retained outputs", retainedOutput: true},
		{name: "pending cleanup delivery", queued: true},
		{name: "live access grant", grant: "active"},
		{name: "revoked access grant", grant: "revoked", wantDeleted: true},
		{name: "expired access grant", grant: "expired", wantDeleted: true},
		{name: "pending tunnel", tunnel: "TUNNEL_SESSION_STATUS_PENDING"},
		{name: "running tunnel", tunnel: "TUNNEL_SESSION_STATUS_RUNNING"},
		{name: "degraded tunnel", tunnel: "TUNNEL_SESSION_STATUS_DEGRADED"},
		{name: "revoked tunnel", tunnel: "TUNNEL_SESSION_STATUS_REVOKED", wantDeleted: true},
		{name: "expired tunnel", tunnel: "TUNNEL_SESSION_STATUS_EXPIRED", wantDeleted: true},
		{name: "failed tunnel", tunnel: "TUNNEL_SESSION_STATUS_FAILED", wantDeleted: true},
	}
	var wantDeleted int64
	for _, tc := range cases {
		status := tc.runStatus
		if status == "" {
			status = "RUN_STATUS_SUCCEEDED"
		}
		lifecycle := tc.lifecycle
		if lifecycle == "" {
			lifecycle = "ALLOCATION_LIFECYCLE_STATE_RELEASED"
		}
		runUpdated, allocationUpdated := old, old
		if tc.recentRun {
			runUpdated = now
		}
		if tc.retainedOutput {
			allocationUpdated = now
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO runs(run_id,namespace,environment_id,status,config,environment_spec,resolved_environment_spec,labels,created_at,updated_at) VALUES($1,'retention','retention-environment',$2,'{}','{}','{}','{}',$3,$4)`, tc.name, status, old, runUpdated); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool().Exec(ctx, `INSERT INTO allocations(allocation_id,run_id,node_id,lifecycle_state,cpu_request_milli,created_at,updated_at) VALUES($1,$1,'retention-node',$2,1,$3,$4)`, tc.name, lifecycle, old, allocationUpdated); err != nil {
			t.Fatal(err)
		}
		if tc.retainedOutput {
			var outputRetained bool
			if err := db.Pool().QueryRow(ctx, `SELECT output_expires_at > $2 FROM allocations WHERE allocation_id=$1`, tc.name, now).Scan(&outputRetained); err != nil {
				t.Fatal(err)
			}
			if !outputRetained {
				t.Fatal("allocation cleanup trigger did not freeze a future output expiry")
			}
		}
		if tc.queued {
			if _, err := db.Pool().Exec(ctx, `INSERT INTO allocation_reconcile_queue(allocation_id,next_run_at,created_at,updated_at) VALUES($1,$2,$2,$2)`, tc.name, old); err != nil {
				t.Fatal(err)
			}
		}
		if tc.grant != "" {
			expires := now.Add(time.Hour)
			if tc.grant == "expired" {
				expires = now.Add(-time.Hour)
			}
			if _, err := db.Pool().Exec(ctx, `INSERT INTO allocation_access_grants(purpose,grant_id,allocation_id,node_id,expires_at,revision,revoked,token_hash,created_at) VALUES('ALLOCATION_ACCESS_PURPOSE_INTERACTIVE',$1,$1,'retention-node',$2,1,$3,'retention-token',$4)`, tc.name, expires, tc.grant == "revoked", old); err != nil {
				t.Fatal(err)
			}
		}
		if tc.tunnel != "" {
			if _, err := db.Pool().Exec(ctx, `INSERT INTO tunnel_sessions(session_id,allocation_id,creator_principal_id,remote_port,status,client_token_hash,node_token_encrypted,node_token_hash,revision,created_at,updated_at,expires_at) VALUES($1,$1,'retention-owner',8080,$2,'client-token','encrypted-node-token','node-token',1,$3,$3,$4)`, tc.name, tc.tunnel, old, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		if tc.wantDeleted {
			wantDeleted++
		}
	}
	cfg := retentionkernel.DefaultConfig()
	cfg.TerminalRunsTTL = time.Hour
	result, err := NewPGStore(db).Cleanup(ctx, cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.TerminalRunsDeleted != wantDeleted {
		t.Fatalf("deleted Runs = %d, want %d", result.TerminalRunsDeleted, wantDeleted)
	}
	for _, tc := range cases {
		var exists bool
		if err := db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE run_id=$1)`, tc.name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists == tc.wantDeleted {
			t.Errorf("%s: retained = %v, want %v", tc.name, exists, !tc.wantDeleted)
		}
	}
}

func newRetentionTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	controldtest.ResetPostgresControlTables(t, dsn)
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}
