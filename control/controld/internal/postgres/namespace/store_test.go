package namespace

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestStoreCreatesGetsAndListsNamespaces(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	created, err := store.CreateNamespace(ctx, "team-a", now)
	if err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	if created.GetNamespace() != "team-a" {
		t.Fatalf("namespace = %q, want team-a", created.GetNamespace())
	}
	if created.GetVersion() != 1 {
		t.Fatalf("version = %d, want 1", created.GetVersion())
	}

	got, err := store.GetNamespace(ctx, "team-a")
	if err != nil {
		t.Fatalf("GetNamespace() error = %v", err)
	}
	if got.GetNamespace() != "team-a" {
		t.Fatalf("got namespace = %q, want team-a", got.GetNamespace())
	}
	list, err := store.ListNamespaces(ctx)
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if len(list) != 1 || list[0].GetNamespace() != "team-a" {
		t.Fatalf("namespaces = %v, want team-a", list)
	}

	quota, err := store.Get(ctx, "team-a")
	if err != nil {
		t.Fatalf("Get quota error = %v", err)
	}
	if quota.GetNamespace() != "team-a" {
		t.Fatalf("quota namespace = %q, want team-a", quota.GetNamespace())
	}
}

func TestStoreGetNamespaceNotFound(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)

	_, err := store.GetNamespace(context.Background(), "missing")
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound err=%v", grpcstatus.Code(err), err)
	}
}

func TestStoreDeletesNamespaceWhenInactive(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	if _, err := store.Set(ctx, "team-a", &quotav1.NamespaceQuotaLimits{
		CpuMilli: wrapperspb.Int64(1000),
	}, now); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	deleted, err := store.DeleteNamespace(ctx, "team-a", now)
	if err != nil {
		t.Fatalf("DeleteNamespace() error = %v", err)
	}
	if deleted.GetNamespace() != "team-a" {
		t.Fatalf("deleted namespace = %q, want team-a", deleted.GetNamespace())
	}
	if _, err := store.GetNamespace(ctx, "team-a"); grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("GetNamespace() code = %v, want NotFound err=%v", grpcstatus.Code(err), err)
	}
	if _, err := store.Get(ctx, "team-a"); grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("Get quota code = %v, want NotFound err=%v", grpcstatus.Code(err), err)
	}
}

func TestStoreDeleteNamespaceRejectsActiveReservation(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	if _, err := store.CreateNamespace(ctx, "team-a", now); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	insertReservation(t, db, "active-a", "alloc-active-a", "team-a", 100, 128<<20, false, now)
	if _, err := store.DeleteNamespace(ctx, "team-a", now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteNamespace() code = %v, want FailedPrecondition err=%v", grpcstatus.Code(err), err)
	}
}

