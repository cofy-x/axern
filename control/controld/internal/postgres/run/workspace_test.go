package pgrun

import (
	"context"
	"crypto/sha256"
	"errors"
	pgaccess "github.com/cofy-x/axern/control/controld/internal/postgres/access"
	"sync"
	"testing"
	"time"

	accesskernel "github.com/cofy-x/axern/control/controld/internal/kernel/access"
	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	pgnamespace "github.com/cofy-x/axern/control/controld/internal/postgres/namespace"
	pgworkspace "github.com/cofy-x/axern/control/controld/internal/postgres/workspace"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type workspaceFixture struct {
	db         *postgres.DB
	ctx        context.Context
	now        time.Time
	store      *Store
	workspaces *pgworkspace.Store
	workspace  *workspacev1.Workspace
	params     runkernel.AdmitRunParams
}

func newWorkspaceFixture(t *testing.T) *workspaceFixture {
	t.Helper()
	db := newEnvironmentTestDB(t)
	now := time.Now().UTC()
	store := NewStore(db, WithPlacementEvaluator(placement.NewEngine(placement.Config{})))
	t.Cleanup(store.Close)
	env, err := store.CreateEnvironment(context.Background(), runkernel.CreateEnvironmentParams{Spec: &environmentv1.EnvironmentSpec{Namespace: "default", Image: &environmentv1.EnvironmentImageSource{Ref: "docker.io/library/nginx:1.27"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := sha256.Sum256([]byte("workspace-control-fixture"))
	t.Cleanup(func() {
		_, _ = db.Pool().Exec(context.Background(), `DELETE FROM admin_audit_events WHERE actor_principal_id='workspace-principal';DELETE FROM role_bindings WHERE principal_id='workspace-principal';DELETE FROM principal_credentials WHERE credential_id='workspace-credential';DELETE FROM principals WHERE principal_id='workspace-principal'`)
	})
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO principals(principal_id,name,display_name,kind,status,created_at,updated_at) VALUES('workspace-principal','workspace-principal','Workspace tests','service','active',$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO principal_credentials(credential_id,principal_id,kind,fingerprint,expires_at,label,created_at) VALUES('workspace-credential','workspace-principal','x509_sha256',$1,$2,'test',$3)`, fingerprint[:], now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(context.Background(), `INSERT INTO role_bindings(binding_id,principal_id,scope_type,namespace,role,created_at) VALUES('workspace-binding','workspace-principal','namespace','default','namespace_editor',$1)`, now); err != nil {
		t.Fatal(err)
	}
	ctx := accesskernel.WithActor(context.Background(), accesskernel.Actor{Principal: accesskernel.Principal{ID: "workspace-principal"}, Credential: accesskernel.Credential{Fingerprint: fingerprint}})
	summary := controldtest.ReadySummary(now)
	summary.WorkspaceStorage = &nodev1.WorkspaceStorage{Ready: true, CapacityBytes: 16 << 20, InodeCapacity: 1024, StorageIdentity: "root-owned-fixture"}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,last_heartbeat_at,lifecycle_status) VALUES('workspace-node','127.0.0.1:24010',repeat('0',64),$1,$1,'active')`, now); err != nil {
		t.Fatal(err)
	}
	data, err := protojson.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO node_summaries(node_id,summary) VALUES('workspace-node',$1::jsonb)`, data); err != nil {
		t.Fatal(err)
	}
	workspaces := pgworkspace.NewStore(db, time.Minute, time.Minute)
	w, err := workspaces.CreateWorkspace(ctx, &workspacev1.CreateWorkspaceRequest{Namespace: "default", NodeID: "workspace-node", CapacityBytes: 4 << 20, InodeLimit: 64}, now)
	if err != nil {
		t.Fatal(err)
	}
	item, err := workspaces.ClaimWorkspaceReconcile(ctx, "workspace-provisioner", now.Add(time.Second))
	if err != nil || item == nil {
		t.Fatalf("claim=%+v error=%v", item, err)
	}
	if err := workspaces.CompleteWorkspaceReconcile(ctx, item, "opaque-owned-fixture", 0, 0, nil, now); err != nil {
		t.Fatal(err)
	}
	w, err = workspaces.GetWorkspace(ctx, w.GetID())
	if err != nil {
		t.Fatal(err)
	}
	config := executionkernel.NormalizeConfig(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{CpuMilli: 100, MemoryBytes: 1 << 20, EphemeralStorageBytes: 1 << 20}, Limits: &commonv1.ResourceQuantity{MemoryBytes: 64 << 20}}, Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_HOST}})
	requests := config.GetResources().GetRequests()
	limits := config.GetResources().GetLimits()
	record := &nodekernel.Record{NodeID: "workspace-node", NodeTarget: "127.0.0.1:24010", Lifecycle: nodekernel.LifecycleActive, AdmittedAt: now, LastHeartbeatAt: now, Summary: summary}
	request := &placementkernel.Request{RootfsKey: env.GetID(), RootfsType: nodev1.RootfsType_ROOTFS_TYPE_IMAGE, MountType: nodev1.MountType_MOUNT_TYPE_OCI, RootfsWritable: true, MemoryLimitBytes: limits.GetMemoryBytes(), EphemeralStorageLimitBytes: limits.GetEphemeralStorageBytes(), RequestedCpuMilli: requests.GetCpuMilli(), RequestedMemoryBytes: requests.GetMemoryBytes(), RequestedEphemeralStorageBytes: requests.GetEphemeralStorageBytes(), Network: "host"}
	engine := placement.NewEngine(placement.Config{})
	evaluation := engine.Evaluate(record, request, now)
	if evaluation.GetState() != placementkernel.CandidateStateEligible {
		t.Fatalf("fixture candidate is unavailable: %+v", evaluation.GetRejectionReasons())
	}
	return &workspaceFixture{db: db, ctx: ctx, now: now, store: store, workspaces: workspaces, workspace: w, params: runkernel.AdmitRunParams{Namespace: "default", Environment: env, Config: config, WorkspaceID: w.GetID(), Candidates: []*placementkernel.Candidate{{Record: record, Evaluation: evaluation, BaseRequest: request, Request: request}}}}
}

func TestWorkspaceConcurrentAdmissionsAcquireExactlyOneWriter(t *testing.T) {
	f := newWorkspaceFixture(t)
	start := make(chan struct{})
	results := make(chan error, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := f.store.AdmitRun(f.ctx, f.params, f.now); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	allowed := 0
	for err := range results {
		if err == nil {
			allowed++
		} else if grpcstatus.Code(err) != codes.FailedPrecondition {
			t.Errorf("unexpected admission error: %v", err)
		}
	}
	if allowed != 1 {
		t.Fatalf("writers admitted=%d", allowed)
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetGeneration() != 1 || w.GetAttachedAllocationID() == "" {
		t.Fatalf("writer=%+v", w)
	}
	var runs, allocations, intents int
	if err := f.db.Pool().QueryRow(f.ctx, `SELECT (SELECT COUNT(*) FROM runs),(SELECT COUNT(*) FROM allocations),(SELECT COUNT(*) FROM allocation_reconcile_queue)`).Scan(&runs, &allocations, &intents); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || allocations != 1 || intents != 1 {
		t.Fatalf("atomic admission rows=%d/%d/%d", runs, allocations, intents)
	}
}

func TestWorkspaceWriterReleaseRequiresExactCleanupAndAdvancesGeneration(t *testing.T) {
	f := newWorkspaceFixture(t)
	run, err := f.store.AdmitRun(f.ctx, f.params, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.workspaces.DeleteWorkspace(f.ctx, f.workspace.GetID(), f.now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("delete active writer=%v", err)
	}
	if _, err := f.store.CancelRun(f.ctx, run.GetID(), f.now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ClaimDueReconcileItems(f.ctx, "release-worker", 10, f.now, 30*time.Second)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v error=%v", items, err)
	}
	item := items[0]
	for _, receipt := range []*nodelifecyclev1.WorkspaceCleanupReceipt{nil, {WorkspaceID: f.workspace.GetID(), AllocationID: run.GetAllocationID(), Generation: 2, StorageIdentity: "opaque-owned-fixture"}, {WorkspaceID: f.workspace.GetID(), AllocationID: "another-allocation", Generation: 1, StorageIdentity: "opaque-owned-fixture"}, {WorkspaceID: f.workspace.GetID(), AllocationID: run.GetAllocationID(), Generation: 1, StorageIdentity: "another-directory"}} {
		err := f.store.CompleteAllocationRelease(f.ctx, run.GetAllocationID(), item.ClaimOwner, &allocationkernel.ReleaseResult{WorkspaceCleanup: receipt}, f.now)
		if grpcstatus.Code(err) != codes.FailedPrecondition {
			t.Fatalf("invalid cleanup receipt accepted: %+v error=%v", receipt, err)
		}
	}
	receipt := &nodelifecyclev1.WorkspaceCleanupReceipt{WorkspaceID: f.workspace.GetID(), AllocationID: run.GetAllocationID(), Generation: 1, StorageIdentity: "opaque-owned-fixture"}
	if err := f.store.CompleteAllocationRelease(f.ctx, run.GetAllocationID(), item.ClaimOwner, &allocationkernel.ReleaseResult{WorkspaceCleanup: receipt}, f.now); err != nil {
		t.Fatal(err)
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetAttachedAllocationID() != "" || w.GetGeneration() != 1 {
		t.Fatalf("released writer=%+v", w)
	}
	second, err := f.store.AdmitRun(f.ctx, f.params, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if second.GetAllocationID() == run.GetAllocationID() || second.GetWorkspaceAttachment().GetGeneration() != 2 {
		t.Fatalf("new execution did not get new writer: %+v", second)
	}
	if err := f.store.CompleteAllocationRelease(f.ctx, run.GetAllocationID(), item.ClaimOwner, &allocationkernel.ReleaseResult{WorkspaceCleanup: receipt}, f.now); !errors.Is(err, allocationkernel.ErrReconcileClaimLost) {
		t.Fatalf("stale cleanup claim=%v", err)
	}
	w, err = f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetGeneration() != 2 || w.GetAttachedAllocationID() != second.GetAllocationID() {
		t.Fatalf("late cleanup changed current writer: %+v", w)
	}
}

func TestWorkspaceAdmissionRollsBackWriterWhenIntentFails(t *testing.T) {
	f := newWorkspaceFixture(t)
	if _, err := f.db.Pool().Exec(f.ctx, `CREATE FUNCTION fail_workspace_intent() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'intent fixture failure'; END$$;CREATE TRIGGER fail_workspace_intent BEFORE INSERT ON allocation_reconcile_queue FOR EACH ROW EXECUTE FUNCTION fail_workspace_intent()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.db.Pool().Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_workspace_intent ON allocation_reconcile_queue;DROP FUNCTION IF EXISTS fail_workspace_intent()`)
	})
	if _, err := f.store.AdmitRun(f.ctx, f.params, f.now); err == nil {
		t.Fatal("admission succeeded without durable intent")
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetGeneration() != 0 || w.GetAttachedAllocationID() != "" {
		t.Fatalf("failed transaction acquired writer: %+v", w)
	}
	var runs int
	if err := f.db.Pool().QueryRow(f.ctx, `SELECT COUNT(*) FROM runs`).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("partial Run persisted count=%d err=%v", runs, err)
	}
}

func TestWorkspaceRetainsChargeThroughUnavailableAndFailedDeletion(t *testing.T) {
	f := newWorkspaceFixture(t)
	quotas := pgnamespace.NewStore(f.db)
	if _, err := quotas.Set(f.ctx, "default", &quotav1.NamespaceQuotaLimits{PersistentStorageBytes: wrapperspb.Int64(4 << 20), PersistentStorageInodes: wrapperspb.Int64(64)}, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workspaces.CreateWorkspace(f.ctx, &workspacev1.CreateWorkspaceRequest{Namespace: "default", NodeID: "workspace-node", CapacityBytes: 512, InodeLimit: 1}, f.now); grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("detached capacity not charged: %v", err)
	}
	item, err := f.workspaces.ClaimWorkspaceReconcile(f.ctx, "inspector", f.now.Add(20*time.Second))
	if err != nil || item == nil {
		t.Fatalf("claim inspect=%+v err=%v", item, err)
	}
	if err := f.workspaces.CompleteWorkspaceReconcile(f.ctx, item, "opaque-owned-fixture", 0, 0, errors.New("private filesystem path must not be exposed"), f.now.Add(20*time.Second)); err != nil {
		t.Fatal(err)
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetStatus() != workspacev1.WorkspaceStatus_WORKSPACE_STATUS_UNAVAILABLE || w.GetMessage() != "managed storage operation failed" {
		t.Fatalf("unavailable projection=%+v", w)
	}
	if _, err := f.workspaces.DeleteWorkspace(f.ctx, f.workspace.GetID(), f.now.Add(21*time.Second)); err != nil {
		t.Fatal(err)
	}
	item, err = f.workspaces.ClaimWorkspaceReconcile(f.ctx, "deleter", f.now.Add(22*time.Second))
	if err != nil || item == nil {
		t.Fatalf("claim delete=%+v err=%v", item, err)
	}
	if err := f.workspaces.CompleteWorkspaceReconcile(f.ctx, item, "opaque-owned-fixture", 0, 0, errors.New("disk unavailable"), f.now.Add(21*time.Second)); err != nil {
		t.Fatal(err)
	}
	quota, err := quotas.Get(f.ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if quota.GetUsedPersistentStorageBytes() != 4<<20 || quota.GetUsedPersistentStorageInodes() != 64 {
		t.Fatalf("deletion failure released retained charge: %+v", quota)
	}
	item, err = f.workspaces.ClaimWorkspaceReconcile(f.ctx, "deleter", f.now.Add(30*time.Second))
	if err != nil || item == nil {
		t.Fatalf("claim delete retry=%+v err=%v", item, err)
	}
	if err := f.workspaces.CompleteWorkspaceReconcile(f.ctx, item, "opaque-owned-fixture", 0, 0, nil, f.now.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	quota, err = quotas.Get(f.ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if quota.GetUsedPersistentStorageBytes() != 0 || quota.GetUsedPersistentStorageInodes() != 0 {
		t.Fatalf("confirmed deletion still charged: %+v", quota)
	}
}

func TestWorkspaceAdmissionRechecksRevokedCredentialAfterNodeLockWait(t *testing.T) {
	f := newWorkspaceFixture(t)
	lock, err := f.db.Pool().Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	if _, err := lock.Exec(f.ctx, `SELECT node_id FROM nodes WHERE node_id='workspace-node' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := f.store.AdmitRun(f.ctx, f.params, f.now); result <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-result:
			t.Fatalf("admission returned before lock wait: %v", err)
		default:
		}
		var waiting bool
		if err := f.db.Pool().QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("admission never waited on Node lock")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := f.db.Pool().Exec(f.ctx, `UPDATE principal_credentials SET revoked_at=$1 WHERE credential_id='workspace-credential'`, f.now); err != nil {
		t.Fatal(err)
	}
	if err := lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if grpcstatus.Code(err) != codes.Unauthenticated {
			t.Fatalf("revoked credential admitted: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission did not finish")
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetGeneration() != 0 || w.GetAttachedAllocationID() != "" {
		t.Fatal("revoked authority acquired a writer")
	}
}

func TestRetainedWorkspaceBlocksNamespaceDeletionAndNodeRetirement(t *testing.T) {
	f := newWorkspaceFixture(t)
	if _, err := pgnamespace.NewStore(f.db).DeleteNamespace(f.ctx, "default", f.now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Namespace deletion ignored retained data: %v", err)
	}
	if _, err := pgadmin.NewStore(f.db).RetireNode(f.ctx, adminkernel.RetireNodeRequest{NodeID: "workspace-node", OperatorReason: "test retained storage blocker", Now: f.now.Add(time.Hour), HeartbeatWindow: time.Second}); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Node retirement ignored retained data: %v", err)
	}
}

func TestWorkspaceCrossNamespaceAttachmentIsRejectedBeforeRunCreation(t *testing.T) {
	f := newWorkspaceFixture(t)
	if _, err := pgnamespace.NewStore(f.db).CreateNamespace(f.ctx, "other", f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(f.ctx, `UPDATE workspaces SET namespace='other' WHERE workspace_id=$1`, f.workspace.GetID()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdmitRun(f.ctx, f.params, f.now); grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("cross-namespace attachment=%v", err)
	}
	var runs int
	if err := f.db.Pool().QueryRow(f.ctx, `SELECT COUNT(*) FROM runs`).Scan(&runs); err != nil || runs != 0 {
		t.Fatalf("unauthorized Run persisted count=%d err=%v", runs, err)
	}
}

func TestWorkspaceAuthorizationAndRoleGrantUseNamespaceFirst(t *testing.T) {
	f := newWorkspaceFixture(t)
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `SELECT namespace FROM namespaces WHERE namespace='default' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := pgaccess.NewStore(f.db).GrantBinding(ctx, "workspace-principal", "workspace-principal", accesskernel.ScopeNamespace, "default", accesskernel.RoleNamespaceViewer, f.now)
		result <- err
	}()
	for {
		var waiting bool
		if err := f.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := pgaccess.AuthorizeTransaction(ctx, tx, accesskernel.ActionResourceWrite, "default", f.now); err != nil {
		t.Fatalf("grant holdingPrincipal beforeNamespace deadlocked admission: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceRetainedNamespaceAccountingCannotOverflow(t *testing.T) {
	f := newWorkspaceFixture(t)
	summary := controldtest.ReadySummary(f.now)
	summary.WorkspaceStorage = &nodev1.WorkspaceStorage{Ready: true, CapacityBytes: 9_000_000_000_000_000_000, InodeCapacity: 1024, StorageIdentity: "root-owned-fixture"}
	data, err := protojson.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(f.ctx, `UPDATE node_summaries SET summary=$1::jsonb WHERE node_id='workspace-node'`, data); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(f.ctx, `INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,last_heartbeat_at,lifecycle_status) VALUES('workspace-node-two','127.0.0.1:24011',repeat('0',64),$1,$1,'active')`, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(f.ctx, `INSERT INTO node_summaries(node_id,summary) VALUES('workspace-node-two',$1::jsonb)`, data); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workspaces.CreateWorkspace(f.ctx, &workspacev1.CreateWorkspaceRequest{Namespace: "default", NodeID: "workspace-node", CapacityBytes: 6_000_000_000_000_000_000, InodeLimit: 1}, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workspaces.CreateWorkspace(f.ctx, &workspacev1.CreateWorkspaceRequest{Namespace: "default", NodeID: "workspace-node-two", CapacityBytes: 6_000_000_000_000_000_000, InodeLimit: 1}, f.now); grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("overflowing charge accepted: %v", err)
	}
	quota, err := pgnamespace.NewStore(f.db).Get(f.ctx, "default")
	if err != nil {
		t.Fatalf("retained quota became unreadable: %v", err)
	}
	if quota.GetUsedPersistentStorageBytes() != 6_000_000_000_000_000_000+(4<<20) {
		t.Fatalf("retained aggregate=%d", quota.GetUsedPersistentStorageBytes())
	}
}

func TestWorkspaceOwnershipWaitCannotReuseExpiredNodeObservation(t *testing.T) {
	f := newWorkspaceFixture(t)
	summary := f.params.Candidates[0].Record.Summary
	summary.CollectedAt = timestamppb.New(f.now.Add(-14900 * time.Millisecond))
	data, err := protojson.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(f.ctx, `UPDATE node_summaries SET summary=$1::jsonb WHERE node_id='workspace-node'`, data); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.Pool().Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(f.ctx, `SELECT workspace_id FROM workspaces WHERE workspace_id=$1 FOR UPDATE`, f.workspace.GetID()); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := f.store.AdmitRun(f.ctx, f.params, f.now); result <- err }()
	waitForBlockedWorkspaceAdmission(t, f, result)
	time.Sleep(200 * time.Millisecond)
	if err := lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expired Node observation admitted: %v", err)
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetGeneration() != 0 || w.GetAttachedAllocationID() != "" {
		t.Fatal("stale observation acquired writer")
	}
}

func TestWorkspaceOwnershipWaitRechecksCredentialExpiry(t *testing.T) {
	f := newWorkspaceFixture(t)
	if _, err := f.db.Pool().Exec(f.ctx, `UPDATE principal_credentials SET expires_at=$1 WHERE credential_id='workspace-credential'`, f.now.Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.Pool().Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(f.ctx, `SELECT workspace_id FROM workspaces WHERE workspace_id=$1 FOR UPDATE`, f.workspace.GetID()); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := f.store.AdmitRun(f.ctx, f.params, f.now); result <- err }()
	waitForBlockedWorkspaceAdmission(t, f, result)
	time.Sleep(200 * time.Millisecond)
	if err := lock.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; grpcstatus.Code(err) != codes.Unauthenticated {
		t.Fatalf("expired credential admitted: %v", err)
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil {
		t.Fatal(err)
	}
	if w.GetGeneration() != 0 || w.GetAttachedAllocationID() != "" {
		t.Fatal("expired credential acquired writer")
	}
}

func waitForBlockedWorkspaceAdmission(t *testing.T, f *workspaceFixture, result <-chan error) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case err := <-result:
			t.Fatalf("admission returned before Workspace lock: %v", err)
		default:
		}
		var waiting bool
		if err := f.db.Pool().QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM workspaces%' AND pid<>pg_backend_pid())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("admission did not reach Workspace lock")
		}
		time.Sleep(time.Millisecond)
	}
}