func TestStoreDeleteNamespaceRejectsActiveRoleBinding(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	if _, err := store.CreateNamespace(ctx, "team-a", now); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO principals(principal_id,name,display_name,kind,status,version,created_at,updated_at) VALUES('prn-viewer','viewer','Viewer','human','active',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool().Exec(ctx, `INSERT INTO role_bindings(binding_id,principal_id,scope_type,namespace,role,created_at) VALUES('rb-viewer','prn-viewer','namespace','team-a','namespace_viewer',$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteNamespace(ctx, "team-a", now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteNamespace() code = %v, want FailedPrecondition err=%v", grpcstatus.Code(err), err)
	}
	if _, err := db.Pool().Exec(ctx, `UPDATE role_bindings SET revoked_by_principal_id='prn-viewer', revoked_at=$1 WHERE binding_id='rb-viewer'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteNamespace(ctx, "team-a", now); err != nil {
		t.Fatalf("DeleteNamespace() after revoke error = %v", err)
	}
	var historyCount int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM role_bindings WHERE binding_id='rb-viewer'`).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if historyCount != 1 {
		t.Fatalf("revoked binding history count = %d, want 1", historyCount)
	}
}

func TestStoreDeleteNamespaceRejectsLiveEnvironment(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	if _, err := store.CreateNamespace(ctx, "team-a", now); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	insertEnvironment(t, db, "env-a", "team-a", environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_READY, now)
	if _, err := store.DeleteNamespace(ctx, "team-a", now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DeleteNamespace() code = %v, want FailedPrecondition err=%v", grpcstatus.Code(err), err)
	}
}

func TestStoreDeleteNamespaceAllowsDeletedEnvironment(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	if _, err := store.CreateNamespace(ctx, "team-a", now); err != nil {
		t.Fatalf("CreateNamespace() error = %v", err)
	}
	insertEnvironment(t, db, "env-a", "team-a", environmentv1.EnvironmentStatus_ENVIRONMENT_STATUS_DELETED, now)
	if _, err := store.DeleteNamespace(ctx, "team-a", now); err != nil {
		t.Fatalf("DeleteNamespace() error = %v", err)
	}
}

func TestStoreGetQuotaDoesNotCreateNamespace(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()

	_, err := store.Get(ctx, "missing")
	if grpcstatus.Code(err) != codes.NotFound {
		t.Fatalf("code = %v, want NotFound err=%v", grpcstatus.Code(err), err)
	}
	namespaces, err := store.ListNamespaces(ctx)
	if err != nil {
		t.Fatalf("ListNamespaces() error = %v", err)
	}
	if len(namespaces) != 0 {
		t.Fatalf("namespaces len = %d, want 0", len(namespaces))
	}
}

func TestStoreReportsActiveUsageAndNullableLimits(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	quota, err := store.Set(ctx, "team-a", &quotav1.NamespaceQuotaLimits{
		CpuMilli:    wrapperspb.Int64(1000),
		MemoryBytes: wrapperspb.Int64(1 << 30),
	}, now)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if quota.GetNamespace() != "team-a" {
		t.Fatalf("namespace = %q, want team-a", quota.GetNamespace())
	}
	if quota.GetCpuMilliLimit().GetValue() != 1000 {
		t.Fatalf("cpu limit = %d, want 1000", quota.GetCpuMilliLimit().GetValue())
	}
	insertReservation(t, db, "active-a", "alloc-active-a", "team-a", 300, 256<<20, false, now)
	insertReservation(t, db, "active-b", "alloc-active-b", "team-a", 200, 128<<20, false, now)
	insertReservation(t, db, "released-a", "alloc-released-a", "team-a", 900, 512<<20, true, now)

	quota, err = store.Get(ctx, "team-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if quota.GetReservedCpuMilli() != 500 {
		t.Fatalf("reserved cpu = %d, want 500", quota.GetReservedCpuMilli())
	}
	if quota.GetReservedMemoryBytes() != 384<<20 {
		t.Fatalf("reserved memory = %d, want %d", quota.GetReservedMemoryBytes(), int64(384<<20))
	}
	if quota.GetAvailableCpuMilli().GetValue() != 500 {
		t.Fatalf("available cpu = %d, want 500", quota.GetAvailableCpuMilli().GetValue())
	}
	if quota.GetAvailableMemoryBytes().GetValue() != (1<<30)-(384<<20) {
		t.Fatalf("available memory = %d, want %d", quota.GetAvailableMemoryBytes().GetValue(), int64((1<<30)-(384<<20)))
	}

	quota, err = store.Set(ctx, "team-b", &quotav1.NamespaceQuotaLimits{CpuMilli: wrapperspb.Int64(2000)}, now)
	if err != nil {
		t.Fatalf("Set team-b error = %v", err)
	}
	if quota.GetCpuMilliLimit().GetValue() != 2000 {
		t.Fatalf("team-b cpu limit = %d, want 2000", quota.GetCpuMilliLimit().GetValue())
	}
	if quota.GetMemoryBytesLimit() != nil || quota.GetAvailableMemoryBytes() != nil {
		t.Fatalf("team-b memory should be unlimited: limit=%v available=%v", quota.GetMemoryBytesLimit(), quota.GetAvailableMemoryBytes())
	}
}

func TestStoreUnsetReturnsUnlimitedQuota(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	if _, err := store.Set(ctx, "team-a", &quotav1.NamespaceQuotaLimits{
		CpuMilli:    wrapperspb.Int64(1000),
		MemoryBytes: wrapperspb.Int64(1 << 30),
	}, now); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	insertReservation(t, db, "active-a", "alloc-active-a", "team-a", 300, 256<<20, false, now)

	quota, err := store.Unset(ctx, "team-a", now.Add(time.Second))
	if err != nil {
		t.Fatalf("Unset() error = %v", err)
	}
	if quota.GetCpuMilliLimit() != nil || quota.GetMemoryBytesLimit() != nil {
		t.Fatalf("limits should be nil after clear: cpu=%v memory=%v", quota.GetCpuMilliLimit(), quota.GetMemoryBytesLimit())
	}
	if quota.GetAvailableCpuMilli() != nil || quota.GetAvailableMemoryBytes() != nil {
		t.Fatalf("available should be nil after clear: cpu=%v memory=%v", quota.GetAvailableCpuMilli(), quota.GetAvailableMemoryBytes())
	}
	if quota.GetReservedCpuMilli() != 300 || quota.GetReservedMemoryBytes() != 256<<20 {
		t.Fatalf("reserved after clear = cpu %d memory %d, want cpu 300 memory %d", quota.GetReservedCpuMilli(), quota.GetReservedMemoryBytes(), int64(256<<20))
	}
}

func TestStoreAllowsLoweringQuotaBelowActiveUsage(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	if _, err := store.Set(ctx, "team-a", &quotav1.NamespaceQuotaLimits{
		CpuMilli:    wrapperspb.Int64(4000),
		MemoryBytes: wrapperspb.Int64(4 << 30),
	}, now); err != nil {
		t.Fatalf("initial Set() error = %v", err)
	}
	insertReservation(t, db, "active-a", "alloc-active-a", "team-a", 1200, 2<<30, false, now)

	quota, err := store.Set(ctx, "team-a", &quotav1.NamespaceQuotaLimits{
		CpuMilli:    wrapperspb.Int64(1000),
		MemoryBytes: wrapperspb.Int64(1 << 30),
	}, now)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if quota.GetReservedCpuMilli() != 1200 {
		t.Fatalf("reserved cpu = %d, want 1200", quota.GetReservedCpuMilli())
	}
	if quota.GetAvailableCpuMilli().GetValue() != 0 {
		t.Fatalf("available cpu = %d, want 0", quota.GetAvailableCpuMilli().GetValue())
	}
	if quota.GetReservedMemoryBytes() != 2<<30 {
		t.Fatalf("reserved memory = %d, want %d", quota.GetReservedMemoryBytes(), int64(2<<30))
	}
	if quota.GetAvailableMemoryBytes().GetValue() != 0 {
		t.Fatalf("available memory = %d, want 0", quota.GetAvailableMemoryBytes().GetValue())
	}
}

func TestStoreSetWaitsForNamespaceQuotaLock(t *testing.T) {
	db := newNamespaceTestDB(t)
	store := NewStore(db)
	ctx := context.Background()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := LockQuotaPolicy(ctx, tx, "team-a"); err != nil {
		t.Fatalf("LockQuotaPolicy() error = %v", err)
	}

	blockedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_, err = store.Set(blockedCtx, "team-a", &quotav1.NamespaceQuotaLimits{
		CpuMilli: wrapperspb.Int64(1000),
	}, now)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Set() error = %v, want context deadline while waiting for quota lock", err)
	}
}

func newNamespaceTestDB(t *testing.T) *postgres.DB {
	t.Helper()
	dsn := os.Getenv("AXERN_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AXERN_TEST_POSTGRES_DSN is not set")
	}
	db, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(db.Close)
	if _, err := db.ApplyMigrations(context.Background()); err != nil {
		t.Fatalf("apply postgres migrations: %v", err)
	}
	truncateNamespaceTestTables(t, db)
	return db
}

func truncateNamespaceTestTables(t *testing.T, db *postgres.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		TRUNCATE TABLE
			role_bindings,
			principal_credentials,
			principals,
			runs,
			environments,
			workload_reservations,
			namespace_resource_quotas,
			namespaces
		CASCADE
	`); err != nil {
		t.Fatalf("truncate namespace test tables: %v", err)
	}
}

func insertReservation(t *testing.T, db *postgres.DB, reservationID, allocationID, namespace string, cpuMilli, memoryBytes int64, released bool, now time.Time) {
	t.Helper()
	var releasedAt any
	if released {
		releasedAt = now.Add(time.Minute)
	}
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO workload_reservations (
			reservation_id, allocation_id, namespace, owner_type, owner_id, node_id,
			cpu_milli, memory_bytes, created_at, released_at
		) VALUES ($1, $2, $3, 'run', $4, 'node-a', $5, $6, $7, $8)
	`, reservationID, allocationID, namespace, allocationID, cpuMilli, memoryBytes, now, releasedAt); err != nil {
		t.Fatalf("insert reservation %s: %v", reservationID, err)
	}
}

func insertEnvironment(t *testing.T, db *postgres.DB, environmentID, namespace string, status environmentv1.EnvironmentStatus, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO environments (
			environment_id, namespace, status, spec_hash, spec, resolved_template,
			labels, created_at, updated_at
		) VALUES ($1, $2, $3, $4, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $5, $5)
	`, environmentID, namespace, status.String(), environmentID+"-hash", now); err != nil {
		t.Fatalf("insert environment %s: %v", environmentID, err)
	}
}

func insertRun(t *testing.T, db *postgres.DB, runID, namespace, environmentID, allocationID string, status runv1.RunStatus, now time.Time) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), `
		INSERT INTO runs (
			run_id, namespace, environment_id, allocation_id, status, config,
			labels, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, '{}'::jsonb, '{}'::jsonb, $6, $6)
	`, runID, namespace, environmentID, allocationID, status.String(), now); err != nil {
		t.Fatalf("insert run %s: %v", runID, err)
	}
}
